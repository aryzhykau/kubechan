// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package ws

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 50 * time.Second // must be < pongWait
	maxMessageSize = 512
	sendBufSize    = 256
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins for dev; tighten in production via CORS headers on the ingress.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Client represents a single WebSocket connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// ServeWS upgrades the HTTP connection to WebSocket and registers the client.
func ServeWS(hub *Hub, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Error("ws upgrade failed", "error", err)
			return
		}
		c := &Client{hub: hub, conn: conn, send: make(chan []byte, sendBufSize)}
		hub.register(c)
		go c.writePump()
		go c.readPump()
	}
}

// ServeWSWithAuth is like ServeWS but requires a valid JWT in the ?token= query param.
func ServeWSWithAuth(hub *Hub, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		if err := validateWSToken(token); err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Error("ws upgrade failed", "error", err)
			return
		}
		c := &Client{hub: hub, conn: conn, send: make(chan []byte, sendBufSize)}
		hub.register(c)
		go c.writePump()
		go c.readPump()
	}
}

func validateWSToken(raw string) error {
	secret := []byte(os.Getenv("JWT_SECRET"))
	_, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithExpirationRequired())
	return err
}

// readPump drains incoming messages (we don't use client→server messages).
// It also handles pong frames and triggers unregister on close.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
