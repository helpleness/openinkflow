param(
    # Destination 是 InkFlowLayout.dll 的输出目录，通常为 build/package。
    [string]$Destination = "build\package"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$sourceDirectory = Join-Path $repoRoot "utils\ocr\layout\native"
$runtimeDirectory = Join-Path $repoRoot "third_party\onnxruntime\windows_amd64"
$buildDirectory = Join-Path $repoRoot "build\native\onnx-layout"
$outputDirectory = if ([IO.Path]::IsPathRooted($Destination)) {
    [IO.Path]::GetFullPath($Destination)
} else {
    [IO.Path]::GetFullPath((Join-Path $repoRoot $Destination))
}

function Find-MSVCEnvironmentScript {
    $installRoots = @()
    foreach ($vswhere in @(
        "C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe",
        "D:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe"
    )) {
        if (Test-Path -LiteralPath $vswhere) {
            $installationPath = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
            if ($LASTEXITCODE -eq 0 -and $installationPath) {
                $installRoots += $installationPath.Trim()
            }
        }
    }
    $installRoots += @(
        $env:VSINSTALLDIR,
        "D:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools",
        "C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools"
    )
    return $installRoots |
        Where-Object { $_ } |
        Select-Object -Unique |
        ForEach-Object { Join-Path $_ "VC\Auxiliary\Build\vcvars64.bat" } |
        Where-Object { Test-Path -LiteralPath $_ } |
        Select-Object -First 1
}

function Import-MSVCEnvironment {
    $vcvars = Find-MSVCEnvironmentScript
    if (-not $vcvars) {
        throw "MSVC toolchain requested, but vcvars64.bat was not found. Install Visual Studio 2022 Build Tools with the Desktop C++ workload."
    }
    $environmentLines = & $env:ComSpec /d /c "call `"$vcvars`" >nul && set"
    foreach ($line in $environmentLines) {
        $separator = $line.IndexOf('=')
        if ($separator -gt 0) {
            Set-Item -Path "Env:$($line.Substring(0, $separator))" -Value $line.Substring($separator + 1)
        }
    }
    if (-not (Get-Command cl.exe -ErrorAction SilentlyContinue)) {
        throw "MSVC environment initialization failed: cl.exe is not available after loading $vcvars."
    }
}

$requiredInputs = @(
    (Join-Path $sourceDirectory "layout_engine.cpp"),
    (Join-Path $sourceDirectory "layout_engine.h"),
    (Join-Path $runtimeDirectory "include\onnxruntime_c_api.h"),
    (Join-Path $runtimeDirectory "lib\onnxruntime.lib"),
    (Join-Path $runtimeDirectory "lib\onnxruntime.dll")
)
$missingInputs = @($requiredInputs | Where-Object { -not (Test-Path -LiteralPath $_) })
if ($missingInputs.Count -gt 0) {
    throw "ONNX layout bridge prerequisites are missing: $($missingInputs -join ', '). Run scripts\prepare_onnx_layout.ps1 first."
}

Import-MSVCEnvironment
New-Item -ItemType Directory -Force -Path $buildDirectory, $outputDirectory | Out-Null
$objectFile = Join-Path $buildDirectory "layout_engine.obj"
$outputDLL = Join-Path $outputDirectory "InkFlowLayout.dll"
$outputImportLibrary = Join-Path $buildDirectory "InkFlowLayout.lib"

# 单独构建为 MSVC DLL：ONNX Runtime 的官方 .lib、C++ 标准库和异常处理都由同一套
# 工具链负责。Go 侧通过 Windows 动态加载调用纯 C ABI，不与既有 MinGW CGO 组件混链。
& cl.exe /nologo /utf-8 /std:c++17 /EHsc /O2 /MD /DORT_DLL_IMPORT `
    "/I$($runtimeDirectory)\include" `
    "/I$sourceDirectory" `
    /LD "/Fo$objectFile" $([IO.Path]::Combine($sourceDirectory, "layout_engine.cpp")) `
    /link "/OUT:$outputDLL" "/IMPLIB:$outputImportLibrary" "/LIBPATH:$($runtimeDirectory)\lib" onnxruntime.lib
if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $outputDLL)) {
    throw "MSVC ONNX layout bridge build failed."
}

Write-Host "MSVC ONNX layout bridge: $outputDLL"
