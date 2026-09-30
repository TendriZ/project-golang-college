package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"api-student/app/model"
)

var (
	ErrNotFound  = errors.New("data tidak ditemukan")
	ErrDuplicate = errors.New("data sudah ada")
)

const studentColumns = "id, nim, name, grade, is_active, COALESCE(owner_id, 0), created_at"

type StudentRepository interface {
	FindAll(ctx context.Context, q model.ListQuery) ([]model.Student, int, error)
	FindAfterCursor(ctx context.Context, q model.CursorQuery) ([]model.Student, error)
	FindByID(ctx context.Context, id int) (model.Student, error)
	Create(ctx context.Context, s model.Student) (model.Student, error)
	Update(ctx context.Context, s model.Student) (model.Student, error)
	Delete(ctx context.Context, id int) error
}

var kolomUrut = map[string]string{
	"id":    "id",
	"name":  "name",
	"nim":   "nim",
	"grade": "grade",
}

type studentPostgresRepository struct {
	pool *pgxpool.Pool
}

func NewStudentRepository(pool *pgxpool.Pool) StudentRepository {
	return &studentPostgresRepository{pool: pool}
}

func buildFilter(q model.ListQuery) (string, []any) {
	where := " WHERE 1 = 1"
	args := []any{}

	if q.Search != "" {
		where += fmt.Sprintf(" AND name ILIKE $%d", len(args)+1)
		args = append(args, "%"+q.Search+"%")
	}
	if q.IsActive != nil {
		where += fmt.Sprintf(" AND is_active = $%d", len(args)+1)
		args = append(args, *q.IsActive)
	}
	if q.MinGrade != nil {
		where += fmt.Sprintf(" AND grade >= $%d", len(args)+1)
		args = append(args, *q.MinGrade)
	}
	if q.MaxGrade != nil {
		where += fmt.Sprintf(" AND grade <= $%d", len(args)+1)
		args = append(args, *q.MaxGrade)
	}
	return where, args
}

func (r *studentPostgresRepository) FindAll(ctx context.Context, q model.ListQuery) ([]model.Student, int, error) {
	where, args := buildFilter(q)

	var total int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM students"+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("menghitung student: %w", err)
	}

	arah := "ASC"
	if q.Order == "desc" {
		arah = "DESC"
	}

	sqlText := fmt.Sprintf(
		`SELECT id, nim, name, grade, is_active, COALESCE(owner_id, 0), created_at
		 FROM students%s
		 ORDER BY %s %s
		 LIMIT $%d OFFSET $%d`,
		where, kolomUrut[q.Sort], arah, len(args)+1, len(args)+2,
	)
	args = append(args, q.Limit, q.Offset())

	rows, err := r.pool.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("mengambil daftar student: %w", err)
	}
	defer rows.Close()

	hasil := []model.Student{}
	for rows.Next() {
		var s model.Student
		if err := rows.Scan(&s.ID, &s.NIM, &s.Name, &s.Grade, &s.IsActive, &s.OwnerID, &s.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("membaca baris student: %w", err)
		}
		hasil = append(hasil, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("membaca hasil query: %w", err)
	}
	return hasil, total, nil
}

func (r *studentPostgresRepository) FindByID(ctx context.Context, id int) (model.Student, error) {
	var s model.Student
	err := r.pool.QueryRow(ctx,
		`SELECT id, nim, name, grade, is_active, COALESCE(owner_id, 0), created_at
		 FROM students WHERE id = $1`, id,
	).Scan(&s.ID, &s.NIM, &s.Name, &s.Grade, &s.IsActive, &s.OwnerID, &s.CreatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Student{}, ErrNotFound
		}
		return model.Student{}, fmt.Errorf("mengambil student: %w", err)
	}
	return s, nil
}

func (r *studentPostgresRepository) Create(ctx context.Context, s model.Student) (model.Student, error) {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO students (nim, name, grade, is_active, owner_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, owner_id, created_at`,
		s.NIM, s.Name, s.Grade, s.IsActive, s.OwnerID,
	).Scan(&s.ID, &s.OwnerID, &s.CreatedAt)

	if err != nil {
		if isUniqueViolation(err) {
			return model.Student{}, ErrDuplicate
		}
		return model.Student{}, fmt.Errorf("menyimpan student: %w", err)
	}
	return s, nil
}

func (r *studentPostgresRepository) Update(ctx context.Context, s model.Student) (model.Student, error) {
	err := r.pool.QueryRow(ctx,
		`UPDATE students SET nim = $1, name = $2, grade = $3, is_active = $4
		 WHERE id = $5
		 RETURNING id, nim, name, grade, is_active, COALESCE(owner_id, 0), created_at`,
		s.NIM, s.Name, s.Grade, s.IsActive, s.ID,
	).Scan(&s.ID, &s.NIM, &s.Name, &s.Grade, &s.IsActive, &s.OwnerID, &s.CreatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Student{}, ErrNotFound
		}
		if isUniqueViolation(err) {
			return model.Student{}, ErrDuplicate
		}
		return model.Student{}, fmt.Errorf("memperbarui student: %w", err)
	}
	return s, nil
}

// FindAfterCursor mengambil satu halaman memakai keyset pagination.
//
// id ikut dibandingkan karena created_at TIDAK dijamin unik. Bila dua
// baris dibuat pada mikrodetik yang sama dan hanya created_at yang
// dibandingkan, salah satu baris akan terlewat atau terkirim dua kali.
//
// Jumlah yang diminta sengaja limit+1. Baris tambahan itu tidak dikirim
// ke client; keberadaannya hanya dipakai untuk menjawab "masih ada
// halaman berikutnya?" tanpa perlu COUNT(*) atas seluruh tabel.
//
// Pasangan pengurutan (created_at DESC, id DESC) inilah jawaban D.3.1:
// created_at menambatkan urutan waktu (terbaru dulu), id yang pasti unik
// menjadi pemecah seri sehingga tidak ada dua baris yang menempati
// posisi cursor yang sama.
func (r *studentPostgresRepository) FindAfterCursor(
	ctx context.Context,
	q model.CursorQuery,
) ([]model.Student, error) {
	args := []any{}
	where := " WHERE 1 = 1"

	if q.Search != "" {
		args = append(args, "%"+q.Search+"%")
		where += fmt.Sprintf(" AND name ILIKE $%d", len(args))
	}

	if q.IsActive != nil {
		args = append(args, *q.IsActive)
		where += fmt.Sprintf(" AND is_active = $%d", len(args))
	}

	if q.MinGrade != nil {
		args = append(args, *q.MinGrade)
		where += fmt.Sprintf(" AND grade >= $%d", len(args))
	}

	if q.MaxGrade != nil {
		args = append(args, *q.MaxGrade)
		where += fmt.Sprintf(" AND grade <= $%d", len(args))
	}

	if q.After != nil {
		args = append(args, q.After.CreatedAt, q.After.ID)
		where += fmt.Sprintf(
			" AND (created_at, id) < ($%d, $%d)",
			len(args)-1,
			len(args),
		)
	}

	args = append(args, q.Limit+1)

	query := fmt.Sprintf(
		"SELECT %s FROM students%s ORDER BY created_at DESC, id DESC LIMIT $%d",
		studentColumns,
		where,
		len(args),
	)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mengambil daftar student: %w", err)
	}
	defer rows.Close()

	result := []model.Student{}

	for rows.Next() {
		u, err := scanStudent(rows)
		if err != nil {
			return nil, fmt.Errorf("membaca row student: %w", err)
		}

		result = append(result, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("membaca hasil query student: %w", err)
	}

	return result, nil
}

func scanStudent(row pgx.Row) (model.Student, error) {
	var s model.Student
	err := row.Scan(
		&s.ID,
		&s.NIM,
		&s.Name,
		&s.Grade,
		&s.IsActive,
		&s.OwnerID,
		&s.CreatedAt,
	)
	return s, err
}

func (r *studentPostgresRepository) Delete(ctx context.Context, id int) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM students WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("menghapus student: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
