#!/usr/bin/env bash
set -Eeuo pipefail

echo "=> 1. 创建本地依赖库目录 ./lib"
mkdir -p lib

echo "=> 2. 收集 llama.cpp 动态库到 ./lib"
find ./llama/cmake-build-release -name "*.so*" -exec cp {} ./lib/ \;

echo "=> 3. 检查并收集 USearch 动态库到 ./lib"
USEARCH_HEADER="/usr/local/include/usearch.h"
USEARCH_LIBRARY="/usr/local/lib/libusearch_c.so"
if [ ! -f "$USEARCH_HEADER" ] || [ ! -f "$USEARCH_LIBRARY" ]; then
  echo "错误：未找到 Linux 版 USearch 依赖。"
  echo "请先安装 $USEARCH_HEADER 和 $USEARCH_LIBRARY，然后重新执行构建。"
  exit 1
fi
cp -a "$USEARCH_LIBRARY" ./lib/

echo "=> 4. 构建 Linux ONNX 文档版面检测桥接库"
# Linux 服务端使用本机 g++ 编译 libInkFlowLayout.so。Windows 桌面客户端仍由
# build_onnx_layout.ps1 使用 MSVC 生成 InkFlowLayout.dll，两者不能混用。
bash ./scripts/build_onnx_layout.sh

echo "=> 5. 开始编译 Go 主程序"
CGO_ENABLED=1 go build -tags inkflow_onnx -o InkFlow main.go

echo "=> ✅ 编译成功！现在你可以使用 ./start.sh 启动程序了。"
