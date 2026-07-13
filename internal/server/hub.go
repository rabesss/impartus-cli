package server

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsSendQueueCapacity = 64
	wsWriteTimeout      = 5 * time.Second
	wsPingInterval      = 30 * time.Second
)

type wsClient struct {
	conn      *websocket.Conn
	send      chan []byte
	done      chan struct{}
	writeDone chan struct{}
	stopOnce  sync.Once
}

func newWSClient(conn *websocket.Conn, queueCapacity int) *wsClient {
	return &wsClient{
		conn:      conn,
		send:      make(chan []byte, queueCapacity),
		done:      make(chan struct{}),
		writeDone: make(chan struct{}),
	}
}

func (c *wsClient) stop() {
	c.stopOnce.Do(func() {
		close(c.done)
	})
}

// WSHub manages WebSocket client connections and broadcasts messages to all connected clients.
type WSHub struct {
	clients map[*wsClient]struct{}
	mu      sync.RWMutex

	queueCapacity int
	writeTimeout  time.Duration
	pingInterval  time.Duration
}

// NewWSHub creates a new WebSocket hub with no connected clients.
func NewWSHub() *WSHub {
	return &WSHub{
		clients:       make(map[*wsClient]struct{}),
		queueCapacity: wsSendQueueCapacity,
		writeTimeout:  wsWriteTimeout,
		pingInterval:  wsPingInterval,
	}
}

// Register adds a WebSocket connection to the hub and starts its sole writer.
func (h *WSHub) Register(conn *websocket.Conn) *wsClient {
	if conn == nil {
		return nil
	}

	client := newWSClient(conn, h.queueCapacity)
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	go h.writePump(client)
	return client
}

// Unregister removes a WebSocket client from the hub and signals its writer to stop.
func (h *WSHub) Unregister(client *wsClient) {
	if client == nil {
		return
	}

	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
	client.stop()
}

func (h *WSHub) writePump(client *wsClient) {
	ticker := time.NewTicker(h.pingInterval)
	defer func() {
		ticker.Stop()
		h.Unregister(client)
		_ = client.conn.Close() //nolint:errcheck
		close(client.writeDone)
	}()

	for {
		// Prefer shutdown over draining queued events after a disconnect or
		// overflow. WebSocket delivery is intentionally live and best-effort.
		select {
		case <-client.done:
			return
		default:
		}

		select {
		case <-client.done:
			return
		case data := <-client.send:
			if err := client.conn.SetWriteDeadline(time.Now().Add(h.writeTimeout)); err != nil {
				return
			}
			if err := client.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			if err := client.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(h.writeTimeout)); err != nil {
				return
			}
		}
	}
}

// Broadcast queues one JSON-encoded message for every connected client. It
// never performs network I/O or waits for a client. A client whose bounded
// queue is full is disconnected so it cannot delay healthy clients.
func (h *WSHub) Broadcast(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	var overflowed []*wsClient
	h.mu.RLock()
	for client := range h.clients {
		select {
		case client.send <- data:
		default:
			overflowed = append(overflowed, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range overflowed {
		h.Unregister(client)
	}

	return nil
}

func broadcastEvent(hub *WSHub, payload any) {
	if err := hub.Broadcast(payload); err != nil {
		log.Printf("websocket broadcast failed: %v", err)
	}
}
