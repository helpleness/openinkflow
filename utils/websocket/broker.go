package websocket

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	gorilla "github.com/gorilla/websocket"
)

type Broker struct {
	mu          sync.Mutex
	conn        *gorilla.Conn
	ready       chan struct{}
	writeMu     sync.Mutex
	pending     map[string]chan Response
	timeout     time.Duration
	connectWait time.Duration
}

type Request struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Payload any    `json:"payload"`
}

type Response struct {
	Type        string          `json:"type"`
	ID          string          `json:"id"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	internalErr error
}

var (
	ErrWorkerReconnected  = errors.New("websocket worker reconnected")
	ErrWorkerDisconnected = errors.New("websocket worker disconnected")
	ErrWorkerNotConnected = errors.New("websocket worker is not connected")
)

var upgrader = gorilla.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	writeWait                  = 10 * time.Second
	pongWait                   = 60 * time.Second
	pingPeriod                 = 25 * time.Second
	maxMessageSize             = 1 << 20
	SessionReplacedCloseCode   = 4001
	SessionReplacedCloseReason = "session replaced by a newer tab"
)

func NewBroker(timeout time.Duration) *Broker {
	return &Broker{
		pending:     make(map[string]chan Response),
		ready:       make(chan struct{}),
		timeout:     timeout,
		connectWait: 20 * time.Second,
	}
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*gorilla.Conn, error) {
	return upgrader.Upgrade(w, r, nil)
}

func (b *Broker) Register(ctx context.Context, conn *gorilla.Conn) {
	b.mu.Lock()
	old := b.conn
	b.conn = conn
	b.notifyLocked()
	b.failPendingLocked(ErrWorkerReconnected)
	b.mu.Unlock()
	if old != nil {
		b.closeReplacedConnection(old)
	}
	done := make(chan struct{})
	defer func() {
		close(done)
		_ = conn.Close()
		b.mu.Lock()
		if b.conn == conn {
			b.conn = nil
			b.notifyLocked()
			b.failPendingLocked(ErrWorkerDisconnected)
		}
		b.mu.Unlock()
	}()

	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	go b.keepAlive(conn, done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var msg Response
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		if msg.Type == "heartbeat" || msg.Type == "ping" {
			_ = b.writeJSON(conn, map[string]any{
				"type": "pong",
				"ts":   time.Now().UnixMilli(),
			})
			continue
		}
		if msg.Type == "pong" {
			continue
		}
		if msg.ID == "" {
			continue
		}
		b.mu.Lock()
		ch := b.pending[msg.ID]
		delete(b.pending, msg.ID)
		b.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (b *Broker) closeReplacedConnection(conn *gorilla.Conn) {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	_ = conn.WriteControl(
		gorilla.CloseMessage,
		gorilla.FormatCloseMessage(SessionReplacedCloseCode, SessionReplacedCloseReason),
		time.Now().Add(writeWait),
	)
	_ = conn.Close()
}

func (b *Broker) keepAlive(conn *gorilla.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			b.writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := conn.WriteMessage(gorilla.PingMessage, nil)
			b.writeMu.Unlock()
			if err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func (b *Broker) Request(ctx context.Context, typ string, payload any) (Response, error) {
	id, err := randomID()
	if err != nil {
		return Response{}, err
	}
	ch := make(chan Response, 1)

	conn, err := b.waitConn(ctx)
	if err != nil {
		return Response{}, err
	}

	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()

	b.writeMu.Lock()
	err = conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err == nil {
		err = conn.WriteJSON(Request{Type: typ, ID: id, Payload: payload})
	}
	b.writeMu.Unlock()
	if err != nil {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return Response{}, err
	}

	timeout := time.NewTimer(b.timeout)
	defer timeout.Stop()
	select {
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return Response{}, ctx.Err()
	case <-timeout.C:
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return Response{}, fmt.Errorf("websocket request %s timed out", typ)
	case msg := <-ch:
		if msg.internalErr != nil {
			return Response{}, msg.internalErr
		}
		if msg.Error != "" {
			return Response{}, errors.New(msg.Error)
		}
		return msg, nil
	}
}

func (b *Broker) writeJSON(conn *gorilla.Conn, payload any) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return err
	}
	return conn.WriteJSON(payload)
}

func (b *Broker) waitConn(ctx context.Context) (*gorilla.Conn, error) {
	deadline := time.NewTimer(b.connectWait)
	defer deadline.Stop()

	for {
		b.mu.Lock()
		conn := b.conn
		ready := b.ready
		b.mu.Unlock()
		if conn != nil {
			return conn, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, ErrWorkerNotConnected
		case <-ready:
		}
	}
}

func (b *Broker) notifyLocked() {
	close(b.ready)
	b.ready = make(chan struct{})
}

func (b *Broker) failPendingLocked(err error) {
	for id, ch := range b.pending {
		delete(b.pending, id)
		ch <- Response{Type: "error", ID: id, Error: err.Error(), internalErr: err}
	}
}

func IsRetryableWorkerError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrWorkerReconnected) ||
		errors.Is(err, ErrWorkerDisconnected) ||
		errors.Is(err, ErrWorkerNotConnected) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.EOF) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"websocket: close",
		"broken pipe",
		"connection reset",
		"use of closed network connection",
		"unexpected eof",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func randomID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
