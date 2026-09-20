param(
    # Destination 是最终安装包内的 ocr 目录，不是用户数据目录。
    [string]$Destination = "build\package\ocr",
    [switch]$Force
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$runtimeVersion = "1.23.2"
$runtimeRoot = Join-Path $repoRoot "third_party\onnxruntime\windows_amd64"
$cacheDirectory = Join-Path $repoRoot ".cache\downloads"
$runtimeArchive = Join-Path $cacheDirectory "onnxruntime-win-x64-$runtimeVersion.zip"
$runtimeURL = "https://github.com/microsoft/onnxruntime/releases/download/v$runtimeVersion/onnxruntime-win-x64-$runtimeVersion.zip"

# 该模型是 PP-DocLayout-S 的量化 ONNX 导出版本，Apache-2.0。它直接返回文字、表格、
# 标题等版面区域；不包含 PaddleX/PaddleOCR 运行时或 Python 依赖。
$modelName = "pp_doclayout_s.onnx"
$modelURL = "https://huggingface.co/stefanj0/PP-DocLayout-S-ONNX/resolve/main/pp_doclayout_s.onnx?download=true"
$modelSHA256 = "33688DBEE1C23E34B81777E97CB428EB40F24B242C02B5F623484959E830AEC8"

function Get-SHA256 {
    param([Parameter(Mandatory = $true)][string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToUpperInvariant()
}

function Invoke-Download {
    param(
        [Parameter(Mandatory = $true)][string]$Url,
        [Parameter(Mandatory = $true)][string]$Path
    )
    Write-Host "Downloading $Url"
    Invoke-WebRequest -Uri $Url -OutFile $Path
}

function Assert-RuntimeFiles {
    param([Parameter(Mandatory = $true)][string]$Root)
    $required = @(
        (Join-Path $Root "include\onnxruntime_c_api.h"),
        (Join-Path $Root "lib\onnxruntime.lib"),
        (Join-Path $Root "lib\onnxruntime.dll")
    )
    $missing = @($required | Where-Object { -not (Test-Path -LiteralPath $_) })
    if ($missing.Count -gt 0) {
        throw "ONNX Runtime SDK is incomplete. Missing: $($missing -join ', ')"
    }
}

New-Item -ItemType Directory -Force -Path $cacheDirectory | Out-Null

if ($Force -and (Test-Path -LiteralPath $runtimeRoot)) {
    # 删除目标是固定的第三方 Runtime 目录，避免误删工作区其他内容。
    Remove-Item -LiteralPath $runtimeRoot -Recurse -Force
}
if (-not (Test-Path -LiteralPath $runtimeRoot)) {
    if (-not (Test-Path -LiteralPath $runtimeArchive)) {
        Invoke-Download -Url $runtimeURL -Path $runtimeArchive
    }
    $extractRoot = Join-Path $cacheDirectory "onnxruntime-$runtimeVersion"
    if (Test-Path -LiteralPath $extractRoot) {
        Remove-Item -LiteralPath $extractRoot -Recurse -Force
    }
    Expand-Archive -LiteralPath $runtimeArchive -DestinationPath $extractRoot -Force
    $expandedRoot = Get-ChildItem -LiteralPath $extractRoot -Directory | Select-Object -First 1
    if (-not $expandedRoot) {
        throw "ONNX Runtime archive has an unexpected layout: $runtimeArchive"
    }
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $runtimeRoot) | Out-Null
    Copy-Item -LiteralPath $expandedRoot.FullName -Destination $runtimeRoot -Recurse -Force
}
Assert-RuntimeFiles -Root $runtimeRoot

$destinationRoot = if ([IO.Path]::IsPathRooted($Destination)) {
    [IO.Path]::GetFullPath($Destination)
} else {
    [IO.Path]::GetFullPath((Join-Path $repoRoot $Destination))
}
New-Item -ItemType Directory -Force -Path $destinationRoot | Out-Null
$modelPath = Join-Path $destinationRoot $modelName
if ($Force -or -not (Test-Path -LiteralPath $modelPath) -or (Get-SHA256 -Path $modelPath) -ne $modelSHA256) {
    $temporaryModel = "$modelPath.download"
    if (Test-Path -LiteralPath $temporaryModel) {
        Remove-Item -LiteralPath $temporaryModel -Force
    }
    Invoke-Download -Url $modelURL -Path $temporaryModel
    if ((Get-SHA256 -Path $temporaryModel) -ne $modelSHA256) {
        Remove-Item -LiteralPath $temporaryModel -Force
        throw "PP-DocLayout-S download checksum verification failed."
    }
    Move-Item -LiteralPath $temporaryModel -Destination $modelPath -Force
}

$manifest = [ordered]@{
    engine = "onnxruntime"
    runtime_version = $runtimeVersion
    model = [ordered]@{
        name = "PP-DocLayout-S"
        file = $modelName
        sha256 = $modelSHA256.ToLowerInvariant()
        source = "https://huggingface.co/stefanj0/PP-DocLayout-S-ONNX"
        labels = @("text", "table", "title", "image", "formula", "chart")
    }
}
$manifestPath = Join-Path $destinationRoot "manifest.json"
$manifest | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $manifestPath -Encoding utf8

Write-Host "ONNX Runtime: $runtimeRoot"
Write-Host "Layout model: $modelPath"
