//go:build windows && cgo && inkflow_cuda && !inkflow_vulkan

package inference

func rerankTestGPULayers() int { return -1 }
