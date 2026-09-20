// Package layout 提供本地文档版面检测能力。
//
// 它基于 PP-DocLayout-S 的 ONNX 模型识别图片中的文字、表格、标题、公式和图片区域。
// 这里不做 OCR 文字转写，也不调用 Python；模型推理由同目录的 C++ ONNX Runtime 桥接完成。
package layout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // 注册 GIF 解码器。
	_ "image/jpeg" // 注册 JPEG 解码器。
	_ "image/png"  // 注册 PNG 解码器。
	"os"
	"path/filepath"
	"sync"
)

const (
	defaultScoreThreshold float32 = 0.30
	defaultThreads                = 2
)

// Region 是模型识别出的一个版面区域。坐标以输入图片左上角为原点，单位为像素。
type Region struct {
	ClassID int     `json:"class_id"`
	Class   string  `json:"class"`
	Score   float32 `json:"score"`
	Left    float32 `json:"left"`
	Top     float32 `json:"top"`
	Right   float32 `json:"right"`
	Bottom  float32 `json:"bottom"`
}

// Result 是一张图片的版面检测结果。HasText 与 HasTable 便于调用方快速做分流，
// Regions 则保留完整结果，供后续的文档解析或页面标注使用。
type Result struct {
	HasText  bool     `json:"has_text"`
	HasTable bool     `json:"has_table"`
	Regions  []Region `json:"regions"`
}

// Options 定义检测器的本地运行参数。
type Options struct {
	// ModelPath 是 ONNX 模型文件路径；为空时使用程序目录 ocr/pp_doclayout_s.onnx。
	ModelPath string
	// ScoreThreshold 会过滤置信度低于该值的区域；默认值为 0.30。
	ScoreThreshold float32
	// Threads 是 ONNX Runtime 的 CPU 推理线程数；默认值为 2。
	Threads int
}

// Detector 持有一个可复用的本地 ONNX Runtime 会话。Detector 可以被多个 goroutine
// 并发调用；底层 Runtime 会自行调度实际计算。
type Detector struct {
	mu             sync.RWMutex
	native         nativeDetector
	scoreThreshold float32
}

// New 创建一个本地版面检测器。模型文件不存在时直接返回错误，避免业务请求到来后才暴露
// 安装包不完整的问题。
func New(options Options) (*Detector, error) {
	modelPath := options.ModelPath
	if modelPath == "" {
		modelPath = DefaultModelPath()
	}
	modelPath, err := filepath.Abs(modelPath)
	if err != nil {
		return nil, fmt.Errorf("解析 ONNX 模型路径失败: %w", err)
	}
	if info, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("本地版面检测模型不可用: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("本地版面检测模型路径不是文件: %s", modelPath)
	}

	threshold := options.ScoreThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = defaultScoreThreshold
	}
	threads := options.Threads
	if threads <= 0 {
		threads = defaultThreads
	}

	native, err := newNativeDetector(modelPath, threads)
	if err != nil {
		return nil, err
	}
	return &Detector{native: native, scoreThreshold: threshold}, nil
}

// DefaultModelPath 返回随桌面安装包发布的默认模型位置。服务端部署应通过 Options.ModelPath
// 指向其自己的模型目录，避免依赖当前工作目录。
func DefaultModelPath() string {
	// GoLand 的 `go run` 二进制位于临时目录，无法依靠 os.Executable 推导仓库内的
	// 测试模型位置。开发运行配置可通过该环境变量指向 build/package 中已准备好的模型；
	// 正式桌面安装与 Linux 服务端均无需设置它。
	if configuredPath := os.Getenv("INKFLOW_ONNX_LAYOUT_MODEL"); configuredPath != "" {
		return configuredPath
	}
	if executable, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(executable), "ocr", "pp_doclayout_s.onnx")
	}
	return filepath.Join("ocr", "pp_doclayout_s.onnx")
}

// DetectFile 读取并检测单张图片。调用方可以取消尚未开始的请求；已进入 ONNX Runtime 的
// 单次 CPU 推理不支持中途抢占，因此会在开始前和结束后检查 context。
func (d *Detector) DetectFile(ctx context.Context, imagePath string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	file, err := os.Open(imagePath)
	if err != nil {
		return Result{}, fmt.Errorf("打开待检测图片失败: %w", err)
	}
	defer file.Close()

	decoded, _, err := image.Decode(file)
	if err != nil {
		return Result{}, fmt.Errorf("解析图片失败: %w", err)
	}
	return d.DetectImage(ctx, decoded)
}

// DetectBytes lets callers keep extracted images in object storage rather than
// writing a temporary local file solely for layout detection.
func (d *Detector) DetectBytes(ctx context.Context, data []byte) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("解析图片失败: %w", err)
	}
	return d.DetectImage(ctx, decoded)
}

// DetectImage 检测已经解码的图片。它会统一转换为紧凑的 RGB 像素缓冲区，再传入 C++
// 推理器，避免把 image.Image 的 Go 接口边界带入原生层。
func (d *Detector) DetectImage(ctx context.Context, source image.Image) (Result, error) {
	if d == nil {
		return Result{}, errors.New("本地版面检测器尚未初始化")
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.native == nil {
		return Result{}, errors.New("本地版面检测器尚未初始化")
	}
	if source == nil {
		return Result{}, errors.New("待检测图片不能为空")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return Result{}, errors.New("待检测图片尺寸无效")
	}
	rgb := make([]byte, width*height*3)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := source.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			offset := (y*width + x) * 3
			rgb[offset] = uint8(r >> 8)
			rgb[offset+1] = uint8(g >> 8)
			rgb[offset+2] = uint8(b >> 8)
		}
	}

	regions, err := d.native.detect(rgb, width, height, d.scoreThreshold)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	result := Result{Regions: regions}
	for _, region := range regions {
		switch region.Class {
		case "text", "paragraph_title", "doc_title", "abstract", "content", "reference", "footnote", "header", "footer", "aside_text":
			result.HasText = true
		case "table":
			result.HasTable = true
		}
	}
	return result, nil
}

// Close 释放 ONNX Runtime 会话。桌面程序关闭或替换模型配置时调用即可；重复调用安全。
func (d *Detector) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.native == nil {
		return
	}
	d.native.close()
	d.native = nil
}
