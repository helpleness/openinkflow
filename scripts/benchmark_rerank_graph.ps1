param(
    [string]$ModelPath = "$env:LOCALAPPDATA\InkFlow\models\bge-reranker-v2-m3-Q4_K_M.gguf",
    [string]$Query = "main world",
    [string]$Document = "The main world is the highest-level world in the setting.",
    [int]$Runs = 3,
    [int]$Warmup = 1,
    [int]$Threads = 8,
    [int]$ContextSize = 1024,
    [string]$OutputDir = "build\bench\rerank_graph"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $ModelPath -PathType Leaf)) {
    throw "Rerank model not found: $ModelPath"
}

$gxx = (Get-Command g++.exe -ErrorAction SilentlyContinue).Source
if (-not $gxx) {
    $candidate = "D:\mingw64\bin\g++.exe"
    if (Test-Path -LiteralPath $candidate) {
        $gxx = $candidate
    }
}
if (-not $gxx) {
    throw "g++.exe was not found. Install MinGW-w64 or add it to PATH."
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$llamaRoot = Join-Path $repoRoot "llama\llama.cpp"
$buildRoot = Join-Path $repoRoot "llama\cmake-build-release\llama.cpp"
$source = Join-Path $PSScriptRoot "benchmark_rerank_graph.cpp"
$exe = Join-Path $repoRoot "build\bench\benchmark_rerank_graph.exe"
$output = Join-Path $repoRoot $OutputDir

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $exe), $output | Out-Null

$includeArgs = @(
    "-I$($llamaRoot)\include",
    "-I$($llamaRoot)\ggml\include"
)
$libraryArgs = @(
    "-L$($buildRoot)\src",
    "-L$($buildRoot)\ggml\src",
    "-Wl,--start-group",
    "-l:libllama.a",
    "-l:ggml.a",
    "-l:ggml-base.a",
    "-l:ggml-cpu.a",
    "-Wl,--end-group",
    "-lgomp",
    "-lwinpthread",
    "-lstdc++"
)

& $gxx -std=c++17 -O2 @includeArgs $source @libraryArgs -o $exe
if ($LASTEXITCODE -ne 0) {
    throw "Failed to compile benchmark (exit code $LASTEXITCODE)."
}

& $exe `
    --model $ModelPath `
    --query $Query `
    --document $Document `
    --runs $Runs `
    --warmup $Warmup `
    --threads $Threads `
    --context $ContextSize `
    --output-dir $output
if ($LASTEXITCODE -ne 0) {
    throw "Benchmark failed (exit code $LASTEXITCODE)."
}

$dot = (Get-Command dot.exe -ErrorAction SilentlyContinue).Source
if (-not $dot) {
    $candidate = "C:\Program Files\Graphviz\bin\dot.exe"
    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
        $dot = $candidate
    }
}

if ($dot) {
    $graphDot = Join-Path $output "graph.dot"
    $layersDot = Join-Path $output "layers.dot"
    $graphSvg = Join-Path $output "graph.svg"
    $layersSvg = Join-Path $output "layers.svg"
    $layersPng = Join-Path $output "layers.png"

    & $dot -Tsvg $graphDot -o $graphSvg
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to render full compute graph SVG (exit code $LASTEXITCODE)."
    }
    & $dot -Tsvg $layersDot -o $layersSvg
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to render layer graph SVG (exit code $LASTEXITCODE)."
    }
    & $dot -Tpng $layersDot -o $layersPng
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to render layer graph PNG (exit code $LASTEXITCODE)."
    }

    Write-Output "graph_svg: $graphSvg"
    Write-Output "layers_svg: $layersSvg"
    Write-Output "layers_png: $layersPng"
} else {
    Write-Warning "Graphviz dot.exe was not found; DOT files were exported but SVG files were not rendered."
}
