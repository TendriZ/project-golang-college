package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"latihan-fiber/app/repository"
	"latihan-fiber/config"
	"latihan-fiber/database"
)

var metodeBerbody = map[string]bool{
	fiber.MethodPost: true,
	fiber.MethodPut:  true,
	fiber.MethodPatch: true,
}

//menolak content-type yang bukan JSON
func requireJSON(c *fiber.Ctx) error {
	if metodeBerbody[c.Method()] {
		ct := c.Get("Content-Type")
		if !strings.HasPrefix(ct, fiber.MIMEApplicationJSON) {
			return fail(c, fiber.StatusUnsupportedMediaType, 
						"Content-Type harus application/json")
		}
	}
	return c.Next()
}

func main() {
	// 1. Konfigurasi
	config.LoadEnv()

	// 2. Koneksi basis data
	pool, err := database.NewPool(context.Background())
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	// 3. Perakitan: pool -> repository -> handler
	userRepository := repository.NewUserRepository(pool)
	userHandler := NewUserHandler(userRepository)

	// 4. Aplikasi (bagian ini sama seperti pertemuan 2)
	app := fiber.New(fiber.Config{ /* ... */ })
	app.Use(requestid.New())
	app.Use(logger.New( /* ... */ ))
	app.Use(cors.New())

	api := app.Group("/api/v1")

	api.Get("/health", func(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.UserContext(), 2*time.Second)
	defer cancel()
	
	// Kesehatan layanan kini ikut bergantung pada basis data.
		if err := pool.Ping(ctx); err != nil {
			return fail(c, fiber.StatusServiceUnavailable,
			"database tidak dapat dihubungi")
		}
		return ok(c, "server dan database berjalan", nil)
	})

	u := api.Group("/users", requireJSON)
	u.Get("/", userHandler.List)
	u.Get("/:id", userHandler.Get)
	u.Post("/", userHandler.Create)
	u.Put("/:id", userHandler.Replace)
	u.Patch("/:id", userHandler.Patch)
	u.Delete("/:id", userHandler.Delete)

	port := config.GetEnv("APP_PORT", "3000")
	log.Fatal(app.Listen(":" + port))
}
