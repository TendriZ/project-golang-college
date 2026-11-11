package service

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
	"api-student/app/repository"
	"api-student/helper"
)

// SupervisionService mengatur penetapan dosen pembimbing tugas akhir.
//
// Seluruh operasi yang mengubah lebih dari satu tabel dibungkus WithTx.
// repository.WithTx(tx) menghasilkan salinan repository yang memakai
// koneksi transaction tersebut — bukan pool — sehingga seluruh langkah
// ikut commit atau ikut rollback bersama-sama.
type SupervisionService struct {
	students     repository.StudentRepository
	supervisors  repository.SupervisorRepository
	supervisions repository.SupervisionRepository
	outbox       repository.OutboxRepository
	tx           *repository.TxManager
}

func NewSupervisionService(
	students repository.StudentRepository,
	supervisors repository.SupervisorRepository,
	supervisions repository.SupervisionRepository,
	outbox repository.OutboxRepository,
	tx *repository.TxManager,
) *SupervisionService {
	return &SupervisionService{
		students:     students,
		supervisors:  supervisors,
		supervisions: supervisions,
		outbox:       outbox,
		tx:           tx,
	}
}

type assignSupervisionResponse struct {
	StudentID  int              `json:"student_id"`
	Supervisor model.Supervisor `json:"supervisor"`
}

type switchSupervisionResponse struct {
	StudentID            int              `json:"student_id"`
	PreviousSupervisorID int              `json:"previous_supervisor_id"`
	Supervisor           model.Supervisor `json:"supervisor"`
}

// ---------- POST /supervisors/:id/assign ----------
func (s *SupervisionService) Assign(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	supervisorID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id pembimbing harus berupa angka positif")
	}

	var req model.AssignSupervisionRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	var result assignSupervisionResponse

	err := s.tx.WithTx(ctx, func(tx repository.DBTX) error {
		students := s.students.WithTx(tx)
		supervisors := s.supervisors.WithTx(tx)
		supervisions := s.supervisions.WithTx(tx)

		// 1. Kunci row mahasiswa. Request lain untuk mahasiswa YANG SAMA
		//    harus antre sampai transaction ini selesai. Tanpa langkah
		//    ini, dua request dapat sama-sama lolos pemeriksaan
		//    "belum punya pembimbing" sebelum salah satunya menulis.
		if _, err := students.LockByID(ctx, req.StudentID); err != nil {
			return err
		}

		// 2. Periksa apakah mahasiswa sudah punya pembimbing. Aman karena
		//    langkah 1 memegang kunci row mahasiswa tersebut.
		_, err := supervisions.FindByStudent(ctx, req.StudentID)
		if err == nil {
			return repository.ErrAlreadySupervised
		}
		if !errors.Is(err, repository.ErrNotSupervised) {
			return err
		}

		// 3. Ambil satu slot bimbingan secara ATOMIK. Bila kuota penuh,
		//    UPDATE mengenai 0 baris dan ErrSupervisorFull dikembalikan.
		supervisor, err := supervisors.IncrementSupervised(ctx, supervisorID)
		if err != nil {
			return err
		}

		// 4. Catat penetapannya. Bila UNIQUE (student_id) ternyata
		//    terpicu, error di sini membatalkan juga pengambilan slot
		//    pada langkah 3. Tidak perlu kode "kembalikan slot".
		if err := supervisions.Assign(ctx, req.StudentID, supervisorID); err != nil {
			return err
		}

		result = assignSupervisionResponse{
			StudentID:  req.StudentID,
			Supervisor: supervisor,
		}
		return nil
	})
	if err != nil {
		return translateSupervisionError(err)
	}

	return helper.Created(
		c,
		"pembimbing berhasil ditetapkan",
		result,
		"/api/v1/supervisors/"+strconv.Itoa(supervisorID),
	)
}

// ---------- POST /supervisions/switch ----------
func (s *SupervisionService) Switch(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.SwitchSupervisionRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	var result switchSupervisionResponse

	err := s.tx.WithTx(ctx, func(tx repository.DBTX) error {
		students := s.students.WithTx(tx)
		supervisors := s.supervisors.WithTx(tx)
		supervisions := s.supervisions.WithTx(tx)

		// 1. Kunci row mahasiswa lebih dulu. Inilah kunci tambahan yang
		//    menjawab D.4 nomor 1: UPDATE bersyarat menjaga kuota, tetapi
		//    ia tidak menjamin bahwa pembacaan "pembimbing lama" masih
		//    berlaku ketika baris supervisions diubah.
		if _, err := students.LockByID(ctx, req.StudentID); err != nil {
			return err
		}

		// 2. Baca pembimbing lama. Aman karena langkah 1 sudah mengunci
		//    seluruh perubahan penetapan milik mahasiswa ini.
		current, err := supervisions.FindByStudent(ctx, req.StudentID)
		if err != nil {
			return err
		}
		if current.SupervisorID == req.SupervisorID {
			return repository.ErrSameSupervisor
		}

		// 3. Kunci KEDUA pembimbing dalam urutan id menaik. Tanpa urutan
		//    yang seragam, mahasiswa A yang pindah 1 -> 2 dan mahasiswa B
		//    yang pindah 2 -> 1 pada saat yang sama dapat saling
		//    menunggu kunci milik lawannya: deadlock.
		if _, err := supervisors.LockByIDs(
			ctx, current.SupervisorID, req.SupervisorID,
		); err != nil {
			return err
		}

		// 4. Ambil slot pembimbing BARU lebih dulu. Bila penuh, error di
		//    sini membatalkan seluruh transaction, sehingga mahasiswa
		//    TETAP berada di pembimbing lama. Inilah atomicity yang
		//    diminta D.2 nomor 3.
		updated, err := supervisors.IncrementSupervised(ctx, req.SupervisorID)
		if err != nil {
			return err
		}

		// 5. Baru lepaskan slot pembimbing lama, lalu pindahkan baris
		//    supervisions. Ketiganya berada dalam transaction yang sama.
		if _, err := supervisors.DecrementSupervised(ctx, current.SupervisorID); err != nil {
			return err
		}
		if err := supervisions.Move(ctx, req.StudentID, req.SupervisorID); err != nil {
			return err
		}

		result = switchSupervisionResponse{
			StudentID:            req.StudentID,
			PreviousSupervisorID: current.SupervisorID,
			Supervisor:           updated,
		}
		return nil
	})
	if err != nil {
		return translateSupervisionError(err)
	}

	return helper.Success(c, fiber.StatusOK, "pembimbing berhasil diganti", result)
}

// ---------- PATCH /supervisors/:id ----------
func (s *SupervisionService) UpdateQuota(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id pembimbing harus berupa angka positif")
	}

	var req model.UpdateSupervisorQuotaRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	var updated model.Supervisor

	// Sebelum ada notifikasi, UpdateQuota hanyalah SATU pernyataan SQL dan
	// tidak membutuhkan transaction. Sekarang ia menjadi DUA pernyataan:
	// mengubah kuota DAN mencatat event. Keduanya wajib berada di dalam
	// satu transaction, karena tanpa itu hanya ada dua kemungkinan buruk:
	//
	//   - kuota tersimpan tetapi event gagal dicatat  -> perubahan berlaku
	//     tanpa satu pun email, dan tidak ada cara menemukannya kembali;
	//   - event tercatat tetapi kuota gagal tersimpan -> email mengumumkan
	//     kuota yang tidak pernah berlaku.
	//
	// Dengan satu transaction, keduanya tersimpan bersama atau batal
	// bersama. Perhatikan juga: fn ini TIDAK mengirim email dan TIDAK
	// menyentuh Redis — hanya menulis dua baris database. Itu penting
	// karena WithTx dapat menjalankan fn lebih dari sekali saat deadlock
	// atau serialization failure.
	err := s.tx.WithTx(ctx, func(tx repository.DBTX) error {
		var err error
		updated, err = s.supervisors.WithTx(tx).UpdateQuota(
			ctx, id, req.Quota, req.Version,
		)
		if err != nil {
			return err
		}

		// Nama pembimbing disalin ke dalam event agar notifikasi tetap
		// menyebut nama yang berlaku saat perubahan terjadi, walaupun
		// namanya diubah kemudian.
		return s.outbox.WithTx(tx).Insert(ctx, model.EventQuotaChanged,
			model.QuotaChangedEvent{
				SupervisorID:   updated.ID,
				SupervisorName: updated.Name,
				Quota:          updated.Quota,
			})
	})
	if err != nil {
		return translateSupervisionError(err)
	}

	return helper.Success(c, fiber.StatusOK, "kuota pembimbing berhasil diubah", updated)
}

// translateSupervisionError memetakan kegagalan aturan bisnis menjadi 409
// dengan kode yang berbeda-beda. Semua permintaan di sini sah secara bentuk;
// yang menolak adalah keadaan data saat ini.
func translateSupervisionError(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return helper.NotFound("mahasiswa atau pembimbing tidak ditemukan")

	case errors.Is(err, repository.ErrSupervisorFull):
		return helper.NewError(fiber.StatusConflict, "SUPERVISOR_FULL",
			"kuota pembimbing sudah penuh")

	case errors.Is(err, repository.ErrAlreadySupervised):
		return helper.NewError(fiber.StatusConflict, "ALREADY_SUPERVISED",
			"mahasiswa sudah memiliki pembimbing")

	case errors.Is(err, repository.ErrNotSupervised):
		return helper.NewError(fiber.StatusConflict, "NOT_SUPERVISED",
			"mahasiswa belum memiliki pembimbing")

	case errors.Is(err, repository.ErrSameSupervisor):
		return helper.NewError(fiber.StatusConflict, "SAME_SUPERVISOR",
			"mahasiswa sudah dibimbing dosen tersebut")

	case errors.Is(err, repository.ErrVersionConflict):
		return helper.NewError(fiber.StatusConflict, "VERSION_CONFLICT",
			"data pembimbing sudah diubah pihak lain, muat ulang lalu coba lagi")

	case errors.Is(err, repository.ErrQuotaBelowSupervised):
		return helper.NewError(fiber.StatusConflict, "QUOTA_BELOW_SUPERVISED",
			"kuota tidak boleh lebih kecil dari jumlah bimbingan saat ini")

	default:
		return helper.Internal(err)
	}
}
