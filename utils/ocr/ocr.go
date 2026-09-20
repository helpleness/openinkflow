// Package ocr 是应用配置与本地版面检测器之间的轻量入口。
//
// 实际 ONNX 推理实现位于 layout 子包；这里不保存全局状态，也不参与任务队列，调用方按
// 业务生命周期创建并关闭检测器即可。
package ocr

import (
	"fmt"

	"InkFlow/config"
	"InkFlow/utils/ocr/layout"
)

// NewLayoutDetector 根据 config.yaml 中的 ocr 配置创建本地检测器。
//
// ModelPath 为空时会自动使用桌面安装目录下的 ocr/pp_doclayout_s.onnx；服务端部署可在
// YAML 中设置 ocr.model-path 指向已部署的模型文件。
func NewLayoutDetector(settings config.OCR) (*layout.Detector, error) {
	if !settings.Enabled {
		return nil, fmt.Errorf("本地 ONNX 版面检测器已在配置中禁用")
	}
	return layout.New(layout.Options{
		ModelPath:      settings.ModelPath,
		ScoreThreshold: settings.ScoreThreshold,
		Threads:        settings.Threads,
	})
}
