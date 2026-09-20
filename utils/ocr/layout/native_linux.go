//go:build linux && inkflow_onnx

package layout

/*
#cgo LDFLAGS: -L${SRCDIR}/../../../lib -lInkFlowLayout -ldl
#include "native/layout_engine.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

const (
	linuxNativeErrorSize      = 1024
	maximumLinuxNativeRegions = 512
)

// linuxDetector 通过 C ABI 调用由服务器本机 g++ 编译的 ONNX Runtime 桥接库。
// 它不依赖 Windows/MSVC；部署脚本会将 libInkFlowLayout.so 与 libonnxruntime.so
// 一起放入程序目录的 lib 下。
type linuxDetector struct {
	engine *C.InkFlowLayoutEngine
}

func newNativeDetector(modelPath string, threads int) (nativeDetector, error) {
	cModelPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cModelPath))

	errorBuffer := make([]byte, linuxNativeErrorSize)
	engine := C.inkflow_layout_create(
		cModelPath,
		C.int32_t(threads),
		(*C.char)(unsafe.Pointer(&errorBuffer[0])),
		C.size_t(len(errorBuffer)),
	)
	if engine == nil {
		return nil, fmt.Errorf("创建 Linux ONNX 版面检测会话失败: %s", linuxNativeError(errorBuffer))
	}
	return &linuxDetector{engine: engine}, nil
}

func (d *linuxDetector) detect(rgb []byte, width, height int, scoreThreshold float32) ([]Region, error) {
	if d == nil || d.engine == nil {
		return nil, fmt.Errorf("ONNX 版面检测会话已关闭")
	}
	if len(rgb) == 0 {
		return nil, fmt.Errorf("待检测图片像素为空")
	}

	detections := make([]C.InkFlowLayoutDetection, maximumLinuxNativeRegions)
	errorBuffer := make([]byte, linuxNativeErrorSize)
	var count C.size_t
	status := C.inkflow_layout_detect(
		d.engine,
		(*C.uint8_t)(unsafe.Pointer(&rgb[0])),
		C.int32_t(width),
		C.int32_t(height),
		C.uint32_t(math.Float32bits(scoreThreshold)),
		(*C.InkFlowLayoutDetection)(unsafe.Pointer(&detections[0])),
		C.size_t(len(detections)),
		&count,
		(*C.char)(unsafe.Pointer(&errorBuffer[0])),
		C.size_t(len(errorBuffer)),
	)
	if status != 0 {
		return nil, fmt.Errorf("Linux ONNX 版面检测失败: %s", linuxNativeError(errorBuffer))
	}
	if count > C.size_t(len(detections)) {
		return nil, fmt.Errorf("ONNX 版面检测返回了无效结果数量")
	}

	regions := make([]Region, 0, int(count))
	for _, detection := range detections[:int(count)] {
		classID := int(detection.class_id)
		regions = append(regions, Region{
			ClassID: classID,
			Class:   className(classID),
			Score:   float32(detection.score),
			Left:    float32(detection.x1),
			Top:     float32(detection.y1),
			Right:   float32(detection.x2),
			Bottom:  float32(detection.y2),
		})
	}
	return regions, nil
}

func (d *linuxDetector) close() {
	if d != nil && d.engine != nil {
		C.inkflow_layout_destroy(d.engine)
		d.engine = nil
	}
}

func linuxNativeError(buffer []byte) string {
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
