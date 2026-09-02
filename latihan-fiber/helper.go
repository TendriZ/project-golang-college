package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/app/model"
)

func ok(c *fiber.Ctx, message string, data any) error {
 	return c.Status(fiber.StatusOK).JSON(model.WebResponse{
 	Success: true, Message: message, Data: data,
 	})
}

func okList(c *fiber.Ctx, message string, data any, meta *model.Meta) error {
 	return c.Status(fiber.StatusOK).JSON(model.WebResponse{
 	Success: true, Message: message, Data: data, Meta: meta,
 	})
}

func created(c *fiber.Ctx, message string, data any, location string) error {
	c.Set("Location", location)
	return c.Status(fiber.StatusCreated).JSON(model.WebResponse{
		Success: true, Message: message, Data: data,
	})
}

func noContent(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent) //204 yang berarti berhasil tapi no body
}

func fail(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(model.WebResponse{
		Success: false, Message: message,
	})
}

func failValidation(c *fiber.Ctx, errs map[string]string) error {
	return c.Status(fiber.StatusUnprocessableEntity).JSON(model.WebResponse{
		Success: false, Message: "validasi gagal", Errors: errs,
	})
}

var allowedSort = map[string]bool{
	"id" : true,
	"username" : true,
	"email" : true,
	"created_at" : true,
}

func parseListQuery(c *fiber.Ctx) model.ListQuery {
	q := model.ListQuery {
		Page: c.QueryInt("page", 1),
		Limit: c.QueryInt("limit", 10),
		Search: strings.TrimSpace(c.Query("search")),
		Sort: c.Query("sort", "id"),
		Order: strings.ToLower(c.Query("order", "asc")),
	}

	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 10
	}
	if q.Limit > 100 { // batas atas
		q.Limit = 100
	}
	if !allowedSort[q.Sort] {
		q.Sort = "id"
	}
	if q.Order != "desc" {
		q.Order = "asc"
	}

	if raw := c.Query("is_active"); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			q.IsActive = &v
		}
	}
	return q
}

// paramID mengambil dan memvalidasi parameter :id dari URL.
// Mengembalikan false bila bukan angka atau bukan angka positif.
func paramID(c *fiber.Ctx) (int, bool) {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// reqCtx memberi batas waktu untuk setiap operasi basis data.
// Tanpa batas waktu, satu query yang menggantung dapat menahan koneksi
// selamanya dan lama-lama menghabiskan seluruh isi pool.
func reqCtx(c *fiber.Ctx) (context.Context, context.CancelFunc) {
 return context.WithTimeout(c.UserContext(), 5*time.Second)
}