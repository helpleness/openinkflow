[CmdletBinding()]
param(
    # Force 会重新下载模型并重建 DLL；日常 GoLand 启动无需传入。
    [switch]$Force
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$packageDirectory = Join-Path $repoRoot "build\package"
$ocrDirectory = Join-Path $packageDirectory "ocr"
$runtimeDLL = Join-Path $repoRoot "third_party\onnxruntime\windows_amd64\lib\onnxruntime.dll"
$layoutDLL = Join-Path $packageDirectory "InkFlowLayout.dll"
$layoutSourceDirectory = Join-Path $repoRoot "utils\ocr\layout\native"

function Invoke-CheckedScript {
    param(
        [Parameter(Mandatory = $true)][string]$ScriptPath,
        [Parameter(Mandatory = $true)][object[]]$Arguments
    )

    # 子脚本可能完全由 PowerShell cmdlet 构成，未必会写入 LASTEXITCODE；
    # 每次调用前重置它，才能把脚本中真正失败的原生命令识别为失败。
    $global:LASTEXITCODE = 0
    & $ScriptPath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Preparation script failed: $ScriptPath"
    }
}

function Test-LayoutBridgeRebuildRequired {
    param(
        [Parameter(Mandatory = $true)][string]$OutputDLL,
        [Parameter(Mandatory = $true)][string[]]$SourceFiles
    )

    if (-not (Test-Path -LiteralPath $OutputDLL)) {
        return $true
    }

    $outputTime = (Get-Item -LiteralPath $OutputDLL).LastWriteTimeUtc
    foreach ($sourceFile in $SourceFiles) {
        if ((Get-Item -LiteralPath $sourceFile).LastWriteTimeUtc -gt $outputTime) {
            return $true
        }
    }
    return $false
}

New-Item -ItemType Directory -Force -Path $packageDirectory | Out-Null

$prepareArguments = @("-Destination", $ocrDirectory)
if ($Force) {
    $prepareArguments += "-Force"
}
Invoke-CheckedScript -ScriptPath (Join-Path $PSScriptRoot "prepare_onnx_layout.ps1") -Arguments $prepareArguments

$bridgeSources = @(
    (Join-Path $layoutSourceDirectory "layout_engine.cpp"),
    (Join-Path $layoutSourceDirectory "layout_engine.h")
)
if ($Force -or (Test-LayoutBridgeRebuildRequired -OutputDLL $layoutDLL -SourceFiles $bridgeSources)) {
    Invoke-CheckedScript -ScriptPath (Join-Path $PSScriptRoot "build_onnx_layout.ps1") -Arguments @("-Destination", $packageDirectory)
} else {
    Write-Host "MSVC ONNX layout bridge is current: $layoutDLL"
}

if (-not (Test-Path -LiteralPath $runtimeDLL)) {
    throw "ONNX Runtime DLL is missing after preparation: $runtimeDLL"
}
Copy-Item -LiteralPath $runtimeDLL -Destination (Join-Path $packageDirectory "onnxruntime.dll") -Force

Write-Host "GoLand ONNX layout prerequisites are ready."
