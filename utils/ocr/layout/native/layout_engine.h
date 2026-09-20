#ifndef INKFLOW_LAYOUT_ENGINE_H
#define INKFLOW_LAYOUT_ENGINE_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#ifdef _WIN32
#define INKFLOW_LAYOUT_API __declspec(dllexport)
#elif defined(__GNUC__)
#define INKFLOW_LAYOUT_API __attribute__((visibility("default")))
#else
#define INKFLOW_LAYOUT_API
#endif

// InkFlowLayoutDetection 是跨越 Go/C++ 边界的纯 C 数据结构。不要在这里使用 C++ 的
// string、vector 等类型，避免把编译器 ABI 暴露给 CGO。
typedef struct InkFlowLayoutDetection {
    int32_t class_id;
    float score;
    float x1;
    float y1;
    float x2;
    float y2;
} InkFlowLayoutDetection;

typedef struct InkFlowLayoutEngine InkFlowLayoutEngine;

// 创建一个可复用的 ONNX Runtime 会话。错误文本由调用方提供的缓冲区接收。
INKFLOW_LAYOUT_API InkFlowLayoutEngine* inkflow_layout_create(
    const char* model_path,
    int32_t threads,
    char* error_message,
    size_t error_message_size
);

// 对 RGB 图片进行版面检测。detections 是调用方预分配的结果缓冲区；返回值为 0 表示成功。
INKFLOW_LAYOUT_API int32_t inkflow_layout_detect(
    InkFlowLayoutEngine* engine,
    const uint8_t* rgb,
    int32_t width,
    int32_t height,
    uint32_t score_threshold_bits,
    InkFlowLayoutDetection* detections,
    size_t capacity,
    size_t* count,
    char* error_message,
    size_t error_message_size
);

// 释放会话和 ONNX Runtime 资源。允许传入空指针。
INKFLOW_LAYOUT_API void inkflow_layout_destroy(InkFlowLayoutEngine* engine);

#ifdef __cplusplus
}
#endif

#undef INKFLOW_LAYOUT_API

#endif
