package server

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSHub manages WebSocket client connections and broadcasts messages to all connected clients.
type WSHub struct {
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

// NewWSHub creates a new WebSocket hub with no connected clients.
func NewWSHub() *WSHub {
	return &WSHub{clients: make(map[*websocket.Conn]bool)}
}

// Register adds a WebSocket connection to the hub.
func (h *WSHub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

// Unregister removes a WebSocket connection from the hub.
func (h *WSHub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
}

// Broadcast sends a JSON-encoded message to all connected WebSocket clients,
// removing any that have disconnected.
func (h *WSHub) Broadcast(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			_ = conn.Close() //nolint:errcheck
			delete(h.clients, conn)
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			//nolint:errcheck
			_ = conn.Close()
			delete(h.clients, conn)
		}
	}

	return nil
}

func broadcastEvent(hub *WSHub, payload any) {
	if err := hub.Broadcast(payload); err != nil {
		log.Printf("websocket broadcast failed: %v", err)
	}
}
