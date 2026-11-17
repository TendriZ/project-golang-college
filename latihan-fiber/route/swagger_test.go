package route_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"latihan-fiber/config"
	_ "latihan-fiber/docs" // mendaftarkan spesifikasi hasil swag init
	"latihan-fiber/route"
)

// newApp merakit aplikasi tanpa database. Swagger UI tidak membutuhkan
// service apa pun; ia hanya menyajikan file spesifikasi.
func newApp(enableSwagger bool) *fiber.App {
	return config.NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)),
		route.Dependencies{EnableSwagger: enableSwagger})
}

func TestSwaggerUI_Enabled(t *testing.T) {
	app := newApp(true)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil), -1)
	if err != nil {
		t.Fatalf("request swagger: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, seharusnya 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// Judul spesifikasi dibaca Swagger UI dari swagger.json melalui
	// JavaScript, bukan ditanam di HTML. Isinya diperiksa terpisah oleh
	// TestSwaggerDocument.
	if !strings.Contains(string(body), "Swagger UI") {
		t.Errorf("halaman tidak memuat Swagger UI")
	}
}

func TestSwaggerUI_Disabled(t *testing.T) {
	app := newApp(false)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil), -1)
	if err != nil {
		t.Fatalf("request swagger: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, seharusnya 404 saat SWAGGER_ENABLED=false", resp.StatusCode)
	}
}
