package model

import "time"

// Portfolio adalah bentuk portofolio yang dikenal seluruh aplikasi.
//
// Perhatikan: tidak ada satu pun tipe dari driver MongoDB di sini. ID disimpan
// sebagai string, bukan bson.ObjectID. Package model tetap bebas dari tipe
// milik driver — urusan menerjemahkan ke bentuk BSON adalah tugas repository.
type Portfolio struct {
	ID          string        `json:"id"`
	StudentID   int           `json:"student_id"`
	Headline    string        `json:"headline"`
	Bio         string        `json:"bio"`
	Projects    []Project     `json:"projects"`
	Certificates []Certificate `json:"certificates"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// Project disimpan DI DALAM dokumen Portfolio (embedding), bukan di collection
// terpisah. Proyek tidak pernah dibaca tanpa portofolio induknya, dan jumlahnya
// dibatasi agar dokumen tidak pernah menabrak batas 16 MB MongoDB.
type Project struct {
	ID        string   `json:"id"`
	Judul     string   `json:"judul"`
	Teknologi []string `json:"teknologi"`
	Tautan    string   `json:"tautan"`
	Tahun     int      `json:"tahun"`
}

// Certificate juga disimpan di dalam dokumen Portfolio (embedding).
type Certificate struct {
	ID        string    `json:"id"`
	Nama      string    `json:"nama"`
	Penerbit  string    `json:"penerbit"`
	Tanggal   time.Time `json:"tanggal"`
}

// CreatePortfolioRequest adalah body untuk membuat/mengganti portofolio.
// Headline dan bio wajib diisi.
type CreatePortfolioRequest struct {
	Headline string `json:"headline" validate:"required,min=1,max=200"`
	Bio      string `json:"bio" validate:"required,min=1,max=2000"`
}

// UpdatePortfolioRequest memakai pointer seperti PATCH: nil berarti
// "tidak dikirim", bukan "dikosongkan".
type UpdatePortfolioRequest struct {
	Headline *string `json:"headline,omitempty" validate:"omitnil,min=1,max=200"`
	Bio      *string `json:"bio,omitempty" validate:"omitnil,min=1,max=2000"`
}

// AddProjectRequest adalah body untuk menambah satu proyek ke portofolio.
type AddProjectRequest struct {
	Judul     string   `json:"judul" validate:"required,min=1,max=200"`
	Teknologi []string `json:"teknologi" validate:"max=10,dive,min=1,max=50"`
	Tautan    string   `json:"tautan" validate:"required,min=1,max=500"`
	Tahun     int      `json:"tahun" validate:"required,min=1900,max=2100"`
}

// PortfolioStats adalah hasil agregasi teknologi paling banyak dipakai.
type PortfolioStats struct {
	Technologies []TechCount `json:"technologies"`
}

// TechCount pasangkan nama teknologi dengan jumlah portofolio yang memakainya.
type TechCount struct {
	Teknologi string `json:"teknologi"`
	Total     int    `json:"total"`
}
