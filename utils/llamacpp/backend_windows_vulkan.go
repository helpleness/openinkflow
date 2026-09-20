//go:build windows && cgo && inkflow_vulkan && !inkflow_cuda

package llamacpp

/*
// Vulkan build. Vulkan is a separate ggml backend; it is not a CUDA shim.
// It can use an integrated GPU when the machine has a working Vulkan driver.
#cgo LDFLAGS: -L${SRCDIR}/../../llama/cmake-build-vulkan/lib -L${SRCDIR}/../../llama/cmake-build-vulkan/lib/Release -l:llama.lib -l:ggml.lib -l:ggml-base.lib -l:ggml-cpu.lib -l:ggml-vulkan.lib -lvulkan-1
*/
import "C"
