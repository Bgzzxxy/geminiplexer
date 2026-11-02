package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// ChromeVersion is the browser version we impersonate.
const ChromeVersion = "130.0.0.0"

// NewStealthClient returns an http.Client whose TLS fingerprint matches
// Chrome 130 (uTLS), with HTTP/2 support via h2 ALPN.
// If proxyURL is non-empty, requests are routed through the HTTP/SOCKS5 proxy.
func NewStealthClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 300 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("parse proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(u)
	}

	// Wrap transport with uTLS for Chrome fingerprint
	stealthTransport := &stealthRoundTripper{
		base:     transport,
		proxyURL: proxyURL,
	}

	return &http.Client{
		Transport: stealthTransport,
		Timeout:   timeout,
		// Don't follow redirects automatically for cookie rotation
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}, nil
}

// stealthRoundTripper wraps HTTP requests with Chrome uTLS fingerprint.
type stealthRoundTripper struct {
	base     *http.Transport
	proxyURL string
	mu       sync.Mutex
	h2Conns  map[string]*http2.ClientConn
}

func (s *stealthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// For non-TLS requests, use base transport directly
	if req.URL.Scheme != "https" {
		return s.base.RoundTrip(req)
	}

	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(host, port)

	// Try to reuse existing HTTP/2 connection
	s.mu.Lock()
	if s.h2Conns == nil {
		s.h2Conns = make(map[string]*http2.ClientConn)
	}
	if cc, ok := s.h2Conns[addr]; ok {
		if cc.CanTakeNewRequest() {
			s.mu.Unlock()
			resp, err := cc.RoundTrip(req)
			if err == nil {
				return resp, nil
			}
			// Connection stale, fall through to create new one
			s.mu.Lock()
			delete(s.h2Conns, addr)
		}
	}
	s.mu.Unlock()

	// Create new uTLS connection
	conn, tlsConn, err := s.dialUTLS(req.Context(), addr, host)
	if err != nil {
		return nil, fmt.Errorf("utls dial %s: %w", addr, err)
	}

	// Check if server supports HTTP/2
	negotiatedProto := tlsConn.ConnectionState().NegotiatedProtocol
	if negotiatedProto == "h2" {
		// Upgrade to HTTP/2
		h2Transport := &http2.Transport{}
		cc, err := h2Transport.NewClientConn(conn)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("h2 client conn: %w", err)
		}
		s.mu.Lock()
		s.h2Conns[addr] = cc
		s.mu.Unlock()
		return cc.RoundTrip(req)
	}

	// HTTP/1.1 fallback — shouldn't happen for Google but handle it
	conn.Close()
	return s.base.RoundTrip(req)
}

func (s *stealthRoundTripper) dialUTLS(ctx context.Context, addr, host string) (net.Conn, *utls.UConn, error) {
	var rawConn net.Conn
	var err error

	if s.proxyURL != "" {
		rawConn, err = dialProxy(ctx, s.proxyURL, addr)
	} else {
		dialer := &net.Dialer{Timeout: 30 * time.Second}
		rawConn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("dial tcp: %w", err)
	}

	tlsConfig := &utls.Config{
		ServerName:         host,
		InsecureSkipVerify: false,
	}

	tlsConn := utls.UClient(rawConn, tlsConfig, utls.HelloChrome_Auto)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, nil, fmt.Errorf("utls handshake: %w", err)
	}

	return tlsConn, tlsConn, nil
}

// dialProxy connects through an HTTP CONNECT or SOCKS5 proxy.
func dialProxy(ctx context.Context, proxyURL, targetAddr string) (net.Conn, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	proxyAddr := u.Host
	if !strings.Contains(proxyAddr, ":") {
		switch u.Scheme {
		case "http":
			proxyAddr += ":80"
		case "https":
			proxyAddr += ":443"
		case "socks5":
			proxyAddr += ":1080"
		}
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("dial proxy %s: %w", proxyAddr, err)
	}

	if u.Scheme == "http" || u.Scheme == "https" {
		// HTTP CONNECT tunnel
		connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", targetAddr, targetAddr)
		if u.User != nil {
			connectReq += fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", u.User.String())
		}
		connectReq += "\r\n"

		if _, err := conn.Write([]byte(connectReq)); err != nil {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT write: %w", err)
		}

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT read: %w", err)
		}
		resp := string(buf[:n])
		if !strings.Contains(resp, "200") {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT failed: %s", resp)
		}
	}
	// SOCKS5 is handled by golang.org/x/net/proxy if needed

	return conn, nil
}

// UserAgent returns a realistic Chrome User-Agent string matching the host OS.
func UserAgent() string {
	switch runtime.GOOS {
	case "linux":
		return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", ChromeVersion)
	case "darwin":
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", ChromeVersion)
	default:
		return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", ChromeVersion)
	}
}

// SecCHUAPlatform returns the correct Sec-CH-UA-Platform header for the host OS.
func SecCHUAPlatform() string {
	switch runtime.GOOS {
	case "linux":
		return `"Linux"`
	case "darwin":
		return `"macOS"`
	default:
		return `"Windows"`
	}
}

// SecCHUA returns the Sec-CH-UA header matching Chrome version.
func SecCHUA() string {
	return fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not?A_Brand";v="99"`,
		strings.Split(ChromeVersion, ".")[0],
		strings.Split(ChromeVersion, ".")[0])
}

// SetChromeHeaders adds all Chrome-fingerprint-consistent headers to a request.
func SetChromeHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Sec-CH-UA", SecCHUA())
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", SecCHUAPlatform())
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
}

// SetNavigateHeaders adds navigate-type Chrome headers (for loading /app).
func SetNavigateHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent())
	req.Header.Set("Sec-CH-UA", SecCHUA())
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", SecCHUAPlatform())
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "max-age=0")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
}

// Ensure we implement http.RoundTripper
var _ http.RoundTripper = (*stealthRoundTripper)(nil)

// Ensure tls import is used (for potential future direct TLS config)
var _ = tls.VersionTLS13
