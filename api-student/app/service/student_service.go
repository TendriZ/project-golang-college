package service

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
	"api-student/app/repository"
	"api-student/helper"
)

// StudentService memegang dua tanggung jawab sekaligus pada struktur baku
// mata kuliah ini: menerima *fiber.Ctx (peran controller) dan menjalankan
// business rules (peran use case).
type StudentService struct {
	repo  repository.StudentRepository
	perms *helper.PermissionSet
}

// NewStudentService menerima INTERFACE, bukan struct konkret.
func NewStudentService(repo repository.StudentRepository, perms *helper.PermissionSet) *StudentService {
	return &StudentService{repo: repo, perms: perms}
}

func (s *StudentService) List(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	// Format dipilih SEBELUM query dijalankan. Bila client meminta format
	// yang tidak dapat kita hasilkan, tidak ada gunanya membebani database
	// untuk hasil yang akan dibuang.
	format, err := helper.Negotiate(c, helper.FormatJSON, helper.FormatCSV)
	if err != nil {
		return err
	}

	q, err := helper.ParseCursorQuery(c)
	if err != nil {
		return err // AppError 400 bila cursor rusak
	}

	// limit+1: baris tambahan hanya penanda "masih ada halaman
	// berikutnya", bukan bagian dari halaman ini.
	rows, err := s.repo.FindAfterCursor(ctx, q)
	if err != nil {
		return helper.Internal(err)
	}

	hasMore := len(rows) > q.Limit
	if hasMore {
		rows = rows[:q.Limit]
	}

	if format == helper.FormatCSV {
		return helper.WriteStudentsCSV(c, rows)
	}

	meta := &model.CursorMeta{Limit: q.Limit, HasMore: hasMore}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		meta.NextCursor = helper.EncodeCursor(last.CreatedAt, last.ID)
	}

	return helper.SuccessCursor(c, "daftar mahasiswa berhasil diambil", rows, meta)
}

func (s *StudentService) Get(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	student, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(err, "mahasiswa")
	}

	// Hak bergantung pada kepemilikan data: pemilik selalu boleh,
	// selain itu perlu permission student:read:any.
	if !CanAccessStudent(current, student.OwnerID, s.perms, "student:read:any") {
		return helper.Forbidden("tidak berhak mengakses data mahasiswa lain")
	}

	return helper.Success(c, fiber.StatusOK, "mahasiswa ditemukan", student)
}

func (s *StudentService) Create(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	var req model.CreateStudentRequest

	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	req.Name = strings.TrimSpace(req.Name)

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	// owner_id diisi OTOMATIS dari identitas pemanggil. Struct request
	// TIDAK punya field OwnerID sehingga client tidak bisa mengaku-aku
	// sebagai pemilik lain (mass assignment). Mengizinkan client mengisi
	// owner sendiri berarti siapa pun bisa mengklaim data milik orang lain.
	newStudent, err := s.repo.Create(ctx, model.Student{
		NIM:      req.NIM,
		Name:     req.Name,
		Grade:    req.Grade,
		IsActive: true,
		OwnerID:  current.UserID,
	})
	if err != nil {
		return translateError(err, "mahasiswa")
	}

	return helper.Created(
		c,
		"mahasiswa berhasil dibuat",
		newStudent,
		"/api/v1/students/"+strconv.Itoa(newStudent.ID),
	)
}

func (s *StudentService) Replace(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	// Pemilik data perlu diketahui dulu sebelum hak diputuskan:
	// ambil baris lama, lalu cek CanAccessStudent.
	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(err, "mahasiswa")
	}
	if !CanAccessStudent(current, existing.OwnerID, s.perms, "student:update:any") {
		return helper.Forbidden("tidak berhak mengubah data mahasiswa lain")
	}

	var req model.ReplaceStudentRequest

	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	req.Name = strings.TrimSpace(req.Name)

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	result, err := s.repo.Update(ctx, model.Student{
		ID:       id,
		NIM:      req.NIM,
		Name:     req.Name,
		Grade:    req.Grade,
		IsActive: req.IsActive,
		OwnerID:  existing.OwnerID, // dipertahankan, tidak dari body
	})
	if err != nil {
		return translateError(err, "mahasiswa")
	}

	return helper.Success(
		c,
		fiber.StatusOK,
		"mahasiswa berhasil diganti seluruhnya",
		result,
	)
}

func (s *StudentService) Patch(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	currentUser, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	var req model.PatchStudentRequest

	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	// Aturan tag (omitnil, min, nodigit) diperiksa di sini. Tanpa baris ini
	// {"name":""} lolos dan menimpa data dengan string kosong.
	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	if IsEmptyPatch(req) {
		return helper.BadRequest("tidak ada field yang diubah")
	}

	current, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(err, "mahasiswa")
	}

	// Cek hak SETELAH data diambil (owner baru diketahui di sini),
	// SEBELUM perubahan diterapkan.
	if !CanAccessStudent(currentUser, current.OwnerID, s.perms, "student:update:any") {
		return helper.Forbidden("tidak berhak mengubah data mahasiswa lain")
	}

	// ApplyPatch kini hanya menggabungkan; pemeriksaan bentuk sudah
	// selesai dikerjakan tag sebelum fungsi ini dipanggil.
	updated := ApplyPatch(current, req)

	result, err := s.repo.Update(ctx, updated)
	if err != nil {
		return translateError(err, "mahasiswa")
	}

	return helper.Success(
		c,
		fiber.StatusOK,
		"mahasiswa berhasil diperbarui sebagian",
		result,
	)
}

func (s *StudentService) Delete(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id harus berupa angka positif")
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return translateError(err, "mahasiswa")
	}

	return helper.NoContent(c)
}

// translateError mengubah error milik repository menjadi AppError.
//
// Perhatikan tanda tangannya: tidak ada fiber.Ctx. Fungsi ini hanya
// menerjemahkan satu jenis error menjadi jenis lain, dan tidak tahu
// apa pun tentang HTTP. Yang tidak dikenali menjadi Internal — fail
// closed: lebih baik membalas 500 daripada menebak-nebak status.
func translateError(err error, entity string) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return helper.NotFound(entity + " tidak ditemukan")

	case errors.Is(err, repository.ErrDuplicate):
		return helper.Conflict("nim sudah digunakan")

	default:
		return helper.Internal(err)
	}
}
