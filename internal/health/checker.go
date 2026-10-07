package health

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type SiteHealth struct {
	Domain       string   `json:"domain"`
	Status       string   `json:"status"` // "HEALTHY", "WARNING", "DOWN"
	StatusCode   int      `json:"status_code"`
	LatencyMs    int64    `json:"latency_ms"`
	IPAddresses  []string `json:"ip_addresses"`
	SSLExpiry    string   `json:"ssl_expiry,omitempty"`
	SSLDaysLeft  int      `json:"ssl_days_left"`
	ErrorMessage string   `json:"error_message,omitempty"`
	CheckedAt    string   `json:"checked_at"`
}

type Checker struct {
	client *http.Client
}

func NewChecker() *Checker {
	return &Checker{
		client: &http.Client{
			Timeout: 4 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return http.ErrUseLastResponse
				}
				return nil
			},
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

func (c *Checker) CheckSite(domain string, sslEnabled bool) SiteHealth {
	now := time.Now().UTC()
	result := SiteHealth{
		Domain:    domain,
		Status:    "HEALTHY",
		CheckedAt: now.Format(time.RFC3339),
	}

	// 1. DNS Resolution
	ips, err := net.LookupIP(domain)
	if err != nil {
		// Fallback for localhost / local testing
		if domain == "localhost" {
			result.IPAddresses = []string{"127.0.0.1"}
		} else {
			result.Status = "DOWN"
			result.ErrorMessage = fmt.Sprintf("DNS resolution failed: %v", err)
			return result
		}
	} else {
		for _, ip := range ips {
			result.IPAddresses = append(result.IPAddresses, ip.String())
		}
	}

	// 2. HTTP Probe
	proto := "http"
	if sslEnabled {
		proto = "https"
	}
	url := fmt.Sprintf("%s://%s", proto, domain)

	start := time.Now()
	resp, err := c.client.Get(url)
	latency := time.Since(start).Milliseconds()
	result.LatencyMs = latency

	if err != nil {
		// Try fallback to http if https failed on self-signed or localhost
		if sslEnabled {
			urlFallback := fmt.Sprintf("http://%s", domain)
			respFallback, errFallback := c.client.Get(urlFallback)
			if errFallback == nil {
				result.StatusCode = respFallback.StatusCode
				respFallback.Body.Close()
				result.Status = "WARNING"
				result.ErrorMessage = fmt.Sprintf("HTTP OK (%d), but HTTPS probe failed: %v", result.StatusCode, err)
				return result
			}
		}
		result.Status = "DOWN"
		result.ErrorMessage = fmt.Sprintf("Connection failed: %v", err)
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode

	if resp.StatusCode >= 500 {
		result.Status = "DOWN"
		result.ErrorMessage = fmt.Sprintf("Server returned 5xx status: %d", resp.StatusCode)
	} else if resp.StatusCode >= 400 && resp.StatusCode != 404 {
		result.Status = "WARNING"
		result.ErrorMessage = fmt.Sprintf("Server returned status: %d", resp.StatusCode)
	}

	// 3. SSL Expiry Check
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		result.SSLExpiry = cert.NotAfter.Format("2006-01-02")
		daysLeft := int(time.Until(cert.NotAfter).Hours() / 24)
		result.SSLDaysLeft = daysLeft

		if daysLeft < 0 {
			result.Status = "DOWN"
			result.ErrorMessage = "SSL certificate has expired"
		} else if daysLeft < 15 {
			if result.Status == "HEALTHY" {
				result.Status = "WARNING"
			}
			result.ErrorMessage = fmt.Sprintf("SSL certificate expires in %d days", daysLeft)
		}
	}

	if latency > 1500 && result.Status == "HEALTHY" {
		result.Status = "WARNING"
		result.ErrorMessage = fmt.Sprintf("Slow response latency: %dms", latency)
	}

	return result
}

func ValidateDomain(domain string) (bool, string) {
	domain = strings.TrimSpace(domain)
	if len(domain) == 0 {
		return false, "domain cannot be empty"
	}
	if len(domain) > 253 {
		return false, "domain name is too long"
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false, "domain cannot start or end with a dot"
	}
	if strings.Contains(domain, "..") {
		return false, "domain cannot contain consecutive dots"
	}
	if domain == "localhost" {
		return true, ""
	}

	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return false, "domain must contain at least one dot (e.g. example.com)"
	}

	for _, part := range parts {
		if len(part) == 0 || len(part) > 63 {
			return false, "domain label length must be between 1 and 63 characters"
		}
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return false, "domain labels cannot start or end with a hyphen"
		}
		for _, r := range part {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
				return false, "domain contains invalid characters"
			}
		}
	}

	return true, ""
}
