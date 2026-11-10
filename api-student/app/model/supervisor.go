package model

import "time"

// Supervisor adalah dosen pembimbing tugas akhir beserta kuota bimbingannya.
//
// SupervisedCount disimpan sebagai kolom, bukan dihitung COUNT(*) setiap
// saat, karena UPDATE bersyarat membutuhkan sebuah kolom untuk dibandingkan
// dengan quota. Konsekuensinya kolom ini WAJIB selalu sama dengan jumlah
// baris supervisions milik pembimbing tersebut; menjaganya adalah tugas
// transaction di layer service.
type Supervisor struct {
	ID              int       `json:"id"`
	Name            string    `json:"name"`
	Quota           int       `json:"quota"`
	SupervisedCount int       `json:"supervised_count"`
	Version         int       `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
}

// Supervision adalah penetapan seorang mahasiswa ke seorang pembimbing.
//
// Satu mahasiswa hanya memiliki satu baris di tabel ini; aturan tersebut
// dijamin oleh UNIQUE (student_id), bukan oleh kode Go.
type Supervision struct {
	ID           int       `json:"id"`
	StudentID    int       `json:"student_id"`
	SupervisorID int       `json:"supervisor_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// AssignSupervisionRequest adalah body POST /supervisors/:id/assign.
// ID pembimbing diambil dari parameter jalur, bukan dari body, supaya
// tidak ada dua sumber kebenaran yang bisa saling bertentangan.
type AssignSupervisionRequest struct {
	StudentID int `json:"student_id" validate:"required,min=1"`
}

// SwitchSupervisionRequest adalah body POST /supervisions/switch.
// SupervisorID adalah pembimbing TUJUAN; pembimbing lama dibaca dari
// tabel supervisions, bukan dari body.
type SwitchSupervisionRequest struct {
	StudentID    int `json:"student_id" validate:"required,min=1"`
	SupervisorID int `json:"supervisor_id" validate:"required,min=1"`
}

// UpdateSupervisorQuotaRequest memakai optimistic locking: client wajib
// mengirim version yang dilihatnya saat form dibuka. Bila version di
// database sudah berubah, perubahan ditolak.
type UpdateSupervisorQuotaRequest struct {
	Quota   int `json:"quota" validate:"required,min=1"`
	Version int `json:"version" validate:"required,min=1"`
}
