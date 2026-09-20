param(
    [string]$Version = "0.1.0",
    [string]$InferenceProvider = "",
    [ValidateSet("cpu", "cuda", "vulkan", "auto")]
    [string]$Backend = "",
    [string]$CudaVersion = "",
    [switch]$SkipBackendBuild,
    [switch]$SkipClientBuild
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$packageDir = Join-Path $repoRoot "build\package"
$installerScript = Join-Path $repoRoot "installer\InkFlow.iss"
$setupExe = Join-Path $packageDir "InkFlow.exe"
$backendMarker = Join-Path $packageDir "InkFlow.backend"

function Resolve-InferenceProvider {
    param([string]$RequestedProvider)

    $provider = $RequestedProvider.Trim().ToLowerInvariant()
    if (-not $provider) {
        $configPath = Join-Path $repoRoot "config.yaml"
        if (Test-Path -LiteralPath $configPath) {
            $configText = Get-Content -LiteralPath $configPath -Raw
            $match = [regex]::Match($configText, '(?m)^\s*inference-provider:\s*["'']?([^\s#"'']+)')
            if ($match.Success) {
                $provider = $match.Groups[1].Value.Trim().ToLowerInvariant()
            }
        }
    }
    if (-not $provider) {
        $provider = "local"
    }
    if ($provider -notin @("local", "frontend")) {
        throw "Unsupported inference provider '$provider'. Use local or frontend."
    }
    return $provider
}

function Get-PackageName {
    param(
        [Parameter(Mandatory = $true)][string]$Provider,
        [Parameter(Mandatory = $true)][string]$SelectedBackend,
        [string]$SelectedCudaVersion
    )

    if ($Provider -eq "frontend") {
        return "InkFlow-Setup-$Version-frontend-webgpu-webview2-x64"
    }
    switch ($SelectedBackend) {
        "cpu" {
            return "InkFlow-Setup-$Version-local-cpu-msvc-bundled-x64"
        }
        "cuda" {
            return "InkFlow-Setup-$Version-local-cuda$SelectedCudaVersion-nvidia-driver-x64"
        }
        "vulkan" {
            return "InkFlow-Setup-$Version-local-vulkan-driver-x64"
        }
        default {
            throw "Unsupported packaged backend '$SelectedBackend'."
        }
    }
}

function Assert-CudaToolkitVersion {
    param([Parameter(Mandatory = $true)][string]$ExpectedVersion)

    $cachePath = Join-Path $repoRoot "llama\cmake-build-cuda\CMakeCache.txt"
    $actualVersion = ""
    if (Test-Path -LiteralPath $cachePath) {
        $cacheText = Get-Content -LiteralPath $cachePath -Raw
        $rootMatch = [regex]::Match($cacheText, '(?m)^CUDAToolkit_ROOT[^=]*=(.+)$')
        if ($rootMatch.Success) {
            $rootName = Split-Path -Leaf $rootMatch.Groups[1].Value.Trim().TrimEnd([char[]]@('\', '/'))
            if ($rootName -match '^v(\d+\.\d+)$') {
                $actualVersion = $Matches[1]
            }
        }
    }
    if (-not $actualVersion) {
        throw "Cannot determine the CUDA Toolkit version from $cachePath. Rebuild the CUDA backend before creating the installer."
    }
    if ($actualVersion -ne $ExpectedVersion) {
        throw "CUDA backend was built with Toolkit $actualVersion, but -CudaVersion $ExpectedVersion was requested. Rebuild with CUDA $ExpectedVersion."
    }
}

function Find-InnoSetupCompiler {
    $candidates = @(
        (Get-Command ISCC.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -ErrorAction SilentlyContinue),
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
        "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
        "D:\MYgopro\Tools\Inno Setup 6\ISCC.exe",
        "D:\MYgopro\Tools\Inno Setup 6\Inno Setup 6\ISCC.exe"
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | Select-Object -First 1

    if ($candidates.Count -eq 0) {
        throw "Inno Setup 6 was not found. Install it first, then run this script again."
    }
    return $candidates
}

if (-not $SkipClientBuild) {
    & (Join-Path $PSScriptRoot "build_client.ps1") `
        -Output "build\package\InkFlow.exe" `
        -InferenceProvider $InferenceProvider `
        -Backend $Backend `
        -SkipBackendBuild:$SkipBackendBuild
    if ($LASTEXITCODE -ne 0) {
        throw "Client build failed."
    }
}

if (-not (Test-Path -LiteralPath $setupExe)) {
    throw "Client executable is missing: $setupExe. Run without -SkipClientBuild or provide build\\package\\InkFlow.exe."
}
if (-not (Test-Path -LiteralPath $backendMarker)) {
    throw "Client backend marker is missing: $backendMarker. Rebuild the client before creating the installer."
}
$onnxRuntime = Join-Path $packageDir "onnxruntime.dll"
$onnxBridge = Join-Path $packageDir "InkFlowLayout.dll"
$ocrModel = Join-Path $packageDir "ocr\pp_doclayout_s.onnx"
$ocrManifest = Join-Path $packageDir "ocr\manifest.json"
foreach ($requiredOCRPath in @($onnxRuntime, $onnxBridge, $ocrModel, $ocrManifest)) {
    if (-not (Test-Path -LiteralPath $requiredOCRPath)) {
        throw "ONNX layout runtime is missing: $requiredOCRPath. Rebuild the client package."
    }
}
$packagedBackend = (Get-Content -LiteralPath $backendMarker -Raw).Trim().ToLowerInvariant()
if ($Backend -and $Backend -ne "auto" -and $packagedBackend -ne $Backend) {
    throw "The packaged client uses backend '$packagedBackend', but installer backend '$Backend' was requested. Rebuild the client."
}
if ($packagedBackend -notin @("cpu", "cuda", "vulkan")) {
    throw "The packaged client has unsupported backend marker '$packagedBackend'."
}

$provider = Resolve-InferenceProvider $InferenceProvider
$CudaVersion = $CudaVersion.Trim()
if ($packagedBackend -eq "cuda") {
    if (-not $CudaVersion) {
        throw "-CudaVersion is required for a CUDA installer, for example -CudaVersion 13.3."
    }
    if ($CudaVersion -notmatch '^\d+\.\d+$') {
        throw "CudaVersion must look like a CUDA toolkit version such as 13.3."
    }
    Assert-CudaToolkitVersion $CudaVersion
} elseif ($CudaVersion) {
    throw "-CudaVersion can only be used when the packaged backend is cuda."
}

$compiler = Find-InnoSetupCompiler
$env:INKFLOW_VERSION = $Version
$packageName = Get-PackageName -Provider $provider -SelectedBackend $packagedBackend -SelectedCudaVersion $CudaVersion
$env:INKFLOW_PACKAGE_NAME = $packageName
& $compiler $installerScript
if ($LASTEXITCODE -ne 0) {
    throw "Inno Setup build failed."
}

$output = Join-Path $repoRoot "build\installer\$packageName.exe"
Write-Host "Installer created: $output (provider: $provider, backend: $packagedBackend)"
