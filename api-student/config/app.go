package config

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
	"api-student/helper"
	"api-student/middleware"
	"api-student/route"
)

// NewApp merakit aplikasi: membuat instance Fiber, memasang middleware, lalu mendaftarkan route.
func NewApp(logger *slog.Logger, deps route.Dependencies) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      GetEnv("APP_NAME", "db-students"),
		ErrorHandler: newErrorHandler(logger),
		BodyLimit:    1 * 1024 * 1024, // 1 MB — cegah payload raksasa habiskan memori
	})

	middleware.Register(app, logger, GetEnv("ALLOWED_ORIGINS", ""))
	route.Register(app, deps)

	app.Use(func(c *fiber.Ctx) error {
		return helper.NotFound("endpoint tidak ditemukan")
	})

	return app
}

// newErrorHandler adalah SATU-SATUNYA tempat error berubah menjadi
// response HTTP di seluruh aplikasi.
func newErrorHandler(logger *slog.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		requestID := helper.RequestID(c)

		var appErr *helper.AppError

		switch {
		case errors.As(err, &appErr):
			// Kegagalan yang sudah kita rencanakan.

		case errors.Is(err, fiber.ErrRequestEntityTooLarge):
			appErr = &helper.AppError{
				Status:  fiber.StatusRequestEntityTooLarge,
				Code:    "PAYLOAD_TOO_LARGE",
				Message: "ukuran body melebihi batas yang diizinkan",
			}

		default:
			// Kegagalan yang tidak kita duga.
			var fiberErr *fiber.Error

			if errors.As(err, &fiberErr) {
				appErr = &helper.AppError{
					Status:  fiberErr.Code,
					Code:    "HTTP_ERROR",
					Message: fiberErr.Message,
				}
			} else {
				appErr = helper.Internal(err)
			}
		}

		// Hanya kegagalan sisi server yang dicatat sebagai Error.
		// Kegagalan 4xx adalah kesalahan pemakai API, bukan kerusakan
		// sistem; mencatatnya sebagai Error membuat log penuh bising
		// sehingga kerusakan yang sesungguhnya justru tenggelam.
		if appErr.Status < fiber.StatusInternalServerError {
			logger.Warn("request_rejected",
				slog.String("request_id", requestID),
				slog.String("path", c.Path()),
				slog.String("code", appErr.Code),
				slog.Int("status", appErr.Status),
			)
		} else {
			logger.Error("request_failed",
				slog.String("request_id", requestID),
				slog.String("path", c.Path()),
				slog.String("code", appErr.Code),
				slog.Int("status", appErr.Status),
				slog.String("error", appErr.Error()),
			)
		}

		return c.Status(appErr.Status).JSON(model.ErrorResponse{
			Success:   false,
			Code:      appErr.Code,
			Message:   appErr.Message,
			Fields:    appErr.Fields,
			RequestID: requestID,
		})
	}
}
