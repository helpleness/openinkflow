#include "layout_engine.h"

#include <onnxruntime_c_api.h>

#include <algorithm>
#include <cmath>
#include <cstdio>
#include <cstring>
#include <memory>
#include <string>
#include <vector>

#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#endif

namespace {

constexpr int kInputWidth = 480;
constexpr int kInputHeight = 480;
constexpr size_t kMaximumDetections = 512;

const float kMean[3] = {0.485F, 0.456F, 0.406F};
const float kStd[3] = {0.229F, 0.224F, 0.225F};

void write_error(char* destination, size_t size, const char* message) {
    if (destination == nullptr || size == 0) {
        return;
    }
    std::snprintf(destination, size, "%s", message == nullptr ? "未知 ONNX Runtime 错误" : message);
}

bool consume_status(const OrtApi* api, OrtStatus* status, char* error_message, size_t error_message_size) {
    if (status == nullptr) {
        return true;
    }
    write_error(error_message, error_message_size, api->GetErrorMessage(status));
    api->ReleaseStatus(status);
    return false;
}

#ifdef _WIN32
std::wstring utf8_to_wide(const char* input) {
    if (input == nullptr || *input == '\0') {
        return {};
    }
    const int count = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, input, -1, nullptr, 0);
    if (count <= 1) {
        return {};
    }
    std::wstring output(static_cast<size_t>(count - 1), L'\0');
    MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, input, -1, output.data(), count);
    return output;
}
#endif

void resize_and_normalize(const uint8_t* rgb, int source_width, int source_height, std::vector<float>* output) {
    output->assign(static_cast<size_t>(3 * kInputWidth * kInputHeight), 0.0F);
    for (int y = 0; y < kInputHeight; ++y) {
        const float source_y = (static_cast<float>(y) + 0.5F) * source_height / kInputHeight - 0.5F;
        const int y0 = std::max(0, static_cast<int>(std::floor(source_y)));
        const int y1 = std::min(source_height - 1, y0 + 1);
        const float wy = std::max(0.0F, source_y - std::floor(source_y));

        for (int x = 0; x < kInputWidth; ++x) {
            const float source_x = (static_cast<float>(x) + 0.5F) * source_width / kInputWidth - 0.5F;
            const int x0 = std::max(0, static_cast<int>(std::floor(source_x)));
            const int x1 = std::min(source_width - 1, x0 + 1);
            const float wx = std::max(0.0F, source_x - std::floor(source_x));
            const size_t output_offset = static_cast<size_t>(y * kInputWidth + x);

            for (int channel = 0; channel < 3; ++channel) {
                const float top_left = rgb[(static_cast<size_t>(y0) * source_width + x0) * 3 + channel];
                const float top_right = rgb[(static_cast<size_t>(y0) * source_width + x1) * 3 + channel];
                const float bottom_left = rgb[(static_cast<size_t>(y1) * source_width + x0) * 3 + channel];
                const float bottom_right = rgb[(static_cast<size_t>(y1) * source_width + x1) * 3 + channel];
                const float top = top_left + (top_right - top_left) * wx;
                const float bottom = bottom_left + (bottom_right - bottom_left) * wx;
                const float normalized = ((top + (bottom - top) * wy) / 255.0F - kMean[channel]) / kStd[channel];
                (*output)[static_cast<size_t>(channel) * kInputWidth * kInputHeight + output_offset] = normalized;
            }
        }
    }
}

}  // namespace

struct InkFlowLayoutEngine {
    const OrtApi* api = nullptr;
    OrtEnv* environment = nullptr;
    OrtSessionOptions* session_options = nullptr;
    OrtSession* session = nullptr;
    OrtAllocator* allocator = nullptr;
    std::vector<const char*> input_names;
    std::vector<const char*> output_names;
    std::vector<char*> owned_names;

    ~InkFlowLayoutEngine() {
        if (api != nullptr) {
            for (char* name : owned_names) {
                if (allocator != nullptr && name != nullptr) {
                    // AllocatorFree 返回 OrtStatus；析构流程不能抛出异常，但仍需释放
                    // 可能返回的错误状态，避免忽略带 warn_unused_result 的返回值。
                    OrtStatus* status = api->AllocatorFree(allocator, name);
                    if (status != nullptr) {
                        api->ReleaseStatus(status);
                    }
                }
            }
            if (session != nullptr) api->ReleaseSession(session);
            if (session_options != nullptr) api->ReleaseSessionOptions(session_options);
            if (environment != nullptr) api->ReleaseEnv(environment);
        }
    }
};

extern "C" InkFlowLayoutEngine* inkflow_layout_create(
    const char* model_path,
    int32_t threads,
    char* error_message,
    size_t error_message_size
) {
    if (model_path == nullptr || *model_path == '\0') {
        write_error(error_message, error_message_size, "ONNX 模型路径不能为空");
        return nullptr;
    }

    // 新版 Runtime 支持与头文件一致的 API 版本。个别 Windows 发行包会把 C API 表
    // 固定在 v10；本桥接只使用 v10 之前就存在的基础会话/张量 API，因此可安全回退。
    const OrtApiBase* api_base = OrtGetApiBase();
    const OrtApi* api = api_base == nullptr ? nullptr : api_base->GetApi(ORT_API_VERSION);
    if (api == nullptr && api_base != nullptr) {
        api = api_base->GetApi(10);
    }
    if (api == nullptr) {
        write_error(error_message, error_message_size, "无法加载 ONNX Runtime C API");
        return nullptr;
    }
    std::unique_ptr<InkFlowLayoutEngine> engine(new InkFlowLayoutEngine());
    engine->api = api;
    if (!consume_status(api, api->CreateEnv(ORT_LOGGING_LEVEL_WARNING, "InkFlowLayout", &engine->environment), error_message, error_message_size) ||
        !consume_status(api, api->CreateSessionOptions(&engine->session_options), error_message, error_message_size) ||
        !consume_status(api, api->SetIntraOpNumThreads(engine->session_options, std::max(1, threads)), error_message, error_message_size) ||
        !consume_status(api, api->SetSessionGraphOptimizationLevel(engine->session_options, ORT_ENABLE_ALL), error_message, error_message_size) ||
        !consume_status(api, api->GetAllocatorWithDefaultOptions(&engine->allocator), error_message, error_message_size)) {
        return nullptr;
    }

#ifdef _WIN32
    const std::wstring wide_path = utf8_to_wide(model_path);
    if (wide_path.empty()) {
        write_error(error_message, error_message_size, "ONNX 模型路径不是有效 UTF-8 文本");
        return nullptr;
    }
    if (!consume_status(api, api->CreateSession(engine->environment, wide_path.c_str(), engine->session_options, &engine->session), error_message, error_message_size)) {
        return nullptr;
    }
#else
    if (!consume_status(api, api->CreateSession(engine->environment, model_path, engine->session_options, &engine->session), error_message, error_message_size)) {
        return nullptr;
    }
#endif

    size_t input_count = 0;
    size_t output_count = 0;
    if (!consume_status(api, api->SessionGetInputCount(engine->session, &input_count), error_message, error_message_size) ||
        !consume_status(api, api->SessionGetOutputCount(engine->session, &output_count), error_message, error_message_size)) {
        return nullptr;
    }
    if (input_count != 2 || output_count < 2) {
        write_error(error_message, error_message_size, "PP-DocLayout-S ONNX 模型的输入或输出数量不匹配");
        return nullptr;
    }
    for (size_t index = 0; index < input_count; ++index) {
        char* name = nullptr;
        if (!consume_status(api, api->SessionGetInputName(engine->session, index, engine->allocator, &name), error_message, error_message_size)) {
            return nullptr;
        }
        engine->owned_names.push_back(name);
        engine->input_names.push_back(name);
    }
    for (size_t index = 0; index < 2; ++index) {
        char* name = nullptr;
        if (!consume_status(api, api->SessionGetOutputName(engine->session, index, engine->allocator, &name), error_message, error_message_size)) {
            return nullptr;
        }
        engine->owned_names.push_back(name);
        engine->output_names.push_back(name);
    }
    return engine.release();
}

extern "C" int32_t inkflow_layout_detect(
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
) {
    if (count != nullptr) *count = 0;
    if (engine == nullptr || rgb == nullptr || width <= 0 || height <= 0 || detections == nullptr || count == nullptr) {
        write_error(error_message, error_message_size, "ONNX 版面检测参数无效");
        return -1;
    }
    if (capacity == 0) {
        write_error(error_message, error_message_size, "ONNX 检测结果缓冲区为空");
        return -1;
    }

    float score_threshold = 0.0F;
    std::memcpy(&score_threshold, &score_threshold_bits, sizeof(score_threshold));
    std::vector<float> image_tensor;
    resize_and_normalize(rgb, width, height, &image_tensor);
    const float scale_factor[] = {
        static_cast<float>(kInputHeight) / height,
        static_cast<float>(kInputWidth) / width,
    };
    const int64_t image_shape[] = {1, 3, kInputHeight, kInputWidth};
    const int64_t scale_shape[] = {1, 2};
    OrtMemoryInfo* memory_info = nullptr;
    OrtValue* input_image = nullptr;
    OrtValue* input_scale = nullptr;
    OrtValue* output_values[2] = {nullptr, nullptr};

    const OrtApi* api = engine->api;
    const auto release_values = [&]() {
        if (output_values[0] != nullptr) api->ReleaseValue(output_values[0]);
        if (output_values[1] != nullptr) api->ReleaseValue(output_values[1]);
        if (input_image != nullptr) api->ReleaseValue(input_image);
        if (input_scale != nullptr) api->ReleaseValue(input_scale);
        if (memory_info != nullptr) api->ReleaseMemoryInfo(memory_info);
    };
    if (!consume_status(api, api->CreateCpuMemoryInfo(OrtArenaAllocator, OrtMemTypeDefault, &memory_info), error_message, error_message_size) ||
        !consume_status(api, api->CreateTensorWithDataAsOrtValue(memory_info, image_tensor.data(), image_tensor.size() * sizeof(float), image_shape, 4, ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT, &input_image), error_message, error_message_size) ||
        !consume_status(api, api->CreateTensorWithDataAsOrtValue(memory_info, const_cast<float*>(scale_factor), sizeof(scale_factor), scale_shape, 2, ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT, &input_scale), error_message, error_message_size)) {
        release_values();
        return -1;
    }
    const OrtValue* input_values[] = {input_image, input_scale};
    if (!consume_status(api, api->Run(engine->session, nullptr, engine->input_names.data(), input_values, 2, engine->output_names.data(), 2, output_values), error_message, error_message_size)) {
        release_values();
        return -1;
    }

    OrtTensorTypeAndShapeInfo* detection_info = nullptr;
    if (!consume_status(api, api->GetTensorTypeAndShape(output_values[0], &detection_info), error_message, error_message_size)) {
        release_values();
        return -1;
    }
    size_t element_count = 0;
    const bool has_element_count = consume_status(api, api->GetTensorShapeElementCount(detection_info, &element_count), error_message, error_message_size);
    api->ReleaseTensorTypeAndShapeInfo(detection_info);
    if (!has_element_count || element_count % 6 != 0) {
        if (has_element_count) write_error(error_message, error_message_size, "PP-DocLayout-S 检测输出格式不是 [N, 6]");
        release_values();
        return -1;
    }
    float* raw_detections = nullptr;
    if (!consume_status(api, api->GetTensorMutableData(output_values[0], reinterpret_cast<void**>(&raw_detections)), error_message, error_message_size)) {
        release_values();
        return -1;
    }
    const size_t rows = element_count / 6;
    const size_t maximum_rows = std::min({rows, capacity, kMaximumDetections});
    size_t written = 0;
    for (size_t row = 0; row < maximum_rows; ++row) {
        const float* raw = raw_detections + row * 6;
        if (raw[1] < score_threshold) continue;
        detections[written++] = InkFlowLayoutDetection{
            static_cast<int32_t>(std::lround(raw[0])), raw[1], raw[2], raw[3], raw[4], raw[5],
        };
    }
    *count = written;
    release_values();
    return 0;
}

extern "C" void inkflow_layout_destroy(InkFlowLayoutEngine* engine) {
    delete engine;
}
