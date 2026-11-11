package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"api-student/app/model"
)

// SupervisionRepository adalah kontrak penyimpanan penetapan pembimbing.
type SupervisionRepository interface {
	// WithTx mengembalikan salinan repository yang menjalankan seluruh
	// query-nya di dalam transaction tx.
	WithTx(tx DBTX) SupervisionRepository

	FindByStudent(ctx context.Context, studentID int) (model.Supervision, error)
	Assign(ctx context.Context, studentID, supervisorID int) error
	Move(ctx context.Context, studentID, supervisorID int) error

	// FindRecipients mengambil seluruh mahasiswa binaan seorang pembimbing
	// beserta akun pemilik datanya. Hanya mahasiswa yang memiliki akun
	// (owner_id terisi) yang dapat menerima email.
	FindRecipients(ctx context.Context, supervisorID int) ([]model.QuotaRecipient, error)
}

const supervisionColumns = `id, student_id, supervisor_id, created_at`

type supervisionPostgresRepository struct{ db DBTX }

func NewSupervisionRepository(db DBTX) SupervisionRepository {
	return &supervisionPostgresRepository{db: db}
}

func (r *supervisionPostgresRepository) WithTx(tx DBTX) SupervisionRepository {
	return &supervisionPostgresRepository{db: tx}
}

func scanSupervision(row pgx.Row) (model.Supervision, error) {
	var s model.Supervision
	err := row.Scan(&s.ID, &s.StudentID, &s.SupervisorID, &s.CreatedAt)
	return s, err
}

// FindByStudent mengambil penetapan pembimbing milik satu mahasiswa.
//
// "Belum punya pembimbing" bukan kegagalan query: ia adalah keadaan sah
// yang harus dibedakan dari kegagalan database. Karena itu ErrNoRows
// diterjemahkan menjadi ErrNotSupervised.
func (r *supervisionPostgresRepository) FindByStudent(
	ctx context.Context, studentID int,
) (model.Supervision, error) {
	s, err := scanSupervision(r.db.QueryRow(ctx,
		"SELECT "+supervisionColumns+" FROM supervisions WHERE student_id = $1",
		studentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Supervision{}, ErrNotSupervised
		}
		return model.Supervision{}, fmt.Errorf("mengambil pembimbing mahasiswa: %w", err)
	}
	return s, nil
}

// Assign mencatat penetapan pembimbing baru.
//
// UNIQUE (student_id) adalah penjaga terakhir aturan "satu mahasiswa satu
// pembimbing". Bila dua request lolos pemeriksaan di service, hanya satu
// INSERT yang berhasil; yang lain menjadi ErrAlreadySupervised dan seluruh
// transaction-nya (termasuk pengambilan kuota) di-rollback.
func (r *supervisionPostgresRepository) Assign(
	ctx context.Context, studentID, supervisorID int,
) error {
	_, err := r.db.Exec(ctx,
		"INSERT INTO supervisions (student_id, supervisor_id) VALUES ($1, $2)",
		studentID, supervisorID)
	if err != nil {
		if pgErrorCode(err) == codeUniqueViolation {
			return ErrAlreadySupervised
		}
		return fmt.Errorf("mencatat pembimbing: %w", err)
	}
	return nil
}

// Move memindahkan penetapan mahasiswa ke pembimbing lain.
//
// RowsAffected diperiksa dengan sengaja. UPDATE yang tidak menemukan row
// bukan error menurut PostgreSQL; bila diabaikan, pemanggil akan mengira
// perpindahan berhasil padahal tidak ada baris yang berubah.
func (r *supervisionPostgresRepository) Move(
	ctx context.Context, studentID, supervisorID int,
) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE supervisions SET supervisor_id = $1 WHERE student_id = $2`,
		supervisorID, studentID)
	if err != nil {
		return fmt.Errorf("memindahkan pembimbing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotSupervised
	}
	return nil
}

// FindRecipients menelusuri relasi yang menghubungkan seorang pembimbing
// dengan alamat email mahasiswa binaannya:
//
//	supervisions.student_id -> students.id -> students.owner_id -> users.id
//
// students tidak menyimpan email; email ada pada akun yang mendaftarkan
// data mahasiswa. Karena itu mahasiswa tanpa owner_id tidak muncul di sini
// dan tidak menerima email — keadaan itu sah, bukan kegagalan query.
//
// GROUP BY mencegah satu akun menerima beberapa email untuk event yang
// sama ketika akun tersebut memiliki lebih dari satu mahasiswa binaan
// pembimbing ini. Idempotency key (outbox_id, user_id) memang hanya
// mengizinkan satu notifikasi per akun per event.
func (r *supervisionPostgresRepository) FindRecipients(
	ctx context.Context, supervisorID int,
) ([]model.QuotaRecipient, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.username, u.email
		FROM supervisions sp
		JOIN students s ON s.id = sp.student_id
		JOIN users u    ON u.id = s.owner_id
		WHERE sp.supervisor_id = $1
		GROUP BY u.id, u.username, u.email
		ORDER BY u.id`, supervisorID)
	if err != nil {
		return nil, fmt.Errorf("mengambil penerima perubahan kuota: %w", err)
	}
	defer rows.Close()

	recipients := []model.QuotaRecipient{}
	for rows.Next() {
		var rcp model.QuotaRecipient
		if err := rows.Scan(&rcp.UserID, &rcp.Username, &rcp.Email); err != nil {
			return nil, fmt.Errorf("membaca row penerima: %w", err)
		}
		recipients = append(recipients, rcp)
	}
	return recipients, rows.Err()
}
