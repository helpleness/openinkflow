[CmdletBinding()]
param(
    [ValidateSet('cpu', 'cuda', 'vulkan', 'auto')]
    [string]$Backend,
    [ValidateSet('auto', 'mingw', 'msvc')]
    [string]$Toolchain = 'auto',
    [string]$BuildDir = '',
    [string]$Generator = ''
)

$ErrorActionPreference = 'Stop'

$llamaRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $llamaRoot
$configPath = Join-Path $repoRoot 'config.yaml'

function Get-ConfigBackend {
    if (-not (Test-Path -LiteralPath $configPath)) {
        return 'cpu'
    }

    $yaml = Get-Content -LiteralPath $configPath -Raw
    $sectionMatch = [regex]::Match($yaml, '(?ms)^llm-local:\s*(.*?)(?=^\S|\z)')
    if (-not $sectionMatch.Success) {
        return 'cpu'
    }

    $backendMatch = [regex]::Match($sectionMatch.Groups[1].Value, '(?m)^\s+backend:\s*["'']?([A-Za-z0-9_-]+)')
    if ($backendMatch.Success) {
        return $backendMatch.Groups[1].Value.ToLowerInvariant()
    }
    return 'cpu'
}

function Test-CommandAvailable([string]$name) {
    return $null -ne (Get-Command $name -ErrorAction SilentlyContinue)
}

function Find-MSVCEnvironmentScript {
    $installRoots = @()
    $vswhereCandidates = @(
        'C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe',
        'D:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe'
    ) | Where-Object { Test-Path -LiteralPath $_ }

    foreach ($vswhere in $vswhereCandidates) {
        $installationPath = & $vswhere `
            -latest `
            -products '*' `
            -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 `
            -property installationPath
        if ($LASTEXITCODE -eq 0 -and $installationPath) {
            $installRoots += $installationPath.Trim()
        }
    }

    $installRoots += @(
        $env:VSINSTALLDIR,
        'D:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools',
        'C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools'
    )

    $candidates = $installRoots | Where-Object { $_ } | Select-Object -Unique | ForEach-Object {
        Join-Path $_ 'VC\Auxiliary\Build\vcvars64.bat'
    } | Where-Object { Test-Path -LiteralPath $_ }
    return $candidates | Select-Object -First 1
}

function Import-MSVCEnvironment {
    $vcvars = Find-MSVCEnvironmentScript
    if (-not $vcvars) {
        throw 'MSVC toolchain requested, but vcvars64.bat was not found. Install Visual Studio 2022 Build Tools with the Desktop C++ workload.'
    }

    $environmentLines = & $env:ComSpec /d /c "call `"$vcvars`" >nul && set"
    foreach ($line in $environmentLines) {
        $separator = $line.IndexOf('=')
        if ($separator -gt 0) {
            $name = $line.Substring(0, $separator)
            $value = $line.Substring($separator + 1)
            Set-Item -Path "Env:$name" -Value $value
        }
    }
    if (-not (Test-CommandAvailable 'cl.exe')) {
        throw "MSVC environment initialization failed: cl.exe is not on PATH after importing $vcvars."
    }
}

function Resolve-VulkanSDK {
    $command = Get-Command 'glslc.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
    $commandRoot = if ($command) { Split-Path -Parent (Split-Path -Parent $command.Source) } else { $null }
    $candidates = @(
        $env:VULKAN_SDK,
        $env:MSYSTEM_PREFIX,
        $commandRoot,
        'D:\msys64\ucrt64',
        'C:\msys64\ucrt64'
    ) | Where-Object { $_ } | Select-Object -Unique

    $sdkRoot = $candidates | Where-Object {
        (Test-Path -LiteralPath (Join-Path $_ 'bin\glslc.exe')) -and
        (Test-Path -LiteralPath (Join-Path $_ 'include\vulkan\vulkan.h'))
    } | Select-Object -First 1
    if (-not $sdkRoot) {
        throw 'Vulkan backend requested, but a Vulkan SDK with glslc and headers was not found. Install the Vulkan SDK or the MSYS2 ucrt64 vulkan-devel and shaderc packages.'
    }

    $env:VULKAN_SDK = $sdkRoot
    $env:PATH = "$(Join-Path $sdkRoot 'bin');$env:PATH"
    return $sdkRoot
}

function New-MSVCVulkanImportLibrary {
    param([Parameter(Mandatory = $true)][string]$OutputDirectory)

    $systemVulkan = Join-Path $env:WINDIR 'System32\vulkan-1.dll'
    $gendef = @(
        (Get-Command 'gendef.exe' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -ErrorAction SilentlyContinue),
        'D:\msys64\ucrt64\bin\gendef.exe',
        'C:\msys64\ucrt64\bin\gendef.exe'
    ) | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | Select-Object -First 1
    if (-not $gendef -or -not (Test-Path -LiteralPath $systemVulkan)) {
        throw 'Cannot create the MSVC Vulkan import library. gendef.exe or C:\Windows\System32\vulkan-1.dll is missing.'
    }

    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $definition = Join-Path $OutputDirectory 'vulkan-1.def'
    $library = Join-Path $OutputDirectory 'vulkan-1.lib'
    Push-Location $OutputDirectory
    try {
        & $gendef $systemVulkan | Out-Host
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $definition)) {
            throw 'gendef failed to create vulkan-1.def.'
        }
        & lib.exe "/def:$definition" '/machine:x64' "/out:$library" | Out-Host
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $library)) {
            throw 'MSVC lib.exe failed to create vulkan-1.lib.'
        }
    } finally {
        Pop-Location
    }
    return $library
}

function Resolve-Backend([string]$requested) {
    $value = $requested
    if ([string]::IsNullOrWhiteSpace($value)) {
        $value = Get-ConfigBackend
    }
    $value = $value.Trim().ToLowerInvariant()

    if ($value -eq 'auto') {
        $hasNvcc = Test-CommandAvailable 'nvcc'
        if (-not $hasNvcc -and $env:CUDA_PATH) {
            $hasNvcc = Test-Path (Join-Path $env:CUDA_PATH 'bin\nvcc.exe')
        }
        if ($hasNvcc) {
            return 'cuda'
        }
        if ($env:VULKAN_SDK -or (Test-CommandAvailable 'glslc')) {
            return 'vulkan'
        }
        return 'cpu'
    }

    if ($value -notin @('cpu', 'cuda', 'vulkan')) {
        throw "llm-local.backend must be cpu, cuda, vulkan, or auto; got '$value'."
    }
    return $value
}

$backend = Resolve-Backend $Backend
$msvcAvailable = $null -ne (Find-MSVCEnvironmentScript)
if ($Toolchain -eq 'auto') {
    $Toolchain = if ($msvcAvailable) { 'msvc' } else { 'mingw' }
}
if ($Toolchain -eq 'msvc') {
    Import-MSVCEnvironment
} elseif ($Toolchain -eq 'mingw' -and -not (Test-CommandAvailable 'gcc.exe')) {
    throw 'MinGW toolchain requested, but gcc.exe was not found on PATH.'
}
if ([string]::IsNullOrWhiteSpace($BuildDir)) {
    $directoryName = switch ($backend) {
        'cpu' { 'cmake-build-cpu' }
        'cuda' { 'cmake-build-cuda' }
        'vulkan' { 'cmake-build-vulkan' }
    }
    $BuildDir = Join-Path $llamaRoot $directoryName
} elseif (-not [IO.Path]::IsPathRooted($BuildDir)) {
    $BuildDir = Join-Path $repoRoot $BuildDir
}

if ($Toolchain -eq 'msvc') {
    # CMake --fresh resets the cache but leaves old linker outputs in place.
    # Remove only the known MinGW artifacts so validation cannot pass on files
    # left by a previous build with a different ABI.
    $staleMinGWArtifacts = @(
        'lib\libllama.a', 'lib\ggml.a', 'lib\ggml-base.a', 'lib\ggml-cpu.a',
        'lib\libllama.dll.a', 'lib\libggml.dll.a', 'lib\libggml-base.dll.a',
        'lib\libggml-cpu.dll.a', 'lib\libggml-vulkan.dll.a', 'bin\libllama.dll'
    )
    foreach ($relativePath in $staleMinGWArtifacts) {
        $artifactPath = Join-Path $BuildDir $relativePath
        if (Test-Path -LiteralPath $artifactPath) {
            Remove-Item -LiteralPath $artifactPath -Force
        }
    }
}

if ($backend -eq 'cuda') {
    # Prefer the upgraded toolkit when it is installed, even if this PowerShell
    # process inherited an older CUDA_PATH from a previously opened terminal.
    $cudaRoots = @(
        'C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3',
        $env:CUDA_PATH,
        'C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.9',
        'C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v12.8',
        'C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v11.8'
    ) | Where-Object { $_ -and (Test-Path -LiteralPath (Join-Path $_ 'bin\nvcc.exe')) }
    if (-not $cudaRoots) {
        throw 'CUDA backend requested, but no supported CUDA Toolkit installation with nvcc.exe was found.'
    }
    $cudaRoot = $cudaRoots[0]
    $cudaNvcc = Join-Path $cudaRoot 'bin\nvcc.exe'
    $env:CUDA_PATH = $cudaRoot
    $env:PATH = "$(Join-Path $cudaRoot 'bin');$env:PATH"
}

if ($backend -eq 'vulkan') {
    $vulkanRoot = Resolve-VulkanSDK
    if ($Toolchain -eq 'msvc') {
        $msvcVulkanImport = New-MSVCVulkanImportLibrary (Join-Path $BuildDir 'lib')
    }
}
$cmakeArgs = @(
    '-S', $llamaRoot,
    '-B', $BuildDir,
    '-DCMAKE_BUILD_TYPE=Release',
    '-DLLAMA_BUILD_SERVER=OFF',
    '-DLLAMA_BUILD_TESTS=OFF',
    '-DLLAMA_BUILD_EXAMPLES=OFF',
    '-DLLAMA_BUILD_TOOLS=OFF',
    '-DGGML_AVX2=ON',
    '-DGGML_FMA=ON',
    '-DGGML_CPU_REPACK=ON',
    '-DGGML_OPENMP=OFF',
    '-DGGML_CUDA=OFF',
    '-DGGML_VULKAN=OFF'
)

if ($backend -eq 'cpu') {
    $cmakeArgs += $(if ($Toolchain -eq 'msvc') { '-DBUILD_SHARED_LIBS=ON' } else { '-DBUILD_SHARED_LIBS=OFF' })
} else {
    $cmakeArgs += '-DBUILD_SHARED_LIBS=ON'
    if ($backend -eq 'cuda') {
        $cmakeArgs[$cmakeArgs.IndexOf('-DGGML_CUDA=OFF')] = '-DGGML_CUDA=ON'
    } else {
        $cmakeArgs[$cmakeArgs.IndexOf('-DGGML_VULKAN=OFF')] = '-DGGML_VULKAN=ON'
    }
}
if ($backend -eq 'cuda') {
    $cmakeArgs += "-DCMAKE_CUDA_COMPILER=$cudaNvcc"
    # Keep FindCUDAToolkit on the same version as nvcc. This is important on
    # machines that retain CUDA_PATH_V11_8 or stale CMake cache entries.
    $cmakeArgs += "-DCUDAToolkit_ROOT=$cudaRoot"
}
if ($backend -eq 'vulkan') {
    $cmakeArgs += "-DCMAKE_PREFIX_PATH=$vulkanRoot"
    if ($Toolchain -eq 'msvc') {
        $cmakeArgs += "-DVulkan_LIBRARY=$msvcVulkanImport"
    }
}
if ($env:CMAKE_RC_COMPILER) {
    $cmakeArgs += "-DCMAKE_RC_COMPILER=$env:CMAKE_RC_COMPILER"
}
if ($env:CMAKE_MT) {
    $cmakeArgs += "-DCMAKE_MT=$env:CMAKE_MT"
}
if ($Toolchain -eq 'msvc') {
    $cmakeArgs += '-DCMAKE_C_COMPILER=cl.exe'
    $cmakeArgs += '-DCMAKE_CXX_COMPILER=cl.exe'
}
if (-not [string]::IsNullOrWhiteSpace($Generator)) {
    $cmakeArgs = @('-G', $Generator) + $cmakeArgs
} elseif ($Toolchain -eq 'msvc') {
    $cmakeArgs = @('-G', 'Ninja') + $cmakeArgs
} elseif (-not (Test-Path -LiteralPath (Join-Path $BuildDir 'CMakeCache.txt')) -and $backend -in @('cpu', 'vulkan') -and (Test-Path -LiteralPath 'D:\mingw64\bin\gcc.exe')) {
    # CGo uses MinGW on Windows. Keep CPU and Vulkan import libraries ABI-compatible
    # with the final Go external link.
    $cmakeArgs = @('-G', 'MinGW Makefiles') + $cmakeArgs
}

Write-Host "InkFlow llama.cpp backend: $backend"
Write-Host "CMake toolchain: $Toolchain"
Write-Host "Build directory: $BuildDir"
Write-Host "CMake flags: GGML_OPENMP=OFF GGML_AVX2=ON GGML_FMA=ON GGML_CPU_REPACK=ON"
Write-Host "Go build tag: $(if ($backend -eq 'cpu') { '(none; CPU link)' } else { "inkflow_$backend" })"

$cachePath = Join-Path $BuildDir 'CMakeCache.txt'
$needsFreshConfigure = $false
if (Test-Path -LiteralPath $cachePath) {
    $cacheText = Get-Content -LiteralPath $cachePath -Raw
    $cachedMSVC = $cacheText -match '(?m)^CMAKE_CXX_COMPILER:[^=]+=.*(?:\\|/)cl\.exe'
    $cachedGenerator = [regex]::Match($cacheText, '(?m)^CMAKE_GENERATOR:INTERNAL=(.*)$').Groups[1].Value
    $generatorMismatch = if ($Generator) {
        $cachedGenerator -ne $Generator
    } elseif ($Toolchain -eq 'msvc') {
        $cachedGenerator -ne 'Ninja'
    } else {
        $false
    }
    if (($Toolchain -eq 'msvc') -ne $cachedMSVC -or $generatorMismatch) {
        $needsFreshConfigure = $true
    }
}
if ($needsFreshConfigure) {
    $cmakeArgs = @('--fresh') + $cmakeArgs
}

if ($backend -eq 'vulkan') {
    $shaderGeneratorPrefix = Join-Path $BuildDir 'llama.cpp\ggml\src\ggml-vulkan\vulkan-shaders-gen-prefix'
    $shaderGeneratorBuild = Join-Path $shaderGeneratorPrefix 'src\vulkan-shaders-gen-build'
    $shaderGeneratorCache = Join-Path $shaderGeneratorBuild 'CMakeCache.txt'
    if (Test-Path -LiteralPath $shaderGeneratorCache) {
        $nestedCache = Get-Content -LiteralPath $shaderGeneratorCache -Raw
        $nestedGenerator = [regex]::Match($nestedCache, '(?m)^CMAKE_GENERATOR:INTERNAL=(.*)$').Groups[1].Value
        $expectedGenerator = if ($Generator) { $Generator } elseif ($Toolchain -eq 'msvc') { 'Ninja' } else { 'MinGW Makefiles' }
        if ($nestedGenerator -ne $expectedGenerator) {
            & cmake -E remove_directory $shaderGeneratorPrefix
            if ($LASTEXITCODE -ne 0) {
                throw "Failed to reset stale Vulkan shader generator directory: $shaderGeneratorPrefix."
            }
        }
    } elseif (Test-Path -LiteralPath $shaderGeneratorPrefix) {
        & cmake -E remove_directory $shaderGeneratorPrefix
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to reset incomplete Vulkan shader generator directory: $shaderGeneratorPrefix."
        }
    }
}

& cmake @cmakeArgs
if ($LASTEXITCODE -ne 0) {
    throw "CMake configure failed with exit code $LASTEXITCODE."
}

& cmake --build $BuildDir --config Release --parallel
if ($LASTEXITCODE -ne 0) {
    throw "CMake build failed with exit code $LASTEXITCODE."
}

if ($backend -eq 'vulkan' -and $Toolchain -eq 'msvc') {
    # The restored CGo line uses -lvulkan-1. Keep a MinGW import library next
    # to the MSVC import libraries so Go's external linker can resolve it.
    $vulkanImport = Join-Path $vulkanRoot 'lib\libvulkan-1.dll.a'
    if (Test-Path -LiteralPath $vulkanImport) {
        Copy-Item -LiteralPath $vulkanImport -Destination (Join-Path $BuildDir 'lib\libvulkan-1.dll.a') -Force
    } else {
        throw "Vulkan MinGW import library is missing: $vulkanImport."
    }
}

$requiredArtifacts = switch ($backend) {
    'cpu' {
        if ($Toolchain -eq 'msvc') {
            @('lib\llama.lib', 'lib\ggml.lib', 'lib\ggml-base.lib', 'lib\ggml-cpu.lib', 'bin\llama.dll', 'bin\ggml.dll', 'bin\ggml-base.dll', 'bin\ggml-cpu.dll')
        } else {
            @('lib\libllama.a', 'lib\ggml.a', 'lib\ggml-base.a', 'lib\ggml-cpu.a')
        }
    }
    'vulkan' {
        if ($Toolchain -eq 'msvc') {
            @('lib\llama.lib', 'lib\ggml.lib', 'lib\ggml-base.lib', 'lib\ggml-cpu.lib', 'lib\ggml-vulkan.lib', 'lib\libvulkan-1.dll.a', 'bin\llama.dll', 'bin\ggml.dll', 'bin\ggml-base.dll', 'bin\ggml-cpu.dll', 'bin\ggml-vulkan.dll')
        } else {
        @(
            'lib\libllama.dll.a', 'lib\libggml.dll.a', 'lib\libggml-base.dll.a',
            'lib\libggml-cpu.dll.a', 'lib\libggml-vulkan.dll.a',
            'bin\libllama.dll', 'bin\ggml.dll', 'bin\ggml-base.dll',
            'bin\ggml-cpu.dll', 'bin\ggml-vulkan.dll'
        )
        }
    }
    'cuda' {
        @(
            'lib\llama.lib', 'lib\ggml.lib', 'lib\ggml-base.lib',
            'lib\ggml-cpu.lib', 'lib\ggml-cuda.lib',
            'bin\llama.dll', 'bin\ggml.dll', 'bin\ggml-base.dll',
            'bin\ggml-cpu.dll', 'bin\ggml-cuda.dll'
        )
    }
}
$missingArtifacts = @($requiredArtifacts | Where-Object { -not (Test-Path -LiteralPath (Join-Path $BuildDir $_)) })
if ($missingArtifacts.Count -gt 0) {
    throw "Backend build completed without required $backend artifacts: $($missingArtifacts -join ', ')."
}

$cachePath = Join-Path $BuildDir 'CMakeCache.txt'
if (Test-Path -LiteralPath $cachePath) {
    Write-Host 'Verified CMake cache:'
    Select-String -LiteralPath $cachePath -Pattern '^(GGML_(OPENMP|AVX2|FMA|CPU_REPACK|CUDA|VULKAN):|BUILD_SHARED_LIBS:)' | ForEach-Object { Write-Host "  $($_.Line)" }
}

Write-Host "Backend build completed. Use CGO_ENABLED=1 and build with $(if ($backend -eq 'cpu') { 'no extra Go tag' } else { "-tags inkflow_$backend" })."
