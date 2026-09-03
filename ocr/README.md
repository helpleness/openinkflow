# 本地文档版面检测模型

Windows 桌面构建会通过 `scripts/prepare_onnx_layout.ps1` 下载量化的
`PP-DocLayout-S` ONNX 模型到安装包的 `ocr/pp_doclayout_s.onnx`，并用 MSVC 构建
`InkFlowLayout.dll`。Linux 服务端则由 `scripts/prepare_onnx_layout.sh` 下载同一模型
和 Linux ONNX Runtime，再由 `scripts/build_onnx_layout.sh` 用服务器本机 `g++` 构建
`libInkFlowLayout.so`。

它的职责仅是图片版面检测：输出文字、表格、标题、公式、图片和图表等区域的类别、
置信度和边界框。它不执行文字转写，因此不包含 Python、PaddleX、PaddleOCR 或
PP-Structure 运行时。

实际推理由 `utils/ocr/layout` 中的 C++ ONNX Runtime 桥接完成。模型来源、校验和与
运行时版本会写入同目录的 `manifest.json`。Linux 部署只需运行 `build_priv.sh`；它会
自动调用上述两个 Shell 脚本，不需要也不能在 Linux 服务器上安装 MSVC。
