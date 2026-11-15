package risk

import (
	"net/netip"
	"strings"
)

// NormalizeIP 统一 IPv4/IPv6 文本，并剥离 IPv6 zone。
func NormalizeIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if host, _, ok := strings.Cut(raw, "%"); ok {
		raw = host
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	return addr.Unmap().String()
}

// Subnet 返回 IPv4 /24 或 IPv6 /64 网段；无法解析时返回空字符串。
func Subnet(raw string) string {
	raw = strings.TrimSpace(raw)
	if host, _, ok := strings.Cut(raw, "%"); ok {
		raw = host
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 64
	if addr.Is4() {
		bits = 24
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return prefix.Masked().String()
}

// ClientKey 将非法或空 IP 归一到一个固定桶，保证限流不会因解析失败失效。
func ClientKey(raw string) string {
	if ip := NormalizeIP(raw); ip != "" {
		return ip
	}
	return "unknown"
}
