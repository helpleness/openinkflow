package websocket

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	gorilla "github.com/gorilla/websocket"
)

func TestIsRetryableWorkerError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "reconnected", err: ErrWorkerReconnected, want: true},
		{name: "wrapped disconnect", err: errors.Join(errors.New("request failed"), ErrWorkerDisconnected), want: true},
		{name: "not connected", err: ErrWorkerNotConnected, want: true},
		{name: "eof", err: io.EOF, want: true},
		{name: "closed websocket", err: errors.New("websocket: close 1006 (abnormal closure)"), want: true},
		{name: "context canceled", err: context.Canceled, want: false},
		{name: "context deadline", err: context.DeadlineExceeded, want: false},
		{name: "worker model error", err: errors.New("embedding tensor shape is invalid"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryableWorkerError(tt.err); got != tt.want {
				t.Fatalf("IsRetryableWorkerError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestSessionReplacedClosePayloadUsesPrivateApplicationCode(t *testing.T) {
	payload := gorilla.FormatCloseMessage(SessionReplacedCloseCode, SessionReplacedCloseReason)
	if len(payload) < 3 {
		t.Fatalf("close payload is too short: %v", payload)
	}
	if code := int(binary.BigEndian.Uint16(payload[:2])); code != SessionReplacedCloseCode {
		t.Fatalf("close code = %d, want %d", code, SessionReplacedCloseCode)
	}
	if reason := string(payload[2:]); !strings.Contains(reason, "newer tab") {
		t.Fatalf("close reason = %q", reason)
	}
}
