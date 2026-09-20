// Package http provides internal HTTP clients for user-configured remote
// integrations.
package http

import (
	"crypto/tls"
	"errors"
	"fmt"
	nethttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout bounds one remote integration request when the caller does
// not specify a timeout.
const DefaultTimeout = 60 * time.Second

// remoteTransport owns the process-wide connection pool. Session-specific
// deadlines are enforced by http.Client.Timeout, including waiting for headers.
var remoteTransport = &nethttp.Transport{
	Proxy:                 nil,
	DialContext:           newPublicDialer(10*time.Second, 30*time.Second).DialContext,
	ForceAttemptHTTP2:     true,
	TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   10,
	MaxConnsPerHost:       20,
}

// ParsePublicHTTPSURL parses and normalizes a user-configured public HTTPS
// endpoint. Hostnames are checked after DNS resolution by the guarded dialer
// returned from NewPublicHTTPSClient.
func ParsePublicHTTPSURL(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid remote endpoint: %w", err)
	}
	if !strings.EqualFold(endpoint.Scheme, "https") {
		return nil, errors.New("remote endpoint must use HTTPS")
	}
	if endpoint.Host == "" || endpoint.Hostname() == "" {
		return nil, errors.New("remote endpoint host is required")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("remote endpoint cannot contain credentials, query parameters, or a fragment")
	}
	if _, err := endpointPort(endpoint); err != nil {
		return nil, err
	}

	host := strings.TrimSuffix(strings.ToLower(endpoint.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, errors.New("remote endpoint cannot target localhost")
	}
	if address, err := parseIPAddress(host); err == nil && !isPublicAddress(address) {
		return nil, errors.New("remote endpoint cannot target a non-public IP address")
	}
	return endpoint, nil
}

// NormalizeBearerToken trims a write-only bearer token and verifies that it
// can be safely placed in an HTTP Authorization header.
func NormalizeBearerToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if strings.ContainsAny(token, "\r\n\x00") {
		return "", errors.New("bearer token contains invalid characters")
	}
	return token, nil
}

// NewPublicHTTPSClient creates an HTTP client fixed to one public HTTPS
// origin. It shares a process-wide HTTP transport for connection pooling and a
// guarded dialer that rejects non-public and DNS-rebinding destinations.
func NewPublicHTTPSClient(rawEndpoint, bearerToken string, timeout time.Duration) (*nethttp.Client, *url.URL, error) {
	endpoint, err := ParsePublicHTTPSURL(rawEndpoint)
	if err != nil {
		return nil, nil, err
	}
	bearerToken, err = NormalizeBearerToken(bearerToken)
	if err != nil {
		return nil, nil, err
	}
	client := &nethttp.Client{
		Timeout: timeout,
		Transport: bearerTransport{
			base:        remoteTransport,
			origin:      endpoint,
			bearerToken: bearerToken,
		},
		CheckRedirect: func(_ *nethttp.Request, _ []*nethttp.Request) error {
			return errors.New("remote endpoint redirects are not permitted")
		},
	}
	return client, endpoint, nil
}

func endpointPort(endpoint *url.URL) (uint16, error) {
	port := endpoint.Port()
	if port == "" {
		return 443, nil
	}
	value, err := strconv.ParseUint(port, 10, 16)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("invalid remote endpoint port %q", port)
	}
	return uint16(value), nil
}

// bearerTransport deliberately does not forward CloseIdleConnections: the
// underlying pool is shared by all sessions and outlives individual clients.
type bearerTransport struct {
	base        nethttp.RoundTripper
	origin      *url.URL
	bearerToken string
}

func (t bearerTransport) RoundTrip(request *nethttp.Request) (*nethttp.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("invalid remote HTTP request")
	}
	if !sameOrigin(request.URL, t.origin) {
		return nil, errors.New("remote request target differs from its configured endpoint")
	}
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	if t.bearerToken != "" {
		cloned.Header.Set("Authorization", "Bearer "+t.bearerToken)
	}
	return t.base.RoundTrip(cloned)
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil || !strings.EqualFold(left.Scheme, right.Scheme) {
		return false
	}
	if !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	leftPort, leftErr := endpointPort(left)
	rightPort, rightErr := endpointPort(right)
	return leftErr == nil && rightErr == nil && leftPort == rightPort
}
