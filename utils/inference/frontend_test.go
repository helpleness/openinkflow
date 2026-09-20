package inference

import (
	"context"
	"errors"
	"testing"

	ws "InkFlow/utils/websocket"
)

func TestRequestFrontendWithReconnectRetryRecovers(t *testing.T) {
	calls := 0
	response, retries, err := requestFrontendWithReconnectRetry(
		context.Background(),
		"embed",
		5,
		0,
		func() (ws.Response, error) {
			calls++
			if calls <= 2 {
				return ws.Response{}, ws.ErrWorkerReconnected
			}
			return ws.Response{Type: "result", ID: "ok"}, nil
		},
	)
	if err != nil {
		t.Fatalf("request returned error: %v", err)
	}
	if response.ID != "ok" {
		t.Fatalf("response ID = %q, want ok", response.ID)
	}
	if calls != 3 || retries != 2 {
		t.Fatalf("calls=%d retries=%d, want calls=3 retries=2", calls, retries)
	}
}

func TestRequestFrontendWithReconnectRetryStopsAfterFiveRetries(t *testing.T) {
	calls := 0
	_, retries, err := requestFrontendWithReconnectRetry(
		context.Background(),
		"embed",
		5,
		0,
		func() (ws.Response, error) {
			calls++
			return ws.Response{}, ws.ErrWorkerDisconnected
		},
	)
	if !errors.Is(err, ws.ErrWorkerDisconnected) {
		t.Fatalf("error = %v, want ErrWorkerDisconnected", err)
	}
	if calls != 6 || retries != 5 {
		t.Fatalf("calls=%d retries=%d, want calls=6 retries=5", calls, retries)
	}
}

func TestRequestFrontendWithReconnectRetryDoesNotRetryModelErrors(t *testing.T) {
	modelErr := errors.New("invalid embedding output")
	calls := 0
	_, retries, err := requestFrontendWithReconnectRetry(
		context.Background(),
		"embed",
		5,
		0,
		func() (ws.Response, error) {
			calls++
			return ws.Response{}, modelErr
		},
	)
	if !errors.Is(err, modelErr) {
		t.Fatalf("error = %v, want model error", err)
	}
	if calls != 1 || retries != 0 {
		t.Fatalf("calls=%d retries=%d, want calls=1 retries=0", calls, retries)
	}
}

func TestRequestFrontendWithReconnectRetryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, retries, err := requestFrontendWithReconnectRetry(
		ctx,
		"embed",
		5,
		0,
		func() (ws.Response, error) {
			calls++
			return ws.Response{}, ws.ErrWorkerReconnected
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if calls != 1 || retries != 0 {
		t.Fatalf("calls=%d retries=%d, want calls=1 retries=0", calls, retries)
	}
}
