package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParsePublicHTTPSURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{name: "public HTTPS endpoint", endpoint: "https://mcp.example.com/v1/mcp"},
		{name: "HTTP is rejected", endpoint: "http://mcp.example.com", wantErr: true},
		{name: "localhost is rejected", endpoint: "https://localhost/mcp", wantErr: true},
		{name: "private IPv4 is rejected", endpoint: "https://10.0.0.5/mcp", wantErr: true},
		{name: "loopback IPv6 is rejected", endpoint: "https://[::1]/mcp", wantErr: true},
		{name: "NAT64 private IPv4 is rejected", endpoint: "https://[64:ff9b::a00:1]/mcp", wantErr: true},
		{name: "endpoint credentials are rejected", endpoint: "https://user:password@mcp.example.com", wantErr: true},
		{name: "query is rejected", endpoint: "https://mcp.example.com/mcp?token=secret", wantErr: true},
		{name: "port zero is rejected", endpoint: "https://mcp.example.com:0/mcp", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParsePublicHTTPSURL(test.endpoint)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParsePublicHTTPSURL(%q) error = %v, wantErr %v", test.endpoint, err, test.wantErr)
			}
		})
	}
}

func TestResolvePublicAddressesRejectsLoopback(t *testing.T) {
	if _, err := resolvePublicAddresses(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("resolvePublicAddresses() allowed a loopback address")
	}
}

func TestNewPublicHTTPSClientConfiguresReusableConnectionPool(t *testing.T) {
	client, _, err := NewPublicHTTPSClient("https://mcp.example.com/v1/mcp", "", 20*time.Second)
	if err != nil {
		t.Fatalf("NewPublicHTTPSClient() error = %v", err)
	}
	wrapped, ok := client.Transport.(bearerTransport)
	if !ok {
		t.Fatalf("client transport type = %T, want bearerTransport", client.Transport)
	}
	transport, ok := wrapped.base.(*nethttp.Transport)
	if !ok {
		t.Fatalf("base transport type = %T, want *http.Transport", wrapped.base)
	}
	if client.Timeout != 20*time.Second {
		t.Fatalf("client timeout = %v, want 20s", client.Timeout)
	}
	if transport.ResponseHeaderTimeout != 0 || transport.IdleConnTimeout != 90*time.Second {
		t.Fatalf("transport timeouts = header %v, idle %v", transport.ResponseHeaderTimeout, transport.IdleConnTimeout)
	}
	if transport.MaxIdleConns != 100 || transport.MaxIdleConnsPerHost != 10 || transport.MaxConnsPerHost != 20 {
		t.Fatalf("transport pool = total %d, per-host %d, max %d", transport.MaxIdleConns, transport.MaxIdleConnsPerHost, transport.MaxConnsPerHost)
	}
	if transport.DisableKeepAlives {
		t.Fatal("transport disabled connection reuse")
	}
	other, _, err := NewPublicHTTPSClient("https://other.example.com:8443/mcp", "other-token", 120*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	otherWrapped := other.Transport.(bearerTransport)
	if wrapped.base != otherWrapped.base || wrapped.base != remoteTransport {
		t.Fatal("clients do not share the process-wide connection pool")
	}
	if other.Timeout != 120*time.Second || client.Timeout != 20*time.Second {
		t.Fatal("clients did not retain independent request timeouts")
	}
}

func TestNormalizeBearerToken(t *testing.T) {
	got, err := NormalizeBearerToken("  token-value  ")
	if err != nil {
		t.Fatalf("NormalizeBearerToken() error = %v", err)
	}
	if got != "token-value" {
		t.Fatalf("NormalizeBearerToken() = %q, want trimmed token", got)
	}
	if _, err := NormalizeBearerToken("token\r\nInjected: value"); err == nil {
		t.Fatal("NormalizeBearerToken() succeeded for a header-injection value")
	}
}

func TestRemoteRoundTripperSendsTokenOnlyToConfiguredOrigin(t *testing.T) {
	endpoint, err := url.Parse("https://mcp.example.com/v1/mcp")
	if err != nil {
		t.Fatal(err)
	}
	transport := bearerTransport{
		origin:      endpoint,
		bearerToken: "test-token",
		base: roundTripperFunc(func(request *nethttp.Request) (*nethttp.Response, error) {
			if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("Authorization = %q, want bearer token", got)
			}
			return &nethttp.Response{
				StatusCode: nethttp.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(nethttp.Header),
			}, nil
		}),
	}

	request, err := nethttp.NewRequestWithContext(context.Background(), nethttp.MethodPost, endpoint.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	_ = response.Body.Close()

	otherOrigin, err := nethttp.NewRequestWithContext(context.Background(), nethttp.MethodPost, "https://other.example.com/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(otherOrigin); err == nil {
		t.Fatal("RoundTrip() allowed a request to a different origin")
	}
	otherPort := request.Clone(context.Background())
	otherPort.URL.Host = "mcp.example.com:8443"
	if _, err := transport.RoundTrip(otherPort); err == nil {
		t.Fatal("RoundTrip() allowed a request to a different port")
	}
}

func TestClientsSharingTransportKeepBearerTokensSeparate(t *testing.T) {
	var gotTokens []string
	base := roundTripperFunc(func(request *nethttp.Request) (*nethttp.Response, error) {
		gotTokens = append(gotTokens, request.Header.Get("Authorization"))
		return &nethttp.Response{
			StatusCode: nethttp.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Header:     make(nethttp.Header),
		}, nil
	})
	request, err := nethttp.NewRequest(nethttp.MethodGet, "https://mcp.example.com/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"first-token", "second-token", ""} {
		client, _, err := NewPublicHTTPSClient(request.URL.String(), token, DefaultTimeout)
		if err != nil {
			t.Fatal(err)
		}
		wrapped := client.Transport.(bearerTransport)
		wrapped.base = base
		client.Transport = wrapped
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	if strings.Join(gotTokens, ",") != "Bearer first-token,Bearer second-token," {
		t.Fatalf("shared transport received unexpected tokens: %q", gotTokens)
	}
	if request.Header.Get("Authorization") != "" {
		t.Fatal("client modified the original request's authorization header")
	}
}

func TestBearerTransportDoesNotCloseSharedPool(t *testing.T) {
	base := &closeTrackingTransport{}
	client := &nethttp.Client{Transport: bearerTransport{base: base}}
	client.CloseIdleConnections()
	if base.closed {
		t.Fatal("closing one client cleared the shared connection pool")
	}
}

type roundTripperFunc func(*nethttp.Request) (*nethttp.Response, error)

func (f roundTripperFunc) RoundTrip(request *nethttp.Request) (*nethttp.Response, error) {
	return f(request)
}

type closeTrackingTransport struct {
	closed bool
}

func (transport *closeTrackingTransport) RoundTrip(*nethttp.Request) (*nethttp.Response, error) {
	return nil, nil
}

func (transport *closeTrackingTransport) CloseIdleConnections() {
	transport.closed = true
}
