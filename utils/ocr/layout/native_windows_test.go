//go:build windows && inkflow_onnx

package layout

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// TestDetectorRunsModel 是原生 Runtime 的烟雾测试：它验证模型可加载、输入名可解析，
// 并能对一张空白 RGB 图片完成一次推理。测试不依赖文字图片，因此不会将模型的版面
// 检测精度误当作集成正确性。
func TestDetectorRunsModel(t *testing.T) {
	if os.Getenv("INKFLOW_ONNX_SMOKE_TEST") != "1" {
		t.Skip("需要让 onnxruntime.dll 与测试可执行文件同目录；安装包构建后设置 INKFLOW_ONNX_SMOKE_TEST=1 可执行该烟雾测试")
	}
	modelPath, err := filepath.Abs(filepath.Join("..", "..", "..", "build", "package", "ocr", "pp_doclayout_s.onnx"))
	if err != nil {
		t.Fatalf("解析模型路径失败: %v", err)
	}
	detector, err := New(Options{ModelPath: modelPath, Threads: 1})
	if err != nil {
		t.Fatalf("创建检测器失败: %v", err)
	}
	defer detector.Close()

	result, err := detector.DetectImage(context.Background(), image.NewRGBA(image.Rect(0, 0, 48, 48)))
	if err != nil {
		t.Fatalf("执行 ONNX 推理失败: %v", err)
	}
	if result.Regions == nil {
		t.Fatal("检测器应返回非 nil 的区域切片")
	}
}
