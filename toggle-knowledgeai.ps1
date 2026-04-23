[CmdletBinding()]
param(
    [ValidateSet('Remove', 'Restore')]
    [string]$Action,

    [string]$InstallPath,

    [switch]$NoPause
)

$ErrorActionPreference = 'Stop'

$BackupFileName = 'frame.dll.backup'
$ExpectedPatternMatchCount = 2
$PreferredSearchAnchors = @(0x2ce1381, 0x30f824b)
$PreferredSearchRadius = 0x200000

$OldPatternHex = '48 b9 6b 6e 6f 77 6c 65 64 67 48 89 08 c7 40 07 67 65 61 69'
$NewPatternHex = '48 b9 6b 6e 6f 77 6c 65 64 67 48 89 08 c7 40 07 67 65 78 78'
$MessengerBundleRelativePath = 'app\webcontent\messenger-vc\common'
$MessengerBundleRequiredMarkers = @(
    'settingKey:"lark_knowledge_ai_client_setting"',
    'pluginType:a.Vx.EDITOR_EXTENSION',
    'lark__editor--extension-knowledge-qa'
)
$MessengerEnableOriginalText = 'return s.P4.setEnable(u),u},h=e=>'
$MessengerEnablePatchedText = 'return s.P4.setEnable({main:!1,thread:!1}),{main:!1,thread:!1}},h=e=>'
$MessengerVisibilityOriginalText = 'getShowExtension:()=>t.scene===a.pC.main?dt.P4.enable.main:dt.P4.enable.thread'
$MessengerVisibilityPatchedText = 'getShowExtension:()=>!1'

function Write-Title {
    param([string]$Message)
    Write-Host ''
    Write-Host $Message -ForegroundColor Cyan
}

function Write-Info {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Gray
}

function Write-Success {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Green
}

function Write-WarnText {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Yellow
}

function Write-ErrorText {
    param([string]$Message)
    Write-Host $Message -ForegroundColor Red
}

function Convert-HexToBytes {
    param([string]$Hex)

    $clean = $Hex -replace '\s+', ''
    $bytes = New-Object byte[] ($clean.Length / 2)

    for ($i = 0; $i -lt $bytes.Length; $i++) {
        $bytes[$i] = [Convert]::ToByte($clean.Substring($i * 2, 2), 16)
    }

    return $bytes
}

function Get-FramePath {
    param([string]$ResolvedInstallPath)
    return (Join-Path $ResolvedInstallPath 'app\frame.dll')
}

function Get-BackupPath {
    param([string]$ResolvedInstallPath)
    return (Join-Path $ResolvedInstallPath ("app\" + $BackupFileName))
}

function Resolve-InstallPath {
    param([string]$UserInstallPath)

    $candidates = New-Object System.Collections.Generic.List[string]

    if ($UserInstallPath) {
        $candidates.Add($UserInstallPath)
    }

    $scriptRoot = Split-Path -Parent $PSCommandPath
    if ($scriptRoot) {
        $candidates.Add($scriptRoot)
    }

    foreach ($basePath in @($env:ProgramFiles, ${env:ProgramFiles(x86)})) {
        if ($basePath) {
            $candidates.Add((Join-Path $basePath 'Feishu'))
            $candidates.Add((Join-Path $basePath 'Lark'))
        }
    }

    foreach ($candidate in $candidates) {
        if (-not $candidate) {
            continue
        }

        $framePath = Join-Path $candidate 'app\frame.dll'
        if (Test-Path -LiteralPath $framePath) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }

    throw '未找到飞书安装目录。请使用 -InstallPath 手动指定。'
}

function Find-BackupFile {
    param([string]$ResolvedInstallPath)

    $preferred = Get-BackupPath -ResolvedInstallPath $ResolvedInstallPath
    if (Test-Path -LiteralPath $preferred) {
        return $preferred
    }

    $found = Get-ChildItem -LiteralPath $ResolvedInstallPath -Recurse -File -Filter $BackupFileName -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($found) {
        return $found.FullName
    }

    return $null
}

function Find-PatternOffsets {
    param(
        [byte[]]$Bytes,
        [byte[]]$Pattern,
        [object[]]$Ranges
    )

    $offsets = New-Object System.Collections.Generic.List[int]
    $seenOffsets = New-Object 'System.Collections.Generic.HashSet[int]'

    if (-not $Pattern -or $Pattern.Length -eq 0) {
        return @()
    }

    $searchRanges = @()
    if ($Ranges -and $Ranges.Count -gt 0) {
        $searchRanges = $Ranges
    }
    else {
        $searchRanges = @([pscustomobject]@{ Start = 0; End = $Bytes.Length - 1 })
    }

    $firstByte = $Pattern[0]
    foreach ($range in $searchRanges) {
        $rangeStart = [Math]::Max(0, [int]$range.Start)
        $rangeEnd = [Math]::Min($Bytes.Length - 1, [int]$range.End)
        $maxStart = $rangeEnd - $Pattern.Length + 1
        if ($maxStart -lt $rangeStart) {
            continue
        }

        $searchOffset = $rangeStart
        while ($searchOffset -le $maxStart) {
            $remaining = $maxStart - $searchOffset + 1
            $offset = [Array]::IndexOf($Bytes, $firstByte, $searchOffset, $remaining)
            if ($offset -lt 0) {
                break
            }

            $matched = $true
            for ($i = 1; $i -lt $Pattern.Length; $i++) {
                if ($Bytes[$offset + $i] -ne $Pattern[$i]) {
                    $matched = $false
                    break
                }
            }

            if ($matched -and $seenOffsets.Add($offset)) {
                $offsets.Add($offset)
                $searchOffset = $offset + $Pattern.Length
            }
            else {
                $searchOffset = $offset + 1
            }
        }
    }

    return @($offsets)
}

function Get-PreferredSearchRanges {
    param([int]$FileLength)

    $ranges = @()
    foreach ($anchor in $PreferredSearchAnchors) {
        $start = $anchor - $PreferredSearchRadius
        if ($start -lt 0) {
            $start = 0
        }

        $end = $anchor + $PreferredSearchRadius
        $maxEnd = $FileLength - 1
        if ($end -gt $maxEnd) {
            $end = $maxEnd
        }

        $ranges += [pscustomobject]@{
            Start = [int]$start
            End = [int]$end
        }
    }

    return $ranges
}

function Read-BytesFromStream {
    param(
        [System.IO.FileStream]$Stream,
        [long]$Offset,
        [int]$Length
    )

    $buffer = New-Object byte[] $Length
    $Stream.Seek($Offset, [System.IO.SeekOrigin]::Begin) | Out-Null

    $totalRead = 0
    while ($totalRead -lt $Length) {
        $readCount = $Stream.Read($buffer, $totalRead, $Length - $totalRead)
        if ($readCount -le 0) {
            throw "读取文件失败，目标偏移: $Offset"
        }

        $totalRead += $readCount
    }

    return $buffer
}

function Find-PatternOffsetsInFile {
    param(
        [string]$FilePath,
        [byte[]]$Pattern,
        [object[]]$Ranges
    )

    if (-not $Pattern -or $Pattern.Length -eq 0) {
        return @()
    }

    $offsets = New-Object System.Collections.Generic.List[int]
    $seenOffsets = New-Object 'System.Collections.Generic.HashSet[int]'
    $stream = [System.IO.File]::Open($FilePath, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)

    try {
        $searchRanges = @()
        if ($Ranges -and $Ranges.Count -gt 0) {
            $searchRanges = $Ranges
        }
        else {
            $searchRanges = @([pscustomobject]@{ Start = 0; End = [int]$stream.Length - 1 })
        }

        foreach ($range in $searchRanges) {
            $rangeStart = [Math]::Max(0, [int]$range.Start)
            $rangeEnd = [Math]::Min([int]$stream.Length - 1, [int]$range.End)
            $rangeLength = $rangeEnd - $rangeStart + 1
            if ($rangeLength -lt $Pattern.Length) {
                continue
            }

            $buffer = Read-BytesFromStream -Stream $stream -Offset $rangeStart -Length $rangeLength
            $localOffsets = Find-PatternOffsets -Bytes $buffer -Pattern $Pattern
            foreach ($localOffset in $localOffsets) {
                $absoluteOffset = [int]($rangeStart + $localOffset)
                if ($seenOffsets.Add($absoluteOffset)) {
                    $offsets.Add($absoluteOffset)
                }
            }
        }
    }
    finally {
        $stream.Dispose()
    }

    return $offsets.ToArray()
}

function Ensure-BackupExists {
    param(
        [string]$SourcePath,
        [string]$BackupPath
    )

    if (-not (Test-Path -LiteralPath $BackupPath)) {
        Write-Info '正在创建备份文件。'
        Copy-Item -LiteralPath $SourcePath -Destination $BackupPath -Force
    }
}

function Read-TextFileUtf8 {
    param([string]$Path)

    $bytes = [System.IO.File]::ReadAllBytes($Path)
    $hasBom = $bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF
    $start = if ($hasBom) { 3 } else { 0 }
    $content = [System.Text.Encoding]::UTF8.GetString($bytes, $start, $bytes.Length - $start)

    return [pscustomobject]@{
        Content = $content
        HasUtf8Bom = $hasBom
    }
}

function Write-TextFileUtf8 {
    param(
        [string]$Path,
        [string]$Content,
        [bool]$HasUtf8Bom
    )

    $encoding = New-Object System.Text.UTF8Encoding($HasUtf8Bom)
    [System.IO.File]::WriteAllText($Path, $Content, $encoding)
}

function Get-SubstringMatchCount {
    param(
        [string]$Content,
        [string]$Pattern
    )

    return ([regex]::Matches($Content, [regex]::Escape($Pattern))).Count
}

function Replace-ExactTextOnce {
    param(
        [string]$Content,
        [string]$OldValue,
        [string]$NewValue,
        [string]$PatchName
    )

    $matchCount = Get-SubstringMatchCount -Content $Content -Pattern $OldValue
    if ($matchCount -ne 1) {
        throw "$PatchName 匹配数量异常，当前命中数为 $matchCount。"
    }

    return $Content.Replace($OldValue, $NewValue)
}

function Get-MessengerBundleDirectory {
    param([string]$ResolvedInstallPath)
    return (Join-Path $ResolvedInstallPath $MessengerBundleRelativePath)
}

function Test-MessengerBundleCandidate {
    param([string]$Content)

    foreach ($marker in $MessengerBundleRequiredMarkers) {
        if (-not $Content.Contains($marker)) {
            return $false
        }
    }

    return $true
}

function Find-MessengerBundlePath {
    param([string]$ResolvedInstallPath)

    $bundleDirectory = Get-MessengerBundleDirectory -ResolvedInstallPath $ResolvedInstallPath
    if (-not (Test-Path -LiteralPath $bundleDirectory)) {
        return $null
    }

    $candidates = Get-ChildItem -LiteralPath $bundleDirectory -File -Filter '*.js' -ErrorAction SilentlyContinue | Sort-Object Length -Descending
    foreach ($candidate in $candidates) {
        $textInfo = Read-TextFileUtf8 -Path $candidate.FullName
        if (Test-MessengerBundleCandidate -Content $textInfo.Content) {
            return [pscustomobject]@{
                Path = $candidate.FullName
                BackupPath = ($candidate.FullName + '.backup')
                Content = $textInfo.Content
                HasUtf8Bom = $textInfo.HasUtf8Bom
            }
        }
    }

    return $null
}

function Find-MessengerBundleBackupPath {
    param([string]$ResolvedInstallPath)

    $bundleDirectory = Get-MessengerBundleDirectory -ResolvedInstallPath $ResolvedInstallPath
    if (-not (Test-Path -LiteralPath $bundleDirectory)) {
        return $null
    }

    $candidates = Get-ChildItem -LiteralPath $bundleDirectory -File -Filter '*.js.backup' -ErrorAction SilentlyContinue | Sort-Object Length -Descending
    foreach ($candidate in $candidates) {
        $textInfo = Read-TextFileUtf8 -Path $candidate.FullName
        if (Test-MessengerBundleCandidate -Content $textInfo.Content) {
            return [pscustomobject]@{
                Path = $candidate.FullName
                RestorePath = $candidate.FullName.Substring(0, $candidate.FullName.Length - '.backup'.Length)
                Content = $textInfo.Content
                HasUtf8Bom = $textInfo.HasUtf8Bom
            }
        }
    }

    return $null
}

function Set-BytesAtOffset {
    param(
        [byte[]]$Bytes,
        [int]$Offset,
        [byte[]]$Replacement
    )

    [Array]::Copy($Replacement, 0, $Bytes, $Offset, $Replacement.Length)
}

function Set-BytesAtOffsetsInFile {
    param(
        [string]$FilePath,
        [int[]]$Offsets,
        [byte[]]$Replacement
    )

    $stream = [System.IO.File]::Open($FilePath, [System.IO.FileMode]::Open, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None)

    try {
        foreach ($offset in $Offsets) {
            $stream.Seek($offset, [System.IO.SeekOrigin]::Begin) | Out-Null
            $stream.Write($Replacement, 0, $Replacement.Length)
        }

        $stream.Flush()
    }
    finally {
        $stream.Dispose()
    }
}

function Test-PatternAtOffsetsInFile {
    param(
        [string]$FilePath,
        [int[]]$Offsets,
        [byte[]]$Pattern
    )

    $stream = [System.IO.File]::Open($FilePath, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::ReadWrite)

    try {
        foreach ($offset in $Offsets) {
            $buffer = Read-BytesFromStream -Stream $stream -Offset $offset -Length $Pattern.Length
            for ($i = 0; $i -lt $Pattern.Length; $i++) {
                if ($buffer[$i] -ne $Pattern[$i]) {
                    return $false
                }
            }
        }
    }
    finally {
        $stream.Dispose()
    }

    return $true
}

function Stop-FeishuProcesses {
    $processes = Get-Process Feishu -ErrorAction SilentlyContinue
    if ($processes) {
        Write-WarnText '检测到飞书正在运行，脚本将先关闭飞书进程。'
        $processes | Stop-Process -Force
        Start-Sleep -Seconds 2
    }
}

function Invoke-RemoveFramePatch {
    param([string]$ResolvedInstallPath)

    $framePath = Get-FramePath -ResolvedInstallPath $ResolvedInstallPath
    $backupPath = Get-BackupPath -ResolvedInstallPath $ResolvedInstallPath

    if (-not (Test-Path -LiteralPath $framePath)) {
        throw "未找到目标文件: $framePath"
    }

    Stop-FeishuProcesses

    $oldPattern = Convert-HexToBytes -Hex $OldPatternHex
    $newPattern = Convert-HexToBytes -Hex $NewPatternHex
    $frameLength = [int](Get-Item -LiteralPath $framePath).Length
    $preferredRanges = Get-PreferredSearchRanges -FileLength $frameLength
    $preferredOldMatches = Find-PatternOffsetsInFile -FilePath $framePath -Pattern $oldPattern -Ranges $preferredRanges
    $preferredNewMatches = Find-PatternOffsetsInFile -FilePath $framePath -Pattern $newPattern -Ranges $preferredRanges

    if (($preferredOldMatches.Count -eq 0) -and ($preferredNewMatches.Count -eq $ExpectedPatternMatchCount)) {
        Write-Info '优先搜索命中：检测到当前已经是移除状态。'
        return [pscustomobject]@{
            Success = $true
            Message = '当前已经是移除状态，无需重复操作。'
            BackupPath = (Find-BackupFile -ResolvedInstallPath $ResolvedInstallPath)
        }
    }

    if (($preferredOldMatches.Count -eq $ExpectedPatternMatchCount) -and ($preferredNewMatches.Count -eq 0)) {
        Write-Info '优先搜索命中：已在历史锚点附近找到 2 处旧字节模式。'
        Ensure-BackupExists -SourcePath $framePath -BackupPath $backupPath

        Write-Info '优先搜索命中：执行快速定点补丁。'
        Set-BytesAtOffsetsInFile -FilePath $framePath -Offsets $preferredOldMatches -Replacement $newPattern

        $remainingOldMatches = Test-PatternAtOffsetsInFile -FilePath $framePath -Offsets $preferredOldMatches -Pattern $oldPattern
        $patchedNewMatches = Test-PatternAtOffsetsInFile -FilePath $framePath -Offsets $preferredOldMatches -Pattern $newPattern
        if ($remainingOldMatches -or (-not $patchedNewMatches)) {
            throw '快速路径局部校验失败：补丁字节未达到预期。'
        }

        Write-Info '优先搜索命中：局部校验通过。'
        return [pscustomobject]@{
            Success = $true
            Message = '已完成移除。'
            BackupPath = $backupPath
        }
    }

    Write-Info '优先搜索未完整命中，回退到全文件搜索。'
    [byte[]]$bytes = [System.IO.File]::ReadAllBytes($framePath)
    $oldMatches = Find-PatternOffsets -Bytes $bytes -Pattern $oldPattern
    $newMatches = Find-PatternOffsets -Bytes $bytes -Pattern $newPattern

    if (($oldMatches.Count -eq 0) -and ($newMatches.Count -eq $ExpectedPatternMatchCount)) {
        return [pscustomobject]@{
            Success = $true
            Message = '当前已经是移除状态，无需重复操作。'
            BackupPath = (Find-BackupFile -ResolvedInstallPath $ResolvedInstallPath)
        }
    }

    if ($oldMatches.Count -ne $ExpectedPatternMatchCount) {
        throw "未找到预期的旧字节模式，当前命中数为 $($oldMatches.Count)。这通常意味着当前版本的 frame.dll 与本补丁不兼容。"
    }

    if ($newMatches.Count -gt 0) {
        throw "检测到当前 frame.dll 同时包含旧模式和新模式，疑似处于部分修改状态。请先用备份恢复后再重试。"
    }

    Ensure-BackupExists -SourcePath $framePath -BackupPath $backupPath

    Write-Info '开始执行全文件补丁。'
    foreach ($offset in $oldMatches) {
        Set-BytesAtOffset -Bytes $bytes -Offset $offset -Replacement $newPattern
    }

    [System.IO.File]::WriteAllBytes($framePath, $bytes)

    Write-Info '开始执行全文件校验。'
    [byte[]]$patchedBytes = [System.IO.File]::ReadAllBytes($framePath)
    $remainingOldMatches = Find-PatternOffsets -Bytes $patchedBytes -Pattern $oldPattern
    $patchedNewMatches = Find-PatternOffsets -Bytes $patchedBytes -Pattern $newPattern
    if (($remainingOldMatches.Count -ne 0) -or ($patchedNewMatches.Count -ne $ExpectedPatternMatchCount)) {
        throw '补丁后校验失败：字节模式数量不符合预期。'
    }

    Write-Info '全文件校验通过。'

    return [pscustomobject]@{
        Success = $true
        Message = '已完成移除。'
        BackupPath = $backupPath
    }
}

function Invoke-RestoreFramePatch {
    param([string]$ResolvedInstallPath)

    $framePath = Get-FramePath -ResolvedInstallPath $ResolvedInstallPath
    $backupPath = Find-BackupFile -ResolvedInstallPath $ResolvedInstallPath

    if (-not $backupPath) {
        return [pscustomobject]@{
            Success = $false
            Message = '未发现备份文件，无法恢复。建议重新安装飞书。'
        }
    }

    Stop-FeishuProcesses
    Copy-Item -LiteralPath $backupPath -Destination $framePath -Force

    [byte[]]$restoredBytes = [System.IO.File]::ReadAllBytes($framePath)
    $oldPattern = Convert-HexToBytes -Hex $OldPatternHex
    $oldMatches = Find-PatternOffsets -Bytes $restoredBytes -Pattern $oldPattern
    if ($oldMatches.Count -lt 1) {
        throw '恢复后校验失败：未在恢复后的 frame.dll 中找到原始字节模式。'
    }

    return [pscustomobject]@{
        Success = $true
        Message = '已从备份恢复原始文件。'
        BackupPath = $backupPath
    }
}

function Invoke-RemoveMessengerPatch {
    param([string]$ResolvedInstallPath)

    $bundleInfo = Find-MessengerBundlePath -ResolvedInstallPath $ResolvedInstallPath
    if (-not $bundleInfo) {
        throw '未找到消息速览目标 bundle。这通常意味着当前版本的 messenger-vc 与本补丁不兼容。'
    }

    $content = $bundleInfo.Content
    $hasOriginalEnable = $content.Contains($MessengerEnableOriginalText)
    $hasPatchedEnable = $content.Contains($MessengerEnablePatchedText)
    $hasOriginalVisibility = $content.Contains($MessengerVisibilityOriginalText)
    $hasPatchedVisibility = $content.Contains($MessengerVisibilityPatchedText)

    if (($hasPatchedEnable -and -not $hasOriginalEnable) -and ($hasPatchedVisibility -and -not $hasOriginalVisibility)) {
        Write-Info '消息速览组件：检测到当前已经是移除状态。'
        return [pscustomobject]@{
            Success = $true
            Message = '消息速览组件当前已经是移除状态，无需重复操作。'
            BackupPath = if (Test-Path -LiteralPath $bundleInfo.BackupPath) { $bundleInfo.BackupPath } else { $null }
        }
    }

    if (($hasOriginalEnable -ne $hasOriginalVisibility) -or ($hasPatchedEnable -ne $hasPatchedVisibility)) {
        throw '消息速览组件检测到部分修改状态。请先用备份恢复后再重试。'
    }

    if ((-not $hasOriginalEnable) -or (-not $hasOriginalVisibility)) {
        throw '未找到消息速览组件所需的原始文本特征。这通常意味着当前版本的 messenger-vc 与本补丁不兼容。'
    }

    Ensure-BackupExists -SourcePath $bundleInfo.Path -BackupPath $bundleInfo.BackupPath

    Write-Info '消息速览组件：正在写入网页层补丁。'
    $patchedContent = Replace-ExactTextOnce -Content $content -OldValue $MessengerEnableOriginalText -NewValue $MessengerEnablePatchedText -PatchName '消息速览功能开关'
    $patchedContent = Replace-ExactTextOnce -Content $patchedContent -OldValue $MessengerVisibilityOriginalText -NewValue $MessengerVisibilityPatchedText -PatchName '消息速览扩展入口'
    Write-TextFileUtf8 -Path $bundleInfo.Path -Content $patchedContent -HasUtf8Bom $bundleInfo.HasUtf8Bom

    $verifyInfo = Read-TextFileUtf8 -Path $bundleInfo.Path
    if (($verifyInfo.Content.Contains($MessengerEnableOriginalText)) -or ($verifyInfo.Content.Contains($MessengerVisibilityOriginalText))) {
        throw '消息速览组件补丁后校验失败：仍检测到原始文本特征。'
    }

    if ((-not $verifyInfo.Content.Contains($MessengerEnablePatchedText)) -or (-not $verifyInfo.Content.Contains($MessengerVisibilityPatchedText))) {
        throw '消息速览组件补丁后校验失败：未检测到补丁后的文本特征。'
    }

    Write-Info '消息速览组件：网页层补丁校验通过。'
    return [pscustomobject]@{
        Success = $true
        Message = '消息速览组件已禁用。'
        BackupPath = $bundleInfo.BackupPath
    }
}

function Invoke-RestoreMessengerPatch {
    param([string]$ResolvedInstallPath)

    $backupInfo = Find-MessengerBundleBackupPath -ResolvedInstallPath $ResolvedInstallPath
    if (-not $backupInfo) {
        return [pscustomobject]@{
            Success = $false
            Message = '未发现消息速览组件备份文件。'
        }
    }

    Copy-Item -LiteralPath $backupInfo.Path -Destination $backupInfo.RestorePath -Force

    $verifyInfo = Read-TextFileUtf8 -Path $backupInfo.RestorePath
    if ((-not $verifyInfo.Content.Contains($MessengerEnableOriginalText)) -or (-not $verifyInfo.Content.Contains($MessengerVisibilityOriginalText))) {
        throw '消息速览组件恢复后校验失败：未检测到原始文本特征。'
    }

    return [pscustomobject]@{
        Success = $true
        Message = '已恢复消息速览组件原始文件。'
        BackupPath = $backupInfo.Path
    }
}

function Invoke-Remove {
    param([string]$ResolvedInstallPath)

    $frameResult = Invoke-RemoveFramePatch -ResolvedInstallPath $ResolvedInstallPath
    $messengerResult = Invoke-RemoveMessengerPatch -ResolvedInstallPath $ResolvedInstallPath
    $backupPaths = @()

    foreach ($path in @($frameResult.BackupPath, $messengerResult.BackupPath)) {
        if ($path) {
            $backupPaths += $path
        }
    }

    $backupPaths = @($backupPaths | Select-Object -Unique)

    return [pscustomobject]@{
        Success = $true
        Message = '已完成移除。'
        BackupPath = if ($backupPaths.Count -gt 0) { $backupPaths[0] } else { $null }
        BackupPaths = $backupPaths
    }
}

function Invoke-Restore {
    param([string]$ResolvedInstallPath)

    $frameResult = Invoke-RestoreFramePatch -ResolvedInstallPath $ResolvedInstallPath
    $messengerResult = Invoke-RestoreMessengerPatch -ResolvedInstallPath $ResolvedInstallPath
    $backupPaths = @()

    foreach ($path in @($frameResult.BackupPath, $messengerResult.BackupPath)) {
        if ($path) {
            $backupPaths += $path
        }
    }

    $backupPaths = @($backupPaths | Select-Object -Unique)
    if ((-not $frameResult.Success) -and (-not $messengerResult.Success)) {
        return [pscustomobject]@{
            Success = $false
            Message = '未发现备份文件，无法恢复。建议重新安装飞书。'
            BackupPaths = @()
        }
    }

    return [pscustomobject]@{
        Success = $true
        Message = '已从备份恢复原始文件。'
        BackupPath = if ($backupPaths.Count -gt 0) { $backupPaths[0] } else { $null }
        BackupPaths = $backupPaths
    }
}

function Select-ActionInteractive {
    Write-Title '飞书“知识问答 / 消息速览”移除/恢复脚本'
    Write-Host '1. 移除（自动备份并禁用知识问答与消息速览）'
    Write-Host '2. 恢复（从备份恢复知识问答与消息速览）'
    $choice = Read-Host '请输入 1 或 2'

    switch ($choice) {
        '1' { return 'Remove' }
        '2' { return 'Restore' }
        default { throw '无效选择，脚本已退出。' }
    }
}

if (-not $Action) {
    $Action = Select-ActionInteractive
}

try {
    $resolvedInstallPath = Resolve-InstallPath -UserInstallPath $InstallPath
    Write-Info "飞书安装目录: $resolvedInstallPath"

    if ($Action -eq 'Remove') {
        $result = Invoke-Remove -ResolvedInstallPath $resolvedInstallPath
        Write-Success $result.Message
        $backupPaths = @($result.BackupPaths)
        if ($backupPaths.Count -eq 0 -and $result.BackupPath) {
            $backupPaths = @($result.BackupPath)
        }
        foreach ($backupPath in $backupPaths) {
            Write-WarnText "备份文件路径: $backupPath"
        }
        if ($backupPaths.Count -gt 0) {
            Write-WarnText '请妥善保留该备份文件，不要擅自删除。'
        }
        exit 0
    }

    $result = Invoke-Restore -ResolvedInstallPath $resolvedInstallPath
    if ($result.Success) {
        Write-Success $result.Message
        $backupPaths = @($result.BackupPaths)
        if ($backupPaths.Count -eq 0 -and $result.BackupPath) {
            $backupPaths = @($result.BackupPath)
        }
        foreach ($backupPath in $backupPaths) {
            Write-Info "使用的备份文件: $backupPath"
        }
        exit 0
    }

    Write-ErrorText $result.Message
    exit 1
}
catch [System.UnauthorizedAccessException] {
    Write-ErrorText '权限不足，请以管理员身份运行 PowerShell 后重试。'
    exit 1
}
catch {
    Write-ErrorText $_.Exception.Message
    exit 1
}
finally {
    if (-not $NoPause) {
        Write-Host ''
        Read-Host '按回车键退出'
    }
}




