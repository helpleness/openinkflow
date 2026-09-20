#!/usr/bin/env bash

# 构建 Linux 服务器用的 C++ ONNX Runtime 桥接库。Windows 客户端必须继续使用
# build_onnx_layout.ps1，通过 MSVC 输出 InkFlowLayout.dll；不要在 Linux 上尝试运行 MSVC。
set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
readonly SOURCE_DIR="$PROJECT_DIR/utils/ocr/layout/native"
readonly RUNTIME_DIR="$PROJECT_DIR/third_party/onnxruntime/linux_amd64"
readonly MODEL_DIR="$PROJECT_DIR/ocr"
readonly OUTPUT_DIR="$PROJECT_DIR/lib"
readonly BUILD_DIR="$PROJECT_DIR/build/native/onnx-layout-linux"

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf '缺少命令：%s\n' "$1" >&2
    exit 1
  }
}

for command in bash g++; do
  require_command "$command"
done

bash "$SCRIPT_DIR/prepare_onnx_layout.sh" --runtime-dir "$RUNTIME_DIR" --model-dir "$MODEL_DIR"

for required in \
  "$SOURCE_DIR/layout_engine.cpp" \
  "$SOURCE_DIR/layout_engine.h" \
  "$RUNTIME_DIR/include/onnxruntime_c_api.h" \
  "$RUNTIME_DIR/lib/libonnxruntime.so" \
  "$MODEL_DIR/pp_doclayout_s.onnx"; do
  [[ -f "$required" ]] || {
    printf 'ONNX 版面检测构建依赖缺失：%s\n' "$required" >&2
    exit 1
  }
done

mkdir -p "$OUTPUT_DIR" "$BUILD_DIR"
temporary_library="$BUILD_DIR/libInkFlowLayout.so"

g++ -std=c++17 -fPIC -O2 -shared \
  -I"$RUNTIME_DIR/include" \
  -I"$SOURCE_DIR" \
  "$SOURCE_DIR/layout_engine.cpp" \
  -L"$RUNTIME_DIR/lib" \
  -Wl,-z,origin -Wl,-rpath,'$ORIGIN' \
  -lonnxruntime \
  -o "$temporary_library"

cp -f "$temporary_library" "$OUTPUT_DIR/libInkFlowLayout.so"
find "$RUNTIME_DIR/lib" -maxdepth 1 -type f -name 'libonnxruntime*.so*' -exec cp -f {} "$OUTPUT_DIR/" \;
find "$RUNTIME_DIR/lib" -maxdepth 1 -type l -name 'libonnxruntime*.so*' -exec cp -a {} "$OUTPUT_DIR/" \;

printf 'Linux ONNX 版面桥接库：%s/libInkFlowLayout.so\n' "$OUTPUT_DIR"
