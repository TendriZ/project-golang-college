package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"api-student/app/model"
)

// Sentinel error khusus pembimbing dan penetapannya.
var (
	ErrSupervisorFull       = errors.New("kuota pembimbing sudah penuh")
	ErrAlreadySupervised    = errors.New("mahasiswa sudah memiliki pembimbing")
	ErrNotSupervised        = errors.New("mahasiswa belum memiliki pembimbing")
	ErrSameSupervisor       = errors.New("mahasiswa sudah dibimbing dosen tersebut")
	ErrVersionConflict      = errors.New("data sudah diubah pihak lain")
	ErrQuotaBelowSupervised = errors.New("kuota lebih kecil dari jumlah bimbingan")
)

// SupervisorRepository adalah kontrak penyimpanan dosen pembimbing.
type SupervisorRepository interface {
	// WithTx mengembalikan salinan repository yang menjalankan seluruh
	// query-nya di dalam transaction tx.
	WithTx(tx DBTX) SupervisorRepository

	FindByID(ctx context.Context, id int) (model.Supervisor, error)

	// LockByIDs mengunci beberapa pembimbing sekaligus, SELALU dalam
	// urutan id menaik, apa pun urutan argumennya.
	LockByIDs(ctx context.Context, ids ...int) ([]model.Supervisor, error)

	IncrementSupervised(ctx context.Context, id int) (model.Supervisor, error)
	DecrementSupervised(ctx context.Context, id int) (model.Supervisor, error)
	UpdateQuota(ctx context.Context, id, quota, version int) (model.Supervisor, error)
}

const supervisorColumns = `id, name, quota, supervised_count, version, created_at`

type supervisorPostgresRepository struct{ db DBTX }

func NewSupervisorRepository(db DBTX) SupervisorRepository {
	return &supervisorPostgresRepository{db: db}
}

func (r *supervisorPostgresRepository) WithTx(tx DBTX) SupervisorRepository {
	return &supervisorPostgresRepository{db: tx}
}

func scanSupervisor(row pgx.Row) (model.Supervisor, error) {
	var s model.Supervisor
	err := row.Scan(&s.ID, &s.Name, &s.Quota, &s.SupervisedCount,
		&s.Version, &s.CreatedAt)
	return s, err
}

func (r *supervisorPostgresRepository) FindByID(
	ctx context.Context, id int,
) (model.Supervisor, error) {
	s, err := scanSupervisor(r.db.QueryRow(ctx,
		"SELECT "+supervisorColumns+" FROM supervisors WHERE id = $1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Supervisor{}, ErrNotFound
		}
		return model.Supervisor{}, fmt.Errorf("mengambil pembimbing: %w", err)
	}
	return s, nil
}

// LockByIDs mengunci beberapa pembimbing sekaligus dalam urutan id menaik.
//
// Urutan yang seragam inilah yang mencegah deadlock pada operasi pindah
// pembimbing: bila mahasiswa A pindah dari pembimbing 1 ke 2 dan mahasiswa
// B pindah dari 2 ke 1 pada saat yang sama, tanpa urutan keduanya dapat
// saling menunggu kunci yang dipegang lawannya.
func (r *supervisorPostgresRepository) LockByIDs(
	ctx context.Context, ids ...int,
) ([]model.Supervisor, error) {
	rows, err := r.db.Query(ctx,
		"SELECT "+supervisorColumns+" FROM supervisors "+
			"WHERE id = ANY($1) ORDER BY id FOR UPDATE", ids)
	if err != nil {
		return nil, fmt.Errorf("mengunci pembimbing: %w", err)
	}
	defer rows.Close()

	result := []model.Supervisor{}
	for rows.Next() {
		s, err := scanSupervisor(rows)
		if err != nil {
			return nil, fmt.Errorf("membaca row pembimbing: %w", err)
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mengunci pembimbing: %w", err)
	}
	if len(result) != len(ids) {
		return nil, ErrNotFound
	}
	return result, nil
}

// IncrementSupervised mengambil satu slot bimbingan secara ATOMIK.
//
// Pemeriksaan kuota dan penambahan jumlah bimbingan terjadi dalam satu
// pernyataan UPDATE. PostgreSQL mengunci row tersebut selama UPDATE
// berjalan, sehingga dua request yang datang bersamaan tidak mungkin
// sama-sama melihat "masih ada satu slot kosong".
func (r *supervisorPostgresRepository) IncrementSupervised(
	ctx context.Context, id int,
) (model.Supervisor, error) {
	s, err := scanSupervisor(r.db.QueryRow(ctx,
		`UPDATE supervisors SET supervised_count = supervised_count + 1
         WHERE id = $1 AND supervised_count < quota
         RETURNING `+supervisorColumns, id))
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return model.Supervisor{}, fmt.Errorf("menambah jumlah bimbingan: %w", err)
	}

	// Tidak ada row yang berubah. Dua kemungkinan: pembimbingnya tidak
	// ada, atau ada tetapi kuotanya sudah penuh. Bedakan keduanya.
	if _, findErr := r.FindByID(ctx, id); findErr != nil {
		return model.Supervisor{}, findErr
	}
	return model.Supervisor{}, ErrSupervisorFull
}

// DecrementSupervised melepas satu slot bimbingan.
//
// Syarat supervised_count > 0 menjaga agar kolom ini tidak pernah menjadi
// negatif. Bila syarat itu tidak terpenuhi padahal baris supervisions
// sudah dihapus/dipindahkan, berarti data kita tidak konsisten — lebih
// baik menjadi error 500 daripada diam-diam menyimpan angka mustahil.
func (r *supervisorPostgresRepository) DecrementSupervised(
	ctx context.Context, id int,
) (model.Supervisor, error) {
	s, err := scanSupervisor(r.db.QueryRow(ctx,
		`UPDATE supervisors SET supervised_count = supervised_count - 1
         WHERE id = $1 AND supervised_count > 0
         RETURNING `+supervisorColumns, id))
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return model.Supervisor{}, fmt.Errorf("mengurangi jumlah bimbingan: %w", err)
	}

	if _, findErr := r.FindByID(ctx, id); findErr != nil {
		return model.Supervisor{}, findErr
	}
	return model.Supervisor{}, errors.New("jumlah bimbingan tidak konsisten")
}

// UpdateQuota menerapkan optimistic locking.
//
// Tidak ada kunci yang dipegang selama admin mengisi form. Sebagai
// gantinya, UPDATE hanya berhasil bila version di database masih sama
// dengan version yang dikirim client. Bila sudah berubah, berarti ada
// orang lain yang menyimpan lebih dulu, dan perubahan ini ditolak alih-alih
// menimpanya.
//
// Syarat kuota tidak boleh lebih kecil dari jumlah bimbingan juga ditulis
// di dalam WHERE, agar pemeriksaan dan perubahan terjadi dalam satu langkah.
func (r *supervisorPostgresRepository) UpdateQuota(
	ctx context.Context, id, quota, version int,
) (model.Supervisor, error) {
	s, err := scanSupervisor(r.db.QueryRow(ctx,
		`UPDATE supervisors SET quota = $1, version = version + 1
         WHERE id = $2 AND version = $3 AND $1 >= supervised_count
         RETURNING `+supervisorColumns, quota, id, version))
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return model.Supervisor{}, fmt.Errorf("mengubah kuota pembimbing: %w", err)
	}

	current, findErr := r.FindByID(ctx, id)
	if findErr != nil {
		return model.Supervisor{}, findErr
	}
	if current.Version != version {
		return model.Supervisor{}, ErrVersionConflict
	}
	return model.Supervisor{}, ErrQuotaBelowSupervised
}
