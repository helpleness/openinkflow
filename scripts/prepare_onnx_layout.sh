#!/usr/bin/env bash

# 下载 Linux x86_64 ONNX Runtime C/C++ SDK 和 PP-DocLayout-S 模型。
# 该脚本只操作仓库内 third_party、.cache 和 ocr 目录；可被 build_priv.sh 与 Dockerfile
# 复用。Windows 桌面构建仍使用同名的 PowerShell 脚本和 MSVC DLL，不走这里。
set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
readonly RUNTIME_VERSION="1.23.2"
readonly RUNTIME_ARCHIVE="onnxruntime-linux-x64-${RUNTIME_VERSION}.tgz"
readonly RUNTIME_URL="https://github.com/microsoft/onnxruntime/releases/download/v${RUNTIME_VERSION}/${RUNTIME_ARCHIVE}"
readonly MODEL_NAME="pp_doclayout_s.onnx"
readonly MODEL_URL="https://huggingface.co/stefanj0/PP-DocLayout-S-ONNX/resolve/main/pp_doclayout_s.onnx?download=true"
readonly MODEL_SHA256="33688dbee1c23e34b81777e97cb428eb40f24b242c02b5f623484959e830aec8"

runtime_dir="$PROJECT_DIR/third_party/onnxruntime/linux_amd64"
model_dir="$PROJECT_DIR/ocr"
force=false

usage() {
  cat <<'EOF'
Usage: bash scripts/prepare_onnx_layout.sh [--runtime-dir DIR] [--model-dir DIR] [--force]
EOF
}

while (($# > 0)); do
  case "$1" in
    --runtime-dir)
      runtime_dir="$2"
      shift 2
      ;;
    --model-dir)
      model_dir="$2"
      shift 2
      ;;
    --force)
      force=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf '未知参数：%s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf '缺少命令：%s\n' "$1" >&2
    exit 1
  }
}

for command in curl tar sha256sum mktemp; do
  require_command "$command"
done

mkdir -p "$(dirname "$runtime_dir")" "$(dirname "$model_dir")"
runtime_dir="$(cd "$(dirname "$runtime_dir")" && pwd)/$(basename "$runtime_dir")"
model_dir="$(cd "$(dirname "$model_dir")" && pwd)/$(basename "$model_dir")"
cache_dir="$PROJECT_DIR/.cache/downloads"
archive_path="$cache_dir/$RUNTIME_ARCHIVE"
model_path="$model_dir/$MODEL_NAME"

mkdir -p "$cache_dir" "$runtime_dir" "$model_dir"

runtime_ready() {
  [[ -f "$runtime_dir/include/onnxruntime_c_api.h" && -f "$runtime_dir/lib/libonnxruntime.so" ]]
}

if [[ "$force" == true ]] || ! runtime_ready; then
  if [[ "$force" == true ]] || [[ ! -f "$archive_path" ]]; then
    printf '下载 ONNX Runtime Linux SDK：%s\n' "$RUNTIME_URL"
    curl --fail --location --retry 3 --retry-delay 2 --output "$archive_path.part" "$RUNTIME_URL"
    mv -f "$archive_path.part" "$archive_path"
  fi

  extract_dir="$(mktemp -d)"
  trap 'rm -rf "$extract_dir"' EXIT
  tar -xzf "$archive_path" -C "$extract_dir"
  extracted_root="$(find "$extract_dir" -mindepth 1 -maxdepth 1 -type d -print -quit)"
  [[ -n "$extracted_root" ]] || {
    printf 'ONNX Runtime 压缩包目录结构无效：%s\n' "$archive_path" >&2
    exit 1
  }
  [[ -f "$extracted_root/include/onnxruntime_c_api.h" && -f "$extracted_root/lib/libonnxruntime.so" ]] || {
    printf 'ONNX Runtime SDK 内容不完整：%s\n' "$archive_path" >&2
    exit 1
  }
  cp -a "$extracted_root/." "$runtime_dir/"
fi

if [[ -f "$model_path" ]]; then
  current_sha="$(sha256sum "$model_path" | awk '{print tolower($1)}')"
else
  current_sha=""
fi
if [[ "$force" == true ]] || [[ "$current_sha" != "$MODEL_SHA256" ]]; then
  printf '下载 PP-DocLayout-S ONNX 模型\n'
  curl --fail --location --retry 3 --retry-delay 2 --output "$model_path.part" "$MODEL_URL"
  downloaded_sha="$(sha256sum "$model_path.part" | awk '{print tolower($1)}')"
  [[ "$downloaded_sha" == "$MODEL_SHA256" ]] || {
    rm -f "$model_path.part"
    printf 'PP-DocLayout-S 校验和不匹配。\n' >&2
    exit 1
  }
  mv -f "$model_path.part" "$model_path"
fi

printf '{\n  "engine": "onnxruntime",\n  "runtime_version": "%s",\n  "model": {\n    "name": "PP-DocLayout-S",\n    "file": "%s",\n    "sha256": "%s"\n  }\n}\n' \
  "$RUNTIME_VERSION" "$MODEL_NAME" "$MODEL_SHA256" >"$model_dir/manifest.json"

printf 'ONNX Runtime: %s\n模型: %s\n' "$runtime_dir" "$model_path"
