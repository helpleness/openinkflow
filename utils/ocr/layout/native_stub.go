//go:build (!windows && !linux) || !inkflow_onnx

package layout

import "errors"

type unavailableDetector struct{}

func newNativeDetector(_ string, _ int) (nativeDetector, error) {
	return nil, errors.New("本地 ONNX 版面检测器仅在启用 inkflow_onnx 的 Windows 或 Linux 构建中可用")
}

func (unavailableDetector) detect(_ []byte, _, _ int, _ float32) ([]Region, error) { return nil, nil }
func (unavailableDetector) close()                                                 {}
