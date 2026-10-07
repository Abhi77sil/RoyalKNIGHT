package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type NetworkInfo struct {
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

type IPResolver struct {
	mu        sync.RWMutex
	cached    NetworkInfo
	lastCheck time.Time
	client    *http.Client
}

func NewIPResolver() *IPResolver {
	return &IPResolver{
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

func (r *IPResolver) GetNetworkInfo() NetworkInfo {
	r.mu.RLock()
	if time.Since(r.lastCheck) < 5*time.Minute && (r.cached.IPv4 != "" || r.cached.IPv6 != "") {
		info := r.cached
		r.mu.RUnlock()
		return info
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	info := NetworkInfo{
		IPv4: r.resolvePublicIP("https://api4.ipify.org"),
		IPv6: r.resolvePublicIP("https://api6.ipify.org"),
	}

	// Fallback to local network interfaces if public lookup timed out
	if info.IPv4 == "" {
		info.IPv4 = getLocalIPv4()
	}
	if info.IPv6 == "" {
		info.IPv6 = getLocalIPv6()
	}

	r.cached = info
	r.lastCheck = time.Now()
	return info
}

func (r *IPResolver) resolvePublicIP(providerURL string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, providerURL, nil)
	if err != nil {
		return ""
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}

	ipStr := strings.TrimSpace(string(body))
	if ip := net.ParseIP(ipStr); ip != nil {
		return ipStr
	}
	return ""
}

func getLocalIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}

func getLocalIPv6() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To16(); ip != nil && ip.To4() == nil && ip.IsGlobalUnicast() {
				return ip.String()
			}
		}
	}
	return ""
}
