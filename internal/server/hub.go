package server

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type WSHub struct {
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

func NewWSHub() *WSHub {
	return &WSHub{clients: make(map[*websocket.Conn]bool)}
}

func (h *WSHub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

func (h *WSHub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
}

func (h *WSHub) Broadcast(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
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
