//go:build windows && cgo && inkflow_vulkan && !inkflow_cuda

package inference

func rerankTestGPULayers() int { return -1 }
