package layout

// nativeDetector 隔离 Go 业务层与具体的原生实现。Windows 客户端和 Linux 服务端都会
// 使用 ONNX Runtime C++ 实现；未启用 inkflow_onnx 标签的平台使用清晰的占位错误，
// 保证普通 go test 不会意外依赖本机 C++ SDK。
type nativeDetector interface {
	detect(rgb []byte, width, height int, scoreThreshold float32) ([]Region, error)
	close()
}
