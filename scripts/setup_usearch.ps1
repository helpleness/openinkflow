param()

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$libraryDir = Join-Path $repoRoot "third_party\usearch\windows_amd64"
$library = Join-Path $libraryDir "libusearch_c.a"
$header = Join-Path $libraryDir "usearch.h"

if (-not (Test-Path -LiteralPath $library) -or -not (Test-Path -LiteralPath $header)) {
    throw @"
USearch static library is missing.
Expected files:
  $library
  $header

Restore the repository's third_party/usearch directory before building the client.
"@
}

Write-Host "USearch Windows static library is ready: $library"
