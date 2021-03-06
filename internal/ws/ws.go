package ws

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/gofiber/websocket/v2"
	"github.com/nats-io/nats.go"
)

var allowOrigins = map[string]bool{"http://localhost:8080": true}

// Hub maintains the set of active clients and broadcasts messages to the clients
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from clients
	broadcast chan []byte

	// Register requests from the clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Server name
	name string
}

// NewHub creates a new Hub
func NewHub(serverName string) *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		name:       serverName,
	}
}

// Client is a middleman between the websocket connection and the hub
type Client struct {
	hub *Hub

	// The websocket connection
	conn *websocket.Conn

	// Buffered channel of outbound messages
	send chan []byte

	natsSub *nats.Subscription
}

// Run listens and write messages
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Current connected ws client: %d", len(h.clients))
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				if client.natsSub != nil {
					client.natsSub.Unsubscribe()
					log.Println("Unsubscribe")
				}
			}
			log.Printf("Current connected ws client: %d", len(h.clients))
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					log.Println("default")
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

const (
	// Tima allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less then pongWait
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

var (
	newLine = []byte{'\n'}
	space   = []byte{' '}
)

func (c *Client) attachServerName(input []byte) ([]byte, error) {
	if len(input) <= 0 {
		return nil, errors.New("empty message")
	}
	m := make(map[string]string)
	err := json.Unmarshal(input, &m)
	if err != nil {
		log.Println(err)
	}
	m["from"] = c.hub.name
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// readPump pumps messages from the websocket connection to the hub
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is a at most one reader on a connection by executing all
// reads from this goroutine
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)

	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newLine, space, -1))
		c.hub.broadcast <- message
	}
}

// writePump pumps messages from the hub to the websocket connection
//
// A goroutine running writePump is started for each connection. The
// Application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case m, ok := <-c.send:
			message, _ := c.attachServerName(m)
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel
				log.Println("closing")
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newLine)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ServeWs handles websocket requests from the peer
func ServeWs(hub *Hub, conn *websocket.Conn, nc *nats.Conn, subject string) {
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}

	// subscribe nats
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		log.Println(string(m.Data))
		client.hub.broadcast <- m.Data
	})
	if err != nil {
		log.Fatal(err)
	}
	nc.Flush()

	if err := nc.LastError(); err != nil {
		log.Fatal(err)
	}

	client.natsSub = sub
	client.hub.register <- client

	// Allow Connection of memory referenced by the calloer by doing all work in new goroutines
	go client.writePump()
	go client.readPump()
}
