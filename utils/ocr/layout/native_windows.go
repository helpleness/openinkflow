//go:build windows && inkflow_onnx

package layout

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	nativeErrorSize      = 1024
	maximumNativeRegions = 512
)

// nativeDetection 的内存布局与 native/layout_engine.h 中的 InkFlowLayoutDetection
// 完全一致。数值通过内存传递，避免动态调用 Windows ABI 时传递 float 参数。
type nativeDetection struct {
	classID int32
	score   float32
	x1      float32
	y1      float32
	x2      float32
	y2      float32
}

type windowsDetector struct {
	runtime    *syscall.DLL
	library    *syscall.DLL
	create     *syscall.Proc
	detectProc *syscall.Proc
	destroy    *syscall.Proc
	engine     uintptr
}

func newNativeDetector(modelPath string, threads int) (nativeDetector, error) {
	bridgePath := nativeLibraryPath()
	runtimeLibrary, err := syscall.LoadDLL(filepath.Join(filepath.Dir(bridgePath), "onnxruntime.dll"))
	if err != nil {
		return nil, fmt.Errorf("加载 onnxruntime.dll 失败: %w", err)
	}
	library, err := syscall.LoadDLL(bridgePath)
	if err != nil {
		runtimeLibrary.Release()
		return nil, fmt.Errorf("加载 InkFlowLayout.dll 失败: %w", err)
	}
	create, err := library.FindProc("inkflow_layout_create")
	if err != nil {
		library.Release()
		runtimeLibrary.Release()
		return nil, fmt.Errorf("InkFlowLayout.dll 缺少创建函数: %w", err)
	}
	detect, err := library.FindProc("inkflow_layout_detect")
	if err != nil {
		library.Release()
		runtimeLibrary.Release()
		return nil, fmt.Errorf("InkFlowLayout.dll 缺少检测函数: %w", err)
	}
	destroy, err := library.FindProc("inkflow_layout_destroy")
	if err != nil {
		library.Release()
		runtimeLibrary.Release()
		return nil, fmt.Errorf("InkFlowLayout.dll 缺少释放函数: %w", err)
	}

	cModelPath, err := syscall.BytePtrFromString(modelPath)
	if err != nil {
		library.Release()
		runtimeLibrary.Release()
		return nil, fmt.Errorf("ONNX 模型路径包含无效字符: %w", err)
	}
	errorBuffer := make([]byte, nativeErrorSize)
	engine, _, callErr := create.Call(
		uintptr(unsafe.Pointer(cModelPath)),
		uintptr(threads),
		uintptr(unsafe.Pointer(&errorBuffer[0])),
		uintptr(len(errorBuffer)),
	)
	if engine == 0 {
		library.Release()
		runtimeLibrary.Release()
		return nil, fmt.Errorf("创建 ONNX 版面检测会话失败: %s%s", nativeError(errorBuffer), nativeCallSuffix(callErr))
	}
	return &windowsDetector{runtime: runtimeLibrary, library: library, create: create, detectProc: detect, destroy: destroy, engine: engine}, nil
}

func (d *windowsDetector) detect(rgb []byte, width, height int, scoreThreshold float32) ([]Region, error) {
	if d.engine == 0 {
		return nil, fmt.Errorf("ONNX 版面检测会话已关闭")
	}
	if len(rgb) == 0 {
		return nil, fmt.Errorf("待检测图片像素为空")
	}

	detections := make([]nativeDetection, maximumNativeRegions)
	errorBuffer := make([]byte, nativeErrorSize)
	var count uintptr
	status, _, callErr := d.detectProc.Call(
		d.engine,
		uintptr(unsafe.Pointer(&rgb[0])),
		uintptr(width),
		uintptr(height),
		uintptr(math.Float32bits(scoreThreshold)),
		uintptr(unsafe.Pointer(&detections[0])),
		uintptr(len(detections)),
		uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&errorBuffer[0])),
		uintptr(len(errorBuffer)),
	)
	if status != 0 {
		return nil, fmt.Errorf("ONNX 版面检测失败: %s%s", nativeError(errorBuffer), nativeCallSuffix(callErr))
	}
	if count > uintptr(len(detections)) {
		return nil, fmt.Errorf("ONNX 版面检测返回了无效结果数量")
	}

	regions := make([]Region, 0, int(count))
	for _, detection := range detections[:int(count)] {
		classID := int(detection.classID)
		regions = append(regions, Region{
			ClassID: classID,
			Class:   className(classID),
			Score:   detection.score,
			Left:    detection.x1,
			Top:     detection.y1,
			Right:   detection.x2,
			Bottom:  detection.y2,
		})
	}
	return regions, nil
}

func (d *windowsDetector) close() {
	if d.engine != 0 {
		d.destroy.Call(d.engine)
		d.engine = 0
	}
	if d.library != nil {
		d.library.Release()
		d.library = nil
	}
	if d.runtime != nil {
		d.runtime.Release()
		d.runtime = nil
	}
}

func nativeLibraryPath() string {
	if configuredPath := os.Getenv("INKFLOW_ONNX_LAYOUT_DLL"); configuredPath != "" {
		return configuredPath
	}
	if executable, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(executable), "InkFlowLayout.dll")
	}
	return "InkFlowLayout.dll"
}

func nativeError(buffer []byte) string {
	for index, value := range buffer {
		if value == 0 {
			if index > 0 {
				return string(buffer[:index])
			}
			break
		}
	}
	return "原生检测器未返回错误信息"
}

func nativeCallSuffix(err error) string {
	if err == nil || err == syscall.Errno(0) {
		return ""
	}
	return "; Windows 调用错误: " + err.Error()
}
