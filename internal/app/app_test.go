package app

import (
	"context"
	"errors"
	"testing"

	"remove-feishu-ai/internal/cli"
	"remove-feishu-ai/internal/feature"
)

func TestBuildMenuItemsReflectFeatureStates(t *testing.T) {
	app := &App{}

	items := app.buildMenuItems([]FeatureSnapshot{
		{
			ID:          featureIDKnowledgeSidebar,
			DisplayName: "侧边栏知识问答",
			State:       feature.State{Internal: feature.StateOriginal},
		},
		{
			ID:          featureIDGroupSummary,
			DisplayName: "群聊 AI 消息速览",
			State:       feature.State{Internal: feature.StateUnknown},
		},
	})

	if len(items) < 8 {
		t.Fatalf("len(items) = %d, want >= 8", len(items))
	}

	knowledgeRemove := findMenuItem(t, items, cli.ActionRemoveKnowledgeSidebar)
	if !knowledgeRemove.Enabled {
		t.Fatal("knowledge remove item is disabled, want enabled")
	}

	knowledgeRestore := findMenuItem(t, items, cli.ActionRestoreKnowledgeSidebar)
	if knowledgeRestore.Enabled {
		t.Fatal("knowledge restore item is enabled, want disabled")
	}

	groupRemove := findMenuItem(t, items, cli.ActionRemoveGroupSummary)
	if groupRemove.Enabled {
		t.Fatal("group remove item is enabled, want disabled")
	}

	if items[0].Label == "" {
		t.Fatal("first label is empty")
	}
}

func TestAggregateStatesForCompositeFeature(t *testing.T) {
	tests := []struct {
		name   string
		states []feature.State
		want   feature.InternalState
	}{
		{
			name: "all original",
			states: []feature.State{
				{Internal: feature.StateOriginal},
				{Internal: feature.StateOriginal},
			},
			want: feature.StateOriginal,
		},
		{
			name: "all patched",
			states: []feature.State{
				{Internal: feature.StatePatched},
				{Internal: feature.StatePatched},
			},
			want: feature.StatePatched,
		},
		{
			name: "original and patched becomes mixed",
			states: []feature.State{
				{Internal: feature.StateOriginal},
				{Internal: feature.StatePatched},
			},
			want: feature.StateMixed,
		},
		{
			name: "unknown keeps feature unknown",
			states: []feature.State{
				{Internal: feature.StateOriginal},
				{Internal: feature.StateUnknown},
			},
			want: feature.StateUnknown,
		},
		{
			name: "component mixed keeps feature mixed",
			states: []feature.State{
				{Internal: feature.StateMixed},
				{Internal: feature.StateOriginal},
			},
			want: feature.StateMixed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := aggregateStates(test.states)
			if got.Internal != test.want {
				t.Fatalf("aggregateStates() = %q, want %q", got.Internal, test.want)
			}
		})
	}
}

func TestExecuteChoiceRemoveAllRollsBackOnStrictFailure(t *testing.T) {
	removeErr := errors.New("group remove failed")
	firstComponent := &fakeFeatureComponent{id: "knowledge-frame", displayName: "知识问答底层 A"}
	secondComponent := &fakeFeatureComponent{id: "group-js", displayName: "群聊总结底层", removeErr: removeErr}
	fakeTx := &fakeTransaction{}

	app := &App{
		targets: []featureTarget{
			{
				ID:          featureIDKnowledgeSidebar,
				DisplayName: "侧边栏知识问答",
				Components:  []feature.Feature{firstComponent},
			},
			{
				ID:          featureIDGroupSummary,
				DisplayName: "群聊 AI 消息速览",
				Components:  []feature.Feature{secondComponent},
			},
		},
		newTransaction: func(backupRoot, installPath, operation string) (transaction, error) {
			return fakeTx, nil
		},
	}

	snapshots := []FeatureSnapshot{
		{ID: featureIDKnowledgeSidebar, DisplayName: "侧边栏知识问答", State: feature.State{Internal: feature.StateOriginal}},
		{ID: featureIDGroupSummary, DisplayName: "群聊 AI 消息速览", State: feature.State{Internal: feature.StateOriginal}},
	}

	err := app.executeChoice(context.Background(), &Env{StrictMode: true}, cli.ActionRemoveAll, snapshots)
	if !errors.Is(err, removeErr) {
		t.Fatalf("executeChoice() error = %v, want wrapped remove error", err)
	}

	if firstComponent.removeCalls != 1 {
		t.Fatalf("first remove calls = %d, want 1", firstComponent.removeCalls)
	}
	if secondComponent.removeCalls != 1 {
		t.Fatalf("second remove calls = %d, want 1", secondComponent.removeCalls)
	}
	if fakeTx.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", fakeTx.rollbackCalls)
	}
	if fakeTx.commitCalls != 0 {
		t.Fatalf("commit calls = %d, want 0", fakeTx.commitCalls)
	}
}

func TestExecuteChoiceRemoveAllUsesIndependentTransactionsWhenStrictModeDisabled(t *testing.T) {
	removeErr := errors.New("group remove failed")
	firstComponent := &fakeFeatureComponent{id: "knowledge-frame", displayName: "知识问答底层 A"}
	secondComponent := &fakeFeatureComponent{id: "group-js", displayName: "群聊总结底层", removeErr: removeErr}
	firstTx := &fakeTransaction{}
	secondTx := &fakeTransaction{}
	transactionCalls := 0

	app := &App{
		targets: []featureTarget{
			{
				ID:          featureIDKnowledgeSidebar,
				DisplayName: "侧边栏知识问答",
				Components:  []feature.Feature{firstComponent},
			},
			{
				ID:          featureIDGroupSummary,
				DisplayName: "群聊 AI 消息速览",
				Components:  []feature.Feature{secondComponent},
			},
		},
		newTransaction: func(backupRoot, installPath, operation string) (transaction, error) {
			transactionCalls++
			if transactionCalls == 1 {
				return firstTx, nil
			}
			if transactionCalls == 2 {
				return secondTx, nil
			}
			t.Fatalf("newTransaction called %d times, want 2", transactionCalls)
			return nil, nil
		},
	}

	snapshots := []FeatureSnapshot{
		{ID: featureIDKnowledgeSidebar, DisplayName: "侧边栏知识问答", State: feature.State{Internal: feature.StateOriginal}},
		{ID: featureIDGroupSummary, DisplayName: "群聊 AI 消息速览", State: feature.State{Internal: feature.StateOriginal}},
	}

	err := app.executeChoice(context.Background(), &Env{StrictMode: false}, cli.ActionRemoveAll, snapshots)
	if !errors.Is(err, removeErr) {
		t.Fatalf("executeChoice() error = %v, want wrapped remove error", err)
	}

	if firstTx.commitCalls != 1 {
		t.Fatalf("first commit calls = %d, want 1", firstTx.commitCalls)
	}
	if firstTx.rollbackCalls != 0 {
		t.Fatalf("first rollback calls = %d, want 0", firstTx.rollbackCalls)
	}
	if secondTx.commitCalls != 0 {
		t.Fatalf("second commit calls = %d, want 0", secondTx.commitCalls)
	}
	if secondTx.rollbackCalls != 1 {
		t.Fatalf("second rollback calls = %d, want 1", secondTx.rollbackCalls)
	}
}

func findMenuItem(t *testing.T, items []cli.MenuItem, action cli.Action) cli.MenuItem {
	t.Helper()

	for _, item := range items {
		if item.Action == action {
			return item
		}
	}

	t.Fatalf("menu item %q not found", action)
	return cli.MenuItem{}
}

type fakeFeatureComponent struct {
	id           string
	displayName  string
	detectState  feature.State
	detectErr    error
	removeErr    error
	restoreErr   error
	removeCalls  int
	restoreCalls int
}

func (f *fakeFeatureComponent) ID() string {
	return f.id
}

func (f *fakeFeatureComponent) DisplayName() string {
	return f.displayName
}

func (f *fakeFeatureComponent) Detect(ctx context.Context, env feature.Env) (feature.State, error) {
	return f.detectState, f.detectErr
}

func (f *fakeFeatureComponent) Remove(ctx context.Context, env feature.Env, tx feature.Tx) error {
	f.removeCalls++
	return f.removeErr
}

func (f *fakeFeatureComponent) Restore(ctx context.Context, env feature.Env, tx feature.Tx) error {
	f.restoreCalls++
	return f.restoreErr
}

type fakeTransaction struct {
	commitCalls   int
	rollbackCalls int
}

func (tx *fakeTransaction) BackupFile(relativePath, sourcePath string) error {
	return nil
}

func (tx *fakeTransaction) Commit() error {
	tx.commitCalls++
	return nil
}

func (tx *fakeTransaction) Rollback() error {
	tx.rollbackCalls++
	return nil
}
