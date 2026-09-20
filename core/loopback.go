package core

import (
	"fmt"
	"net"
	"strings"
)

// LoopbackAddress 将 HTTP 服务监听地址限制在本机回环网卡。
// 空主机名（例如 :8888）会归一化为 127.0.0.1:8888，其他网络地址会被拒绝，
// 避免桌面客户端的业务接口意外暴露到局域网。
func LoopbackAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "127.0.0.1:8888", nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid local HTTP address %q: %w", address, err)
	}
	if host == "" || host == "localhost" || host == "127.0.0.1" {
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return net.JoinHostPort(host, port), nil
	}
	return "", fmt.Errorf("local HTTP address must use loopback, got %q", address)
}
