// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package ws

import (
	"log/slog"
	"sync"
)

// Hub maintains the set of active WebSocket clients and broadcasts messages.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
	logger  *slog.Logger
}

// NewHub creates a new Hub.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[*Client]struct{}),
		logger:  logger,
	}
}

// register adds a client to the hub.
func (h *Hub) register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.logger.Info("ws client connected", "remoteAddr", c.conn.RemoteAddr())
}

// unregister removes a client and closes its send channel.
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
	h.logger.Info("ws client disconnected", "remoteAddr", c.conn.RemoteAddr())
}

// Broadcast sends a message to all connected clients.
// Clients whose send buffer is full are silently dropped (slow consumer protection).
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			h.logger.Warn("ws client send buffer full, dropping message",
				"remoteAddr", c.conn.RemoteAddr())
		}
	}
}
