package model

import "time"

type Student struct {
	ID        int       `json:"id"`
	NIM       int       `json:"nim"`
	Name      string    `json:"name"`
	Grade     float64   `json:"grade"`
	IsActive  bool      `json:"is_active"`
	OwnerID   int       `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Aturan validasi ditulis sebagai tag pada struct — aturan dan bentuk
// data berada pada baris yang sama, sehingga menambah satu field tanpa
// aturannya menjadi kelalaian yang langsung terlihat.
// min=1 menjaga PATCH: omitnil melewatkan nil, tetapi "" (non-nil) tetap
// harus ditolak. Tanpa min=1, {"name":""} akan lolos.
// POST
type CreateStudentRequest struct {
	NIM   int     `json:"nim" validate:"required,min=1"`
	Name  string  `json:"name" validate:"required,min=1,nodigit,max=100"`
	Grade float64 `json:"grade" validate:"gte=0,lte=100"`
}

// PUT - ganti seluruh isi, jadi field bertipe biasa dan semuanya wajib.
// Grade tanpa required agar nilai 0 yang sah tetap lolos (0 bukan error).
type ReplaceStudentRequest struct {
	Name     string  `json:"name" validate:"required,min=1,nodigit,max=100"`
	NIM      int     `json:"nim" validate:"required,min=1"`
	Grade    float64 `json:"grade" validate:"gte=0,lte=100"`
	IsActive bool    `json:"is_active"`
}

// PATCH - ubah sebagian field, jadi field bertipe pointer supaya bisa dibedakan
// antara "tidak dikirim" (nil) dan "dikirim bernilai kosong".
// omitnil dipilih karena ia menyatakan maksud yang sebenarnya: lewati
// hanya bila nil. {"name":""} TETAP diperiksa (min=1) dan ditolak.
type PatchStudentRequest struct {
	Name     *string  `json:"name,omitempty" validate:"omitnil,min=1,nodigit,max=100"`
	NIM      *int     `json:"nim,omitempty" validate:"omitnil,min=1"`
	Grade    *float64 `json:"grade,omitempty" validate:"omitnil,gte=0,lte=100"`
	IsActive *bool    `json:"is_active,omitempty"`
}

type WebResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Meta    *Meta  `json:"meta,omitempty"`
	Errors  any    `json:"errors,omitempty"`
}

type Meta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type ListQuery struct {
	Page     int
	Limit    int
	Search   string
	Sort     string
	Order    string
	IsActive *bool
	MinGrade *float64
	MaxGrade *float64
}

// Offset menghitung berapa baris yang dilewati untuk halaman ini.
func (q ListQuery) Offset() int {
	return (q.Page - 1) * q.Limit
}

type ErrorResponse struct {
	Success   bool              `json:"success"`
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

// CursorMeta menggantikan Meta pada endpoint yang memakai cursor.
//
// Perhatikan tidak adanya Total dan TotalPages. Keduanya tidak dapat
// disediakan tanpa COUNT(*) atas seluruh tabel — persis biaya yang ingin
// dihindari oleh pagination berbasis cursor.
type CursorMeta struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

type CursorQuery struct {
	Limit    int
	Search   string
	IsActive *bool
	MinGrade *float64
	MaxGrade *float64
	After    *Cursor
}

type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        int       `json:"id"`
}
