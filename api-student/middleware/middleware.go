package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"api-student/helper"
)

// Register memasang seluruh middleware global
func Register(app *fiber.App, logger *slog.Logger, allowedOrigins string) {
	app.Use(requestid.New())
	app.Use(recover.New()) // Mencegah aplikasi mati jika terjadi panic
	app.Use(helmet.New())
	app.Use(corsPolicy(allowedOrigins))

	// Mencatat setiap request HTTP menggunakan logger buatan kita
	app.Use(RequestLogger(logger))
}

// corsPolicy membatasi origin yang boleh memanggil API.
// cors.New() tanpa konfigurasi mengizinkan SEMUA origin — tidak aman untuk API bertoken.
func corsPolicy(allowedOrigins string) fiber.Handler {
	if strings.TrimSpace(allowedOrigins) == "" {
		allowedOrigins = "http://localhost:5173"
	}
	return cors.New(cors.Config{
		AllowOrigins: allowedOrigins,
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders: "Origin,Content-Type,Accept,Authorization",
	})
}

// RequestLogger mencatat detail setiap request (id, method, path, status, latency)
func RequestLogger(logger *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next() // Serahkan ke handler berikutnya

		requestID, _ := c.Locals("requestid").(string)

		logger.Info("http_request",
			slog.String("request_id", requestID),
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.Int("status", c.Response().StatusCode()),
			slog.Duration("duration", time.Since(start)),
			slog.String("ip", c.IP()),
		)

		return err
	}
}

var methodsWithBody = map[string]bool{
	fiber.MethodPost:  true,
	fiber.MethodPut:   true,
	fiber.MethodPatch: true,
}

// RequireJSON menolak request jika bodynya bukan application/json
func RequireJSON(c *fiber.Ctx) error {
	if methodsWithBody[c.Method()] {
		ct := c.Get("Content-Type")
		if !strings.HasPrefix(ct, fiber.MIMEApplicationJSON) {
			return helper.Fail(
				c,
				fiber.StatusUnsupportedMediaType,
				"Content-Type harus application/json",
			)
		}
	}
	return c.Next()
}