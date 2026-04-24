package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"remove-feishu-ai/internal/backup"
	"remove-feishu-ai/internal/cli"
	"remove-feishu-ai/internal/config"
	"remove-feishu-ai/internal/elevate"
	"remove-feishu-ai/internal/feature"
	"remove-feishu-ai/internal/featureframe"
	"remove-feishu-ai/internal/featurejs"
	"remove-feishu-ai/internal/install"
	"remove-feishu-ai/internal/logx"
	"remove-feishu-ai/internal/tx"
)

// adminEnsurer 描述应用入口依赖的管理员权限保障函数。
//
// 返回值 relaunched 为 true 时表示已经通过 UAC 启动了新的提升进程，当前
// 原进程应立即结束，避免继续执行后续补丁逻辑。
type adminEnsurer func([]string) (bool, error)

const (
	// featureIDKnowledgeSidebar 是“侧边栏知识问答”在菜单、日志与内部路由中的稳定标识。
	featureIDKnowledgeSidebar = "knowledge-sidebar"

	// featureIDGroupSummary 是“群聊 AI 消息速览/群聊总结”的稳定标识。
	featureIDGroupSummary = "group-summary"

	// frameOffsetCacheKey 是配置中保存 frame.dll 偏移缓存的键。
	frameOffsetCacheKey = "frame.dll"

	// recentBackupLimit 是“查看最近备份”一次最多展示的事务目录数量。
	recentBackupLimit = 5
)

// transaction 抽象应用层需要的事务能力。
//
// 除 feature.Tx 要求的 `BackupFile` 外，应用层还需要在动作成功时 `Commit`，
// 在严格模式失败时 `Rollback`。
type transaction interface {
	feature.Tx
	Commit() error
	Rollback() error
}

// transactionFactory 负责按当前操作创建一份新的事务实例。
type transactionFactory func(backupRoot, installPath, operation string) (transaction, error)

// configLoader 与 configSaver 用于把配置读写解耦为可替换依赖，便于测试。
type configLoader func(path string) (config.Config, error)
type configSaver func(path string, cfg config.Config) error

// installResolver 用于解析并校验飞书安装路径。
type installResolver func(input string, fallback string) (string, error)

// executableLocator 用于获取当前可执行文件路径。
type executableLocator func() (string, error)

// featureTarget 描述一个用户可见功能及其底层组成部分。
//
// 当前“侧边栏知识问答”由 `frame.dll` 与知识问答侧边栏 JS 两个底层 patch point
// 共同构成；“群聊 AI 消息速览/群聊总结”则暂时只包含一条 JS 规则。
type featureTarget struct {
	ID          string
	DisplayName string
	Components  []feature.Feature
}

// FeatureSnapshot 描述一次扫描后，单个用户可见功能在菜单中的状态快照。
//
// 它已经是应用层聚合后的结果：底层一个或多个组件的状态会先合成为用户可见
// 的 `State`，再交给菜单构建与动作启用判断使用。
type FeatureSnapshot struct {
	ID          string
	DisplayName string
	State       feature.State
}

// App 聚合命令行工具运行期间需要共享的入口依赖与默认目标功能集合。
type App struct {
	logger             *logx.Logger
	console            *cli.Console
	ensureAdmin        adminEnsurer
	loadConfig         configLoader
	saveConfig         configSaver
	resolveInstallPath installResolver
	executablePath     executableLocator
	newTransaction     transactionFactory
	targets            []featureTarget
}

// New 创建一份默认应用实例。
//
// 返回的 App 默认挂载：
// 1. Windows 管理员权限确保逻辑。
// 2. 控制台交互实现。
// 3. 默认两个用户可见功能目标。
// 4. 程序目录下的配置与备份路径解析流程。
func New() *App {
	logger := logx.New(false)

	return &App{
		logger:             logger,
		console:            cli.NewConsole(os.Stdin, os.Stdout),
		ensureAdmin:        elevate.EnsureAdmin,
		loadConfig:         config.LoadOrCreate,
		saveConfig:         config.Save,
		resolveInstallPath: install.Resolve,
		executablePath:     os.Executable,
		newTransaction: func(backupRoot, installPath, operation string) (transaction, error) {
			return tx.New(backupRoot, installPath, operation)
		},
		targets: defaultTargets(),
	}
}

// Run 执行应用主流程。
//
// 函数开始时会先确保当前进程具备管理员权限；如果当前进程未提升并且
// 已成功通过 UAC 启动新进程，则原进程直接返回 nil，由调用方正常退出。
// 在权限满足后，Run 还会串联参数解析、配置加载、安装路径确认、状态扫描、
// 主菜单交互、事务执行以及结果输出。
func (a *App) Run(args []string) error {
	ensureAdmin := a.ensureAdmin
	if ensureAdmin == nil {
		ensureAdmin = elevate.EnsureAdmin
	}

	relaunched, err := ensureAdmin(args)
	if err != nil {
		return err
	}
	if relaunched {
		return nil
	}

	options, err := parseArgs(args)
	if err != nil {
		return err
	}

	a.logger = logx.NewWithWriters(options.verbose, os.Stdout, os.Stderr)
	if a.console == nil {
		a.console = cli.NewConsole(os.Stdin, os.Stdout)
	}

	env, err := a.bootstrap()
	if err != nil {
		return err
	}

	ctx := context.Background()
	for {
		snapshots, err := a.scan(ctx, env)
		if err != nil {
			return err
		}

		choice, err := a.console.SelectAction(a.buildMenuScreen(env, snapshots))
		if err != nil {
			return err
		}
		if choice == cli.ActionExit {
			return nil
		}

		if err := a.executeChoice(ctx, env, choice, snapshots); err != nil {
			a.logger.Fail("%v", err)
		}
	}
}

// defaultTargets 返回当前版本默认支持的两个用户可见功能目标。
//
// 第一个目标“侧边栏知识问答”由 `frame.dll` 与知识问答侧边栏 JS 两个底层组件
// 共同组成；第二个目标“群聊 AI 消息速览/群聊总结”当前只挂载占位 JS 规则。
func defaultTargets() []featureTarget {
	return []featureTarget{
		{
			ID:          featureIDKnowledgeSidebar,
			DisplayName: "侧边栏知识问答",
			Components: []feature.Feature{
				featureframe.New(),
				featurejs.NewKnowledgeSidebarFeature(),
			},
		},
		{
			ID:          featureIDGroupSummary,
			DisplayName: "群聊 AI 消息速览/群聊总结",
			Components: []feature.Feature{
				featurejs.NewGroupSummaryFeature(),
			},
		},
	}
}

// runOptions 保存当前版本支持的命令行参数结果。
type runOptions struct {
	verbose bool
}

// parseArgs 解析启动参数。
//
// 当前版本只接受 `--verbose` 或 `-v`；其他未知参数会直接返回错误，避免用户误以为
// 某个参数已经生效。
func parseArgs(args []string) (runOptions, error) {
	var options runOptions

	for _, arg := range args {
		switch arg {
		case "--verbose", "-v":
			options.verbose = true
		case "":
		default:
			return runOptions{}, fmt.Errorf("不支持的参数: %s", arg)
		}
	}

	return options, nil
}

// bootstrap 组装一份本次运行使用的环境对象，并要求用户确认安装路径。
func (a *App) bootstrap() (*Env, error) {
	executablePath := a.executablePath
	if executablePath == nil {
		executablePath = os.Executable
	}

	exePath, err := executablePath()
	if err != nil {
		return nil, err
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.json")

	loadConfig := a.loadConfig
	if loadConfig == nil {
		loadConfig = config.LoadOrCreate
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}

	env := &Env{
		ConfigPath: configPath,
		ExeDir:     exeDir,
		BackupRoot: ResolveBackupRoot(exeDir, cfg.BackupRoot),
		StrictMode: cfg.StrictModeDefault,
		Config:     cfg,
		Logger:     a.logger,
	}
	ensureConfigMaps(&env.Config)

	if err := a.confirmInstallPath(env); err != nil {
		return nil, err
	}

	return env, nil
}

// confirmInstallPath 反复提示用户确认飞书安装路径，直到 Resolve 成功。
func (a *App) confirmInstallPath(env *Env) error {
	if a.console == nil {
		return errors.New("console is not configured")
	}

	resolveInstallPath := a.resolveInstallPath
	if resolveInstallPath == nil {
		resolveInstallPath = install.Resolve
	}

	for {
		input, err := a.console.PromptInstallPath(env.Config.LastInstallPath)
		if err != nil {
			return err
		}

		installDir, err := resolveInstallPath(input, env.Config.LastInstallPath)
		if err != nil {
			a.logger.Warn("安装路径无效：%v", err)
			continue
		}

		env.InstallDir = installDir
		env.Config.LastInstallPath = installDir
		env.BackupRoot = ResolveBackupRoot(env.ExeDir, env.Config.BackupRoot)

		return a.persistConfig(env)
	}
}

// scan 检测两个用户可见功能的当前状态，并把本地缓存回写到 config。
func (a *App) scan(ctx context.Context, env *Env) ([]FeatureSnapshot, error) {
	snapshots := make([]FeatureSnapshot, 0, len(a.targets))

	for _, target := range a.targets {
		componentStates := make([]feature.State, 0, len(target.Components))
		for _, component := range target.Components {
			state, err := a.detectComponent(ctx, env, component)
			if err != nil {
				return nil, fmt.Errorf("%s 检测失败: %w", target.DisplayName, err)
			}
			componentStates = append(componentStates, state)
		}

		snapshots = append(snapshots, FeatureSnapshot{
			ID:          target.ID,
			DisplayName: target.DisplayName,
			State:       aggregateStates(componentStates),
		})
	}

	if err := a.persistConfig(env); err != nil {
		return nil, err
	}

	return snapshots, nil
}

// detectComponent 对一个底层组件执行检测，并在适用时更新本地加速缓存。
func (a *App) detectComponent(ctx context.Context, env *Env, component feature.Feature) (feature.State, error) {
	ensureConfigMaps(&env.Config)

	switch concrete := component.(type) {
	case *featureframe.Feature:
		cachedOffsets := env.Config.LastSuccessOffsets[frameOffsetCacheKey]
		state, meta, err := concrete.DetectWithCache(ctx, env, cachedOffsets)
		if err != nil {
			return feature.State{}, err
		}
		if len(meta.OldOffsets) > 0 || len(meta.NewOffsets) > 0 {
			targetPath := filepath.Join(env.InstallDir, featureframe.DefaultRule().FileRelativePath)
			if info, statErr := os.Stat(targetPath); statErr == nil {
				env.Config.LastSuccessOffsets[frameOffsetCacheKey] = config.OffsetCache{
					OldPatternOffsets: append([]int64(nil), meta.OldOffsets...),
					NewPatternOffsets: append([]int64(nil), meta.NewOffsets...),
					FileSize:          info.Size(),
					MTime:             info.ModTime().UTC().Format(time.RFC3339Nano),
				}
			}
		}
		return state, nil
	case *featurejs.Feature:
		state, meta, err := concrete.DetectWithCache(ctx, env, env.Config.LastBundleRelativePath)
		if err != nil {
			return feature.State{}, err
		}
		if meta.RelativePath != "" {
			env.Config.LastBundleRelativePath = meta.RelativePath
			if info, statErr := os.Stat(meta.BundlePath); statErr == nil {
				env.Config.LastRuleHits[concrete.ID()] = config.RuleHitCache{
					FileSize: info.Size(),
					MTime:    info.ModTime().UTC().Format(time.RFC3339Nano),
				}
			}
		}
		return state, nil
	default:
		return component.Detect(ctx, env)
	}
}

// aggregateStates 将一个用户可见功能的多个底层组件状态合成为一个菜单状态。
//
// 规则如下：
// 1. 只要任一组件是 mixed，整体就是 mixed。
// 2. 所有组件都 original，整体才是 original。
// 3. 所有组件都 patched，整体才是 patched。
// 4. original 与 patched 同时出现时，说明功能只改了一部分，整体是 mixed。
// 5. 其他情况，包括 unknown 与明确状态混合，统一视为 unknown。
func aggregateStates(states []feature.State) feature.State {
	if len(states) == 0 {
		return feature.State{Internal: feature.StateUnknown}
	}

	hasOriginal := false
	hasPatched := false
	hasUnknown := false

	for _, state := range states {
		switch state.Normalized().Internal {
		case feature.StateMixed:
			return feature.State{Internal: feature.StateMixed}
		case feature.StateOriginal:
			hasOriginal = true
		case feature.StatePatched:
			hasPatched = true
		default:
			hasUnknown = true
		}
	}

	switch {
	case hasOriginal && hasPatched:
		return feature.State{Internal: feature.StateMixed}
	case hasUnknown:
		return feature.State{Internal: feature.StateUnknown}
	case hasOriginal:
		return feature.State{Internal: feature.StateOriginal}
	case hasPatched:
		return feature.State{Internal: feature.StatePatched}
	default:
		return feature.State{Internal: feature.StateUnknown}
	}
}

// buildMenuScreen 把应用层状态转换为 CLI 渲染所需的数据结构。
func (a *App) buildMenuScreen(env *Env, snapshots []FeatureSnapshot) cli.MenuScreen {
	statuses := make([]cli.StatusLine, 0, len(snapshots))
	for _, snapshot := range snapshots {
		statuses = append(statuses, cli.StatusLine{
			DisplayName: snapshot.DisplayName,
			StateText:   snapshot.State.DisplayString(),
		})
	}

	return cli.MenuScreen{
		InstallPath: env.InstallDir,
		StrictMode:  env.StrictMode,
		Statuses:    statuses,
		Items:       a.buildMenuItems(snapshots),
	}
}

// buildMenuItems 根据扫描结果构建主菜单动作列表及其启用状态。
func (a *App) buildMenuItems(snapshots []FeatureSnapshot) []cli.MenuItem {
	knowledgeSnapshot, knowledgeExists := findSnapshot(snapshots, featureIDKnowledgeSidebar)
	groupSnapshot, groupExists := findSnapshot(snapshots, featureIDGroupSummary)

	canRemoveAny := false
	canRestoreAny := false
	for _, snapshot := range snapshots {
		if snapshot.State.CanRemove() {
			canRemoveAny = true
		}
		if snapshot.State.CanRestore() {
			canRestoreAny = true
		}
	}

	return []cli.MenuItem{
		{Action: cli.ActionRemoveAll, Label: "禁用全部可识别功能", Enabled: canRemoveAny},
		{Action: cli.ActionRestoreAll, Label: "恢复全部已禁用功能", Enabled: canRestoreAny},
		{
			Action:  cli.ActionRemoveKnowledgeSidebar,
			Label:   "只禁用侧边栏知识问答",
			Enabled: knowledgeExists && knowledgeSnapshot.State.CanRemove(),
		},
		{
			Action:  cli.ActionRestoreKnowledgeSidebar,
			Label:   "只恢复侧边栏知识问答",
			Enabled: knowledgeExists && knowledgeSnapshot.State.CanRestore(),
		},
		{
			Action:  cli.ActionRemoveGroupSummary,
			Label:   "只禁用群聊 AI 消息速览/群聊总结",
			Enabled: groupExists && groupSnapshot.State.CanRemove(),
		},
		{
			Action:  cli.ActionRestoreGroupSummary,
			Label:   "只恢复群聊 AI 消息速览/群聊总结",
			Enabled: groupExists && groupSnapshot.State.CanRestore(),
		},
		{Action: cli.ActionReselectInstallPath, Label: "重新选择安装路径", Enabled: true},
		{Action: cli.ActionShowRecentBackups, Label: "查看最近备份", Enabled: true},
		{Action: cli.ActionExit, Label: "退出", Enabled: true},
	}
}

// executeChoice 根据用户选择执行批量动作、单项动作或辅助动作。
func (a *App) executeChoice(ctx context.Context, env *Env, choice cli.Action, snapshots []FeatureSnapshot) error {
	switch choice {
	case cli.ActionRemoveAll:
		return a.executeBatch(ctx, env, snapshots, true)
	case cli.ActionRestoreAll:
		return a.executeBatch(ctx, env, snapshots, false)
	case cli.ActionRemoveKnowledgeSidebar:
		return a.executeSingle(ctx, env, featureIDKnowledgeSidebar, true)
	case cli.ActionRestoreKnowledgeSidebar:
		return a.executeSingle(ctx, env, featureIDKnowledgeSidebar, false)
	case cli.ActionRemoveGroupSummary:
		return a.executeSingle(ctx, env, featureIDGroupSummary, true)
	case cli.ActionRestoreGroupSummary:
		return a.executeSingle(ctx, env, featureIDGroupSummary, false)
	case cli.ActionReselectInstallPath:
		return a.confirmInstallPath(env)
	case cli.ActionShowRecentBackups:
		return a.showRecentBackups(env)
	case cli.ActionExit:
		return nil
	default:
		return fmt.Errorf("未知菜单动作: %s", choice)
	}
}

// executeBatch 执行“禁用全部”或“恢复全部”这类批量动作。
//
// remove=true 表示执行移除，否则执行恢复。严格模式下，只要任意目标失败，就会
// 回滚本次事务中已经成功写入的所有文件。
func (a *App) executeBatch(ctx context.Context, env *Env, snapshots []FeatureSnapshot, remove bool) error {
	selectedTargets := make([]featureTarget, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if remove && snapshot.State.CanRemove() {
			if target, ok := a.findTarget(snapshot.ID); ok {
				selectedTargets = append(selectedTargets, target)
			}
		}
		if !remove && snapshot.State.CanRestore() {
			if target, ok := a.findTarget(snapshot.ID); ok {
				selectedTargets = append(selectedTargets, target)
			}
		}
	}

	if len(selectedTargets) == 0 {
		a.logger.Warn("当前没有可执行的批量操作。")
		return nil
	}

	if !env.StrictMode {
		return a.executeBatchNonStrict(ctx, env, selectedTargets, remove)
	}

	txInstance, err := a.createTransaction(env, batchOperationName(remove))
	if err != nil {
		return err
	}

	for _, target := range selectedTargets {
		if err := a.applyTargetAction(ctx, env, txInstance, target, remove); err != nil {
			if env.StrictMode {
				rollbackErr := txInstance.Rollback()
				if rollbackErr != nil {
					return errors.Join(fmt.Errorf("%s 失败: %w", target.DisplayName, err), fmt.Errorf("事务回滚失败: %w", rollbackErr))
				}
			}
			return fmt.Errorf("%s 失败: %w", target.DisplayName, err)
		}
	}

	if err := txInstance.Commit(); err != nil {
		rollbackErr := txInstance.Rollback()
		if rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}

	if remove {
		a.logger.Success("已完成批量禁用。")
	} else {
		a.logger.Success("已完成批量恢复。")
	}

	return nil
}

// executeBatchNonStrict 以“每个用户可见功能一份独立事务”的方式执行批量动作。
//
// 这样做的原因是：一个用户可见功能可能由多个底层组件组成。非严格模式如果仍然共用
// 同一个大事务，一旦某个功能的第二个组件失败，前一个组件就会留下半修改状态。独立
// 事务可以保证每个功能要么完整提交，要么在失败时完整回滚。
func (a *App) executeBatchNonStrict(ctx context.Context, env *Env, targets []featureTarget, remove bool) error {
	var executionErrs []error

	for _, target := range targets {
		txInstance, err := a.createTransaction(env, singleOperationName(target.ID, remove))
		if err != nil {
			executionErrs = append(executionErrs, fmt.Errorf("%s 创建事务失败: %w", target.DisplayName, err))
			continue
		}

		if err := a.applyTargetAction(ctx, env, txInstance, target, remove); err != nil {
			rollbackErr := txInstance.Rollback()
			if rollbackErr != nil {
				executionErrs = append(executionErrs, errors.Join(fmt.Errorf("%s 失败: %w", target.DisplayName, err), fmt.Errorf("%s 回滚失败: %w", target.DisplayName, rollbackErr)))
			} else {
				executionErrs = append(executionErrs, fmt.Errorf("%s 失败: %w", target.DisplayName, err))
			}
			continue
		}

		if err := txInstance.Commit(); err != nil {
			rollbackErr := txInstance.Rollback()
			if rollbackErr != nil {
				executionErrs = append(executionErrs, errors.Join(fmt.Errorf("%s 提交失败: %w", target.DisplayName, err), fmt.Errorf("%s 回滚失败: %w", target.DisplayName, rollbackErr)))
			} else {
				executionErrs = append(executionErrs, fmt.Errorf("%s 提交失败: %w", target.DisplayName, err))
			}
			continue
		}
	}

	if len(executionErrs) > 0 {
		return errors.Join(executionErrs...)
	}

	if remove {
		a.logger.Success("已完成批量禁用。")
	} else {
		a.logger.Success("已完成批量恢复。")
	}

	return nil
}

// executeSingle 执行单个用户可见功能的移除或恢复动作。
func (a *App) executeSingle(ctx context.Context, env *Env, targetID string, remove bool) error {
	target, ok := a.findTarget(targetID)
	if !ok {
		return fmt.Errorf("未找到目标功能: %s", targetID)
	}

	txInstance, err := a.createTransaction(env, singleOperationName(targetID, remove))
	if err != nil {
		return err
	}

	if err := a.applyTargetAction(ctx, env, txInstance, target, remove); err != nil {
		rollbackErr := txInstance.Rollback()
		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("%s 失败: %w", target.DisplayName, err), fmt.Errorf("事务回滚失败: %w", rollbackErr))
		}
		return fmt.Errorf("%s 失败: %w", target.DisplayName, err)
	}

	if err := txInstance.Commit(); err != nil {
		rollbackErr := txInstance.Rollback()
		if rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}

	if remove {
		a.logger.Success("%s 已禁用。", target.DisplayName)
	} else {
		a.logger.Success("%s 已恢复。", target.DisplayName)
	}

	return nil
}

// applyTargetAction 对一个用户可见功能的全部底层组件执行相同方向的动作。
func (a *App) applyTargetAction(ctx context.Context, env *Env, txInstance transaction, target featureTarget, remove bool) error {
	for _, component := range target.Components {
		var err error
		if remove {
			err = component.Remove(ctx, env, txInstance)
		} else {
			err = component.Restore(ctx, env, txInstance)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", component.DisplayName(), err)
		}
	}

	return nil
}

// createTransaction 创建一份用于本次动作的事务实例。
func (a *App) createTransaction(env *Env, operation string) (transaction, error) {
	newTransaction := a.newTransaction
	if newTransaction == nil {
		newTransaction = func(backupRoot, installPath, operation string) (transaction, error) {
			return tx.New(backupRoot, installPath, operation)
		}
	}

	return newTransaction(env.BackupRoot, env.InstallDir, operation)
}

// batchOperationName 返回批量动作写入 manifest 时使用的操作名。
func batchOperationName(remove bool) string {
	if remove {
		return "remove_all"
	}
	return "restore_all"
}

// singleOperationName 返回单项动作写入 manifest 时使用的操作名。
func singleOperationName(targetID string, remove bool) string {
	if remove {
		return "remove_" + targetID
	}
	return "restore_" + targetID
}

// showRecentBackups 展示 backup 目录下最近的若干事务记录。
func (a *App) showRecentBackups(env *Env) error {
	entries, err := os.ReadDir(env.BackupRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.logger.Warn("当前还没有备份记录。")
			return nil
		}
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})

	shown := 0
	a.logger.Info("最近备份：")
	for _, entry := range entries {
		if shown >= recentBackupLimit {
			break
		}
		if !entry.IsDir() {
			continue
		}

		root := filepath.Join(env.BackupRoot, entry.Name())
		manifest, manifestErr := backup.ReadManifest(root)
		if manifestErr != nil {
			a.logger.Warn("%s：manifest 读取失败：%v", entry.Name(), manifestErr)
			continue
		}

		a.logger.Info("%s | 状态=%s | 操作=%s", entry.Name(), manifest.Status, manifest.Operation)
		shown++
	}

	if shown == 0 {
		a.logger.Warn("当前还没有备份记录。")
	}

	return nil
}

// findTarget 根据稳定 ID 查找一个用户可见功能目标。
func (a *App) findTarget(targetID string) (featureTarget, bool) {
	for _, target := range a.targets {
		if target.ID == targetID {
			return target, true
		}
	}

	return featureTarget{}, false
}

// findSnapshot 在扫描结果中查找指定 ID 的状态快照。
func findSnapshot(snapshots []FeatureSnapshot, targetID string) (FeatureSnapshot, bool) {
	for _, snapshot := range snapshots {
		if snapshot.ID == targetID {
			return snapshot, true
		}
	}

	return FeatureSnapshot{}, false
}

// persistConfig 将内存中的当前配置快照写回 config.json。
func (a *App) persistConfig(env *Env) error {
	saveConfig := a.saveConfig
	if saveConfig == nil {
		saveConfig = config.Save
	}

	ensureConfigMaps(&env.Config)

	return saveConfig(env.ConfigPath, env.Config)
}

// ensureConfigMaps 补齐配置中的 map 字段，避免零值 Env 在测试或辅助入口中写入时 panic。
func ensureConfigMaps(cfg *config.Config) {
	if cfg.LastSuccessOffsets == nil {
		cfg.LastSuccessOffsets = make(map[string]config.OffsetCache)
	}
	if cfg.LastRuleHits == nil {
		cfg.LastRuleHits = make(map[string]config.RuleHitCache)
	}
}
