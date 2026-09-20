//go:build windows && cgo && !inkflow_cuda && !inkflow_vulkan

package llamacpp

/*
// CPU-only MSVC build: no CUDA or Vulkan import library is present in this link.
#cgo LDFLAGS: -L${SRCDIR}/../../llama/cmake-build-cpu/lib -L${SRCDIR}/../../llama/cmake-build-cpu/lib/Release -l:llama.lib -l:ggml.lib -l:ggml-base.lib -l:ggml-cpu.lib
*/
import "C"
