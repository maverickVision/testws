package main

import (
	"flag"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/websocket/v2"
	"github.com/maverickvision/testws/internal/ws"

	"github.com/nats-io/nats.go"
)

var addr = flag.String("addr", ":3000", "api service address")
var isNatsPublisher = flag.Bool("natsPublisher", true, "is nats publisher")

const subject = "com.testws.updates"

func run() error {
	flag.Parse()

	var serverName string
	if *addr == ":3000" {
		serverName = "server 1"
	} else {
		serverName = "server 2"
	}
	hub := ws.NewHub(serverName)
	go hub.Run()

	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	ec, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		log.Fatal(err)
	}

	defer ec.Close()
	defer nc.Close()

	app := fiber.New()
	app.Use(cors.New())

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		ws.ServeWs(hub, c, nc, subject)
	}))

	app.Post("/ping", func(c *fiber.Ctx) error {
		// publish to nats
		if *isNatsPublisher {
			message := map[string]string{"sendTime": time.Now().Format(time.ANSIC)}
			if err := ec.Publish(subject, message); err != nil {
				log.Fatal(err)
			}
		}

		return c.JSON(fiber.Map{"message": "pong"})
	})

	return app.Listen(*addr)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
