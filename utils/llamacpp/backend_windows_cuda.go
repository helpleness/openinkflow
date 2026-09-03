//go:build windows && cgo && inkflow_cuda && !inkflow_vulkan

package llamacpp

/*
// CUDA build. These are the import-library names produced by the MSVC build
// used by the hzj branch. If CGo is driven by MinGW, build llama.cpp with a
// MinGW-compatible generator so the corresponding lib*.dll.a files are used.
#cgo LDFLAGS: -L${SRCDIR}/../../llama/cmake-build-cuda/lib -L${SRCDIR}/../../llama/cmake-build-cuda/lib/Release -l:llama.lib -l:ggml.lib -l:ggml-base.lib -l:ggml-cpu.lib -l:ggml-cuda.lib
*/
import "C"
