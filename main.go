package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/websocket/v2"
)

type sampleRequest struct {
	Nome  string `json:"nome"`
	Idade int    `json:"idade"`
}

func setupRoutes(app *fiber.App) {
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				break
			}

			log.Printf("received: %s", msg)
			err = c.WriteMessage(mt, msg)

			if err != nil {
				log.Println("write:", err)
				break
			}
		}
	}))

	app.Post("/", func(c *fiber.Ctx) error {
		sample := new(sampleRequest)

		if err := c.BodyParser(sample); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"status": "error", "message": "Dados inválidos", "data": nil})
		}

		// How to send this response via websocket?
		return c.JSON(fiber.Map{"status": "success", "message": "Dados recebidos com sucesso", "data": sample})
	})
}

func main() {
	app := fiber.New()
	app.Use(cors.New())

	setupRoutes(app)

	log.Fatal(app.Listen(":8000"))
}
