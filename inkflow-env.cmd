@echo off
set "ROOT=%~dp0"

if /I "%~1"=="activate" goto activate
if /I "%~1"=="install" goto install
if /I "%~1"=="run" goto run
if /I "%~1"=="build" goto build
if /I "%~1"=="test" goto test
if /I "%~1"=="test-image" goto test_image
if /I "%~1"=="help" goto usage
goto usage

:activate
if not "%~2"=="" set "INKFLOW_BACKEND=%~2"
call :set_process_env %2
exit /b %errorlevel%

:install
call :set_process_env --quiet
if errorlevel 1 exit /b %errorlevel%
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$root = [IO.Path]::GetFullPath('%ROOT%');" ^
  "$required = @('D:\mingw64\bin', (Join-Path $root 'llama\cmake-build-cpu\bin'), (Join-Path $root 'llama\cmake-build-cuda\bin'), (Join-Path $root 'llama\cmake-build-cuda\dll'), (Join-Path $root 'llama\cmake-build-vulkan\bin'), (Join-Path $root 'llama\cmake-build-vulkan\dll'), (Join-Path $root 'third_party\LiteRT-LM\bazel-bin\c'), (Join-Path $root 'third_party\LiteRT-LM\prebuilt\windows_x86_64'));" ^
  "$current = [Environment]::GetEnvironmentVariable('Path', 'User');" ^
  "$paths = @($current -split ';' | Where-Object { $_ -and $_.Trim() });" ^
  "foreach ($path in $required) { if (-not ($paths | Where-Object { $_.TrimEnd('\') -ieq $path.TrimEnd('\') })) { $paths += $path } };" ^
  "[Environment]::SetEnvironmentVariable('Path', ($paths -join ';'), 'User');" ^
  "[Environment]::SetEnvironmentVariable('CGO_ENABLED', '1', 'User');" ^
  "[Environment]::SetEnvironmentVariable('GOCACHE', (Join-Path $root '.gocache'), 'User');"
if errorlevel 1 exit /b %errorlevel%
echo Installed InkFlow CPU environment variables for the current Windows user.
echo Restart GoLand so its existing run configuration inherits the updated PATH.
exit /b 0

:run
if not "%~2"=="" set "INKFLOW_BACKEND=%~2"
call :set_process_env
if errorlevel 1 exit /b %errorlevel%
call :set_go_tags
go run %GO_TAGS% .
exit /b %errorlevel%

:build
if not "%~2"=="" set "INKFLOW_BACKEND=%~2"
call :set_process_env
if errorlevel 1 exit /b %errorlevel%
call :set_go_tags
go build %GO_TAGS% -buildvcs=false ./...
exit /b %errorlevel%

:test
call :prepare_litertlm_cpu
if errorlevel 1 exit /b %errorlevel%
call :set_process_env
if errorlevel 1 exit /b %errorlevel%
go test -exec "%ROOT%with-dev-env.cmd" -tags litertlm ./utils/litertlm
exit /b %errorlevel%

:test_image
call :prepare_litertlm_cpu
if errorlevel 1 exit /b %errorlevel%
call :set_process_env
if errorlevel 1 exit /b %errorlevel%
set "LITERTLM_RUN_IMAGE_TEST=1"
if "%LITERTLM_MODEL%"=="" set "LITERTLM_MODEL=%ROOT%model\litertlm\gemma-4-E4B-it.litertlm"
if not exist "%ROOT%.gocache" mkdir "%ROOT%.gocache"
set "TEST_EXE=%ROOT%.gocache\litertlm-image.test.exe"
go test -c -tags litertlm ./utils/litertlm -o "%TEST_EXE%"
if errorlevel 1 exit /b %errorlevel%
"%TEST_EXE%" -test.run TestGemma4E4BImageInference -test.v
exit /b %errorlevel%

:set_process_env
set "CGO_ENABLED=1"
if "%INKFLOW_BACKEND%"=="" set "INKFLOW_BACKEND=cpu"
if "%GOCACHE%"=="" set "GOCACHE=%ROOT%.gocache"
set "PATH=D:\mingw64\bin;%ROOT%third_party\LiteRT-LM\bazel-bin\c;%ROOT%third_party\LiteRT-LM\prebuilt\windows_x86_64;%PATH%"
if /I "%INKFLOW_BACKEND%"=="cpu" set "PATH=%ROOT%llama\cmake-build-cpu\bin;%ROOT%llama\cmake-build-cpu\dll;%PATH%"
if /I "%INKFLOW_BACKEND%"=="cuda" set "PATH=%ROOT%llama\cmake-build-cuda\bin;%ROOT%llama\cmake-build-cuda\dll;%ROOT%llama\cmake-build-cuda\dll\Release;%PATH%"
if /I "%INKFLOW_BACKEND%"=="vulkan" set "PATH=%ROOT%llama\cmake-build-vulkan\bin;%ROOT%llama\cmake-build-vulkan\dll;%ROOT%llama\cmake-build-vulkan\dll\Release;%PATH%"
if /I "%INKFLOW_BACKEND%"=="cuda" set "PATH=C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3\bin;%PATH%"
if /I "%INKFLOW_BACKEND%"=="vulkan" if not "%VULKAN_SDK%"=="" set "PATH=%VULKAN_SDK%\Bin;%PATH%"
if /I "%~1"=="--quiet" exit /b 0
echo CGO_ENABLED=%CGO_ENABLED%
echo GOCACHE=%GOCACHE%
echo INKFLOW_BACKEND=%INKFLOW_BACKEND%
echo Added runtime DLL paths:
echo   D:\mingw64\bin
if /I "%INKFLOW_BACKEND%"=="cpu" echo   %ROOT%llama\cmake-build-cpu\bin
if /I "%INKFLOW_BACKEND%"=="cpu" echo   %ROOT%llama\cmake-build-cpu\dll
if /I "%INKFLOW_BACKEND%"=="cuda" echo   %ROOT%llama\cmake-build-cuda\bin
if /I "%INKFLOW_BACKEND%"=="cuda" echo   %ROOT%llama\cmake-build-cuda\dll
if /I "%INKFLOW_BACKEND%"=="cuda" echo   %ROOT%llama\cmake-build-cuda\dll\Release
if /I "%INKFLOW_BACKEND%"=="vulkan" echo   %ROOT%llama\cmake-build-vulkan\bin
if /I "%INKFLOW_BACKEND%"=="vulkan" echo   %ROOT%llama\cmake-build-vulkan\dll
if /I "%INKFLOW_BACKEND%"=="vulkan" echo   %ROOT%llama\cmake-build-vulkan\dll\Release
if /I "%INKFLOW_BACKEND%"=="cuda" echo   C:\Program Files\NVIDIA GPU Computing Toolkit\CUDA\v13.3\bin
echo   %ROOT%third_party\LiteRT-LM\bazel-bin\c
echo   %ROOT%third_party\LiteRT-LM\prebuilt\windows_x86_64
exit /b 0

:set_go_tags
set "GO_TAGS="
if /I "%INKFLOW_BACKEND%"=="cuda" set "GO_TAGS=-tags inkflow_cuda"
if /I "%INKFLOW_BACKEND%"=="vulkan" set "GO_TAGS=-tags inkflow_vulkan"
exit /b 0

:prepare_litertlm_cpu
set "LINK=%ROOT%third_party\LiteRT-LM\bazel-bin"
set "CPU_IMPORT_LIB=%LINK%\c\engine_cpu_dll.if.lib"
if exist "%CPU_IMPORT_LIB%" exit /b 0
if exist "%LINK%" (
    echo LiteRT-LM bazel-bin exists but the CPU import library is missing:
    echo   %CPU_IMPORT_LIB%
    exit /b 1
)
if "%LITERTLM_BAZEL_BIN%"=="" set "LITERTLM_BAZEL_BIN=D:\bzl\execroot\litert_lm\bazel-out\x64_windows-opt\bin"
if not exist "%LITERTLM_BAZEL_BIN%\c\engine_cpu_dll.if.lib" (
    echo LiteRT-LM CPU build output was not found:
    echo   %LITERTLM_BAZEL_BIN%\c\engine_cpu_dll.if.lib
    echo Build //c:engine_cpu_dll first or set LITERTLM_BAZEL_BIN to its Bazel bin directory.
    exit /b 1
)
mklink /J "%LINK%" "%LITERTLM_BAZEL_BIN%" >nul
if errorlevel 1 (
    echo Failed to create LiteRT-LM bazel-bin junction:
    echo   %LINK%
    exit /b 1
)
echo Restored LiteRT-LM CPU bazel-bin junction:
echo   %LINK%
echo   -^> %LITERTLM_BAZEL_BIN%
exit /b 0

:usage
echo InkFlow Windows CPU environment helper
echo.
echo Usage:
echo   call inkflow-env.cmd activate [cpu^|cuda^|vulkan]  Set variables in the current cmd session
echo   inkflow-env.cmd install        Persist variables for GoLand and new terminals
echo   inkflow-env.cmd run [cpu^|cuda^|vulkan]    Run the InkFlow backend
echo   inkflow-env.cmd build [cpu^|cuda^|vulkan]  Build all Go packages
echo   inkflow-env.cmd test           Run LiteRT-LM CPU package tests
echo   inkflow-env.cmd test-image     Run the LiteRT-LM CPU image test
exit /b 1
