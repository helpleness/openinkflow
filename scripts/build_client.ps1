param(
    [string]$Output = "build\InkFlow.exe",
    [string]$InferenceProvider = "",
    [ValidateSet("cpu", "cuda", "vulkan", "auto")]
    [string]$Backend = "",
    [switch]$SkipBackendBuild,
    [switch]$Clean
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot

function Build-WindowsIconResource {
    $windresCandidates = @(
        (Get-Command windres.exe -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -ErrorAction SilentlyContinue),
        "D:\mingw64\bin\windres.exe"
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | Select-Object -First 1
    if (-not $windresCandidates) {
        throw "windres.exe was not found. Install MinGW-w64 so the Windows application icon can be compiled."
    }

    $resourceSource = Join-Path $PSScriptRoot "inkflow-icon.rc"
    $resourceOutput = Join-Path $repoRoot "desktop_icon_windows_amd64.syso"
    & $windresCandidates -O coff -I $repoRoot -i $resourceSource -o $resourceOutput
    if ($LASTEXITCODE -ne 0) {
        throw "Windows application icon resource compilation failed."
    }
}

function Resolve-CommandPath {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$InstallHint
    )

    $command = Get-Command $Name -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $command) {
        throw "$Name was not found on PATH. $InstallHint"
    }
    return $command.Source
}

function Resolve-GoCommand {
    $pathCommand = Get-Command "go.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($pathCommand) {
        return $pathCommand.Source
    }

    $candidates = @()
    if ($env:GOROOT) {
        $candidates += Join-Path $env:GOROOT "bin\go.exe"
    }
    if ($env:ProgramFiles) {
        $candidates += Join-Path $env:ProgramFiles "Go\bin\go.exe"
    }
    $toolchainRoot = Join-Path $HOME "go\pkg\mod\golang.org"
    if (Test-Path -LiteralPath $toolchainRoot) {
        $candidates += Get-ChildItem -LiteralPath $toolchainRoot -Filter "toolchain@*" -Directory -ErrorAction SilentlyContinue |
            Sort-Object LastWriteTime -Descending |
            ForEach-Object { Join-Path $_.FullName "bin\go.exe" }
    }

    $goCommand = $candidates | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | Select-Object -First 1
    if (-not $goCommand) {
        throw "go.exe was not found. Install Go and restart the terminal."
    }
    return $goCommand
}

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

function Get-ConfiguredBackend {
    $configPath = Join-Path $repoRoot "config.yaml"
    if (-not (Test-Path -LiteralPath $configPath)) {
        return "cpu"
    }

    $configText = Get-Content -LiteralPath $configPath -Raw
    $sectionMatch = [regex]::Match($configText, '(?ms)^llm-local:\s*(.*?)(?=^\S|\z)')
    if ($sectionMatch.Success) {
        $backendMatch = [regex]::Match($sectionMatch.Groups[1].Value, '(?m)^\s+backend:\s*["'']?([A-Za-z0-9_-]+)')
        if ($backendMatch.Success) {
            return $backendMatch.Groups[1].Value.Trim().ToLowerInvariant()
        }
    }
    return "cpu"
}

function Resolve-Backend {
    param([string]$RequestedBackend)

    $value = $RequestedBackend.Trim().ToLowerInvariant()
    if (-not $value) {
        $value = Get-ConfiguredBackend
    }
    if ($value -eq "auto") {
        $hasNvcc = $null -ne (Get-Command "nvcc.exe" -ErrorAction SilentlyContinue)
        if (-not $hasNvcc -and $env:CUDA_PATH) {
            $hasNvcc = Test-Path -LiteralPath (Join-Path $env:CUDA_PATH "bin\nvcc.exe")
        }
        if ($hasNvcc) {
            return "cuda"
        }

        $vulkanRoots = @($env:VULKAN_SDK, $env:MSYSTEM_PREFIX, "D:\msys64\ucrt64", "C:\msys64\ucrt64") |
            Where-Object { $_ -and (Test-Path -LiteralPath (Join-Path $_ "bin\glslc.exe")) }
        if ($vulkanRoots -or (Get-Command "glslc.exe" -ErrorAction SilentlyContinue)) {
            return "vulkan"
        }
        return "cpu"
    }
    if ($value -notin @("cpu", "cuda", "vulkan")) {
        throw "Unsupported llama.cpp backend '$value'. Use cpu, cuda, vulkan, or auto."
    }
    return $value
}

function Assert-BackendArtifacts {
    param([Parameter(Mandatory = $true)][string]$SelectedBackend)

    $buildDir = Join-Path $repoRoot "llama\cmake-build-$SelectedBackend"
    $requiredFiles = switch ($SelectedBackend) {
        "cpu" {
            @(
                "lib\llama.lib", "lib\ggml.lib", "lib\ggml-base.lib", "lib\ggml-cpu.lib",
                "bin\llama.dll", "bin\ggml.dll", "bin\ggml-base.dll", "bin\ggml-cpu.dll"
            )
        }
        "vulkan" {
            @(
                "lib\llama.lib", "lib\ggml.lib", "lib\ggml-base.lib",
                "lib\ggml-cpu.lib", "lib\ggml-vulkan.lib", "lib\libvulkan-1.dll.a",
                "bin\llama.dll", "bin\ggml.dll", "bin\ggml-base.dll",
                "bin\ggml-cpu.dll", "bin\ggml-vulkan.dll"
            )
        }
        "cuda" {
            @(
                "lib\llama.lib", "lib\ggml.lib", "lib\ggml-base.lib",
                "lib\ggml-cpu.lib", "lib\ggml-cuda.lib",
                "bin\llama.dll", "bin\ggml.dll", "bin\ggml-base.dll",
                "bin\ggml-cpu.dll", "bin\ggml-cuda.dll"
            )
        }
    }
    $missing = @($requiredFiles | Where-Object { -not (Test-Path -LiteralPath (Join-Path $buildDir $_)) })
    if ($missing.Count -gt 0) {
        throw "The $SelectedBackend llama.cpp backend is incomplete. Missing: $($missing -join ', '). Run llama\build_windows_backend.ps1 -Backend $SelectedBackend."
    }
}

function Sync-BackendRuntimeFiles {
    param(
        [Parameter(Mandatory = $true)][string]$SelectedBackend,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    $knownRuntimeFiles = @(
        "llama.dll", "libllama.dll", "ggml.dll", "ggml-base.dll", "ggml-cpu.dll",
        "ggml-vulkan.dll", "ggml-cuda.dll", "libgcc_s_seh-1.dll",
        "libstdc++-6.dll", "libwinpthread-1.dll", "concrt140.dll",
        "msvcp140.dll", "msvcp140_1.dll", "msvcp140_2.dll",
        "msvcp140_atomic_wait.dll", "msvcp140_codecvt_ids.dll",
        "vccorlib140.dll", "vcruntime140.dll", "vcruntime140_1.dll",
        "vcruntime140_threads.dll"
    )
    foreach ($fileName in $knownRuntimeFiles) {
        $stalePath = Join-Path $Destination $fileName
        if (Test-Path -LiteralPath $stalePath) {
            Remove-Item -LiteralPath $stalePath -Force
        }
    }

    $runtimeDir = Join-Path $repoRoot "llama\cmake-build-$SelectedBackend\bin"
    $runtimeFiles = @(Get-ChildItem -LiteralPath $runtimeDir -Filter "*.dll" -File)
    if ($runtimeFiles.Count -eq 0) {
        throw "No runtime DLLs were produced for the $SelectedBackend backend in $runtimeDir."
    }
    Copy-Item -LiteralPath $runtimeFiles.FullName -Destination $Destination -Force

    $redistRoots = @(
    $env:VCToolsRedistDir,
    "D:\VS2022BuildTools\VC\Redist\MSVC",
    "D:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Redist\MSVC",
        "C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Redist\MSVC"
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_) }
    $redistDirectories = foreach ($root in $redistRoots) {
        if (Test-Path -LiteralPath (Join-Path $root "msvcp140.dll")) {
            $root
        } else {
            Get-ChildItem -LiteralPath $root -Directory -ErrorAction SilentlyContinue |
                Where-Object { $_.Name -match '^\d+\.\d+' } |
                Sort-Object Name -Descending |
                ForEach-Object { Join-Path $_.FullName "x64\Microsoft.VC143.CRT" }
        }
    }
    $redistDirectory = $redistDirectories |
        Where-Object { Test-Path -LiteralPath (Join-Path $_ "msvcp140.dll") } |
        Select-Object -First 1
    if (-not $redistDirectory) {
        throw "The MSVC x64 redistributable runtime was not found. Repair Visual Studio 2022 Build Tools and include the C++ runtime."
    }
    Get-ChildItem -LiteralPath $redistDirectory -Filter "*.dll" -File |
        ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $Destination -Force }
}

$InferenceProvider = Resolve-InferenceProvider $InferenceProvider
$Backend = Resolve-Backend $Backend
$npmCommand = Resolve-CommandPath "npm.cmd" "Install Node.js (including npm) and restart the terminal."
$goCommand = Resolve-GoCommand
$libraryDir = Join-Path $repoRoot "third_party\usearch\windows_amd64"
$library = Join-Path $libraryDir "libusearch_c.a"
$header = Join-Path $libraryDir "usearch.h"
if (-not (Test-Path -LiteralPath $library) -or -not (Test-Path -LiteralPath $header)) {
    throw "USearch static library is missing. Run scripts/setup_usearch.ps1 first."
}

if (-not $SkipBackendBuild) {
    & (Join-Path $repoRoot "llama\build_windows_backend.ps1") -Backend $Backend -Toolchain msvc
    if ($LASTEXITCODE -ne 0) {
        throw "$Backend llama.cpp backend build failed."
    }
}
Assert-BackendArtifacts $Backend

# Keep all build caches on the project drive instead of the system drive.
$cacheRoot = Join-Path $repoRoot ".cache"
$env:TEMP = Join-Path $cacheRoot "tmp"
$env:TMP = $env:TEMP
$env:GOMODCACHE = Join-Path $cacheRoot "go\pkg\mod"
$env:GOCACHE = Join-Path $cacheRoot "go\build"
$env:npm_config_cache = Join-Path $cacheRoot "npm"
foreach ($path in @($env:TEMP, $env:GOMODCACHE, $env:GOCACHE, $env:npm_config_cache)) {
    New-Item -ItemType Directory -Force -Path $path | Out-Null
}

# desktop_main.go embeds web/dist, so build the frontend before compiling Go.
$webDir = Join-Path $repoRoot "web"
Push-Location $webDir
try {
    # A release must use the versions pinned in package-lock.json. This also
    # prevents stale node_modules from being silently embedded in the desktop app.
    & $npmCommand ci --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) {
        throw "Frontend dependency installation failed. Stop any Vite/Node process using web/node_modules and try again."
    }

    & $npmCommand run build
    if ($LASTEXITCODE -ne 0) {
        throw "Frontend build failed."
    }

    $frontendEntry = Join-Path $webDir "dist\index.html"
    $frontendAssets = Join-Path $webDir "dist\assets"
    if (-not (Test-Path -LiteralPath $frontendEntry) -or -not (Test-Path -LiteralPath $frontendAssets)) {
        throw "Frontend build did not produce web/dist/index.html and web/dist/assets."
    }

    $frontendHtml = Get-Content -LiteralPath $frontendEntry -Raw
    $runtimeOffset = $frontendHtml.IndexOf('/inkflow-runtime.js')
    $entryOffset = $frontendHtml.IndexOf('src="/assets/index-')
    if ($runtimeOffset -lt 0 -or $entryOffset -lt 0 -or $runtimeOffset -gt $entryOffset) {
        throw "Frontend runtime settings are missing or load after the application entrypoint."
    }
} finally {
    Pop-Location
}

$outputPath = Join-Path $repoRoot $Output
$outputDir = Split-Path -Parent $outputPath
if ($outputDir) {
    New-Item -ItemType Directory -Force -Path $outputDir | Out-Null
}
$desktopConfigTemplate = Join-Path $repoRoot "config.client.yaml"
$desktopConfigOutput = Join-Path $outputDir "config.yaml"
if (-not (Test-Path -LiteralPath $desktopConfigTemplate)) {
    throw "Desktop config template is missing: $desktopConfigTemplate"
}

# 原生文档版面检测器使用 ONNX Runtime C API。该脚本只下载 C++ Runtime 与一个量化
# ONNX 模型，不会创建或打包 Python/PaddleX 环境。
& (Join-Path $PSScriptRoot "prepare_onnx_layout.ps1") -Destination (Join-Path $outputDir "ocr")
if ($LASTEXITCODE -ne 0) {
    throw "ONNX layout runtime preparation failed."
}
$onnxRuntimeDir = Join-Path $repoRoot "third_party\onnxruntime\windows_amd64"
$onnxRuntimeDLL = Join-Path $onnxRuntimeDir "lib\onnxruntime.dll"
if (-not (Test-Path -LiteralPath $onnxRuntimeDLL)) {
    throw "ONNX Runtime DLL is missing: $onnxRuntimeDLL"
}
& (Join-Path $PSScriptRoot "build_onnx_layout.ps1") -Destination $outputDir
if ($LASTEXITCODE -ne 0) {
    throw "MSVC ONNX layout bridge build failed."
}

$env:CGO_ENABLED = "1"
$env:CGO_CFLAGS = "-I$libraryDir"
$env:CGO_LDFLAGS = "-L$libraryDir -lusearch_c -lstdc++ -static-libgcc -static-libstdc++ -lwinpthread"

Push-Location $repoRoot
try {
	Build-WindowsIconResource

    # Wails requires the production tag for a manual release build. The
    # inference provider is linked into the desktop defaults, then exposed to
    # the embedded frontend through /inkflow-runtime.js at startup.
    # CGO flags and source changes participate in Go's build cache key, so a
    # normal build safely rebuilds affected packages while avoiding a full
    # recompilation on every installer build.
    if ($Clean) {
        & $goCommand clean -cache
        if ($LASTEXITCODE -ne 0) {
            throw "Go build cache cleanup failed."
        }
    }
    $goTags = @("desktop", "production", "inkflow_onnx")
    if ($Backend -ne "cpu") {
        $goTags += "inkflow_$Backend"
    }
    $goLdflags = "-H windowsgui -X InkFlow/core.desktopInferenceProvider=$InferenceProvider -X InkFlow/core.desktopBackend=$Backend"
    & $goCommand build -buildvcs=false -tags ($goTags -join " ") -ldflags $goLdflags -o $outputPath .
    if ($LASTEXITCODE -ne 0) {
        throw "Client build failed for the $Backend backend."
    }
} finally {
    Pop-Location
}

Sync-BackendRuntimeFiles -SelectedBackend $Backend -Destination $outputDir
Copy-Item -LiteralPath $onnxRuntimeDLL -Destination $outputDir -Force
$backendMarker = [IO.Path]::ChangeExtension($outputPath, ".backend")
Set-Content -LiteralPath $backendMarker -Value $Backend -Encoding Ascii

# The release configuration intentionally excludes development database
# credentials. Expand the backend marker while keeping inference settings
# editable after installation.
$desktopConfig = Get-Content -LiteralPath $desktopConfigTemplate -Raw
if (-not $desktopConfig.Contains("__INKFLOW_BACKEND__") -or -not $desktopConfig.Contains("__INKFLOW_GPU_LAYERS__")) {
    throw "Desktop config template does not contain all required markers."
}
$gpuLayers = if ($Backend -eq "cpu") { "0" } else { "-1" }
$desktopConfig = $desktopConfig.Replace("__INKFLOW_BACKEND__", $Backend).Replace("__INKFLOW_GPU_LAYERS__", $gpuLayers)
Set-Content -LiteralPath $desktopConfigOutput -Value $desktopConfig -Encoding utf8

Write-Host "Client created: $outputPath (inference provider: $InferenceProvider, backend: $Backend, OCR engine: ONNX Runtime)"
