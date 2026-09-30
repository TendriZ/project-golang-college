package helper

import (
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
)

const (
	FormatJSON = fiber.MIMEApplicationJSON
	FormatCSV  = "text/csv"
)

// Negotiate memilih format response berdasarkan header Accept.
//
// Perbedaan yang wajib jelas:
// - Content-Type menjelaskan format yang SEDANG DIKIRIM pengirim.
// - Accept menjelaskan format yang DIINGINKAN penerima sebagai balasan.
func Negotiate(c *fiber.Ctx, offered ...string) (string, error) {
	accept := strings.TrimSpace(c.Get(fiber.HeaderAccept))

	// Tidak menyebut Accept sama sekali berarti "terserah server".
	// Demikian pula Accept: */* yang dikirim hampir semua tool CLI.
	if accept == "" {
		return offered[0], nil
	}

	chosen := c.Accepts(offered...)
	if chosen == "" {
		return "", NotAcceptable(
			"format yang diminta tidak tersedia, pilih salah satu dari: " +
				strings.Join(offered, ", "))
	}

	return chosen, nil
}

// WriteStudentsCSV menuliskan daftar student sebagai CSV.
//
// Header Content-Disposition membuat browser menawarkan unduhan alih-alih
// menampilkan isinya sebagai teks mentah.
func WriteStudentsCSV(c *fiber.Ctx, students []model.Student) error {
	c.Set(fiber.HeaderContentType, FormatCSV+"; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="students.csv"`)

	var buffer strings.Builder
	writer := csv.NewWriter(&buffer)

	header := []string{"id", "nim", "name", "grade", "is_active", "owner_id", "created_at"}
	if err := writer.Write(header); err != nil {
		return Internal(err)
	}

	for _, s := range students {
		row := []string{
			strconv.Itoa(s.ID),
			strconv.Itoa(s.NIM),
			s.Name,
			strconv.FormatFloat(s.Grade, 'f', -1, 64),
			strconv.FormatBool(s.IsActive),
			strconv.Itoa(s.OwnerID),
			s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if err := writer.Write(row); err != nil {
			return Internal(err)
		}
	}

	if err := writer.Error(); err != nil {
		return Internal(err)
	}

	return c.SendString(buffer.String())
}
