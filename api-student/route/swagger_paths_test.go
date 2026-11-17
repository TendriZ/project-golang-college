package route_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSwaggerDocument memeriksa isi file yang dihasilkan swag, bukan
// komentar anotasinya. Dokumentasi yang lupa di-generate ulang adalah
// dokumentasi yang berbohong.
func TestSwaggerDocument(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "swagger.json"))
	if err != nil {
		t.Fatalf("membaca docs/swagger.json: %v", err)
	}

	var doc struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("mengurai swagger.json: %v", err)
	}

	if doc.Info.Title != "API Manajemen Mahasiswa" {
		t.Errorf("title = %q", doc.Info.Title)
	}

	wajib := []string{
		"/health",
		"/auth/register", "/auth/login", "/auth/refresh", "/auth/logout", "/auth/me",
		"/students", "/students/{id}",
		"/students/{id}/portfolio", "/students/{id}/portfolio/projects",
		"/students/{id}/portfolio/projects/{projectId}",
		"/portfolios/stats/technologies",
		"/supervisors/{id}/assign", "/supervisors/{id}", "/supervisions/switch",
		"/notifications/mine", "/admin/queue",
	}
	for _, path := range wajib {
		if _, ok := doc.Paths[path]; !ok {
			t.Errorf("path wajib %q tidak ada di swagger.json", path)
		}
	}

	if len(doc.Paths) != 17 {
		t.Errorf("jumlah path = %d, seharusnya 17 (seluruh endpoint yang terdaftar)", len(doc.Paths))
	}
}
