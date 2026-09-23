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

	q := helper.ParseListQuery(c)

	students, total, err := s.repo.FindAll(ctx, q)
	if err != nil {
		return helper.Fail(
			c,
			fiber.StatusInternalServerError,
			"gagal mengambil data mahasiswa",
		)
	}

	// Logika membulatkan ke atas untuk total halaman
	totalPages := 0
	if q.Limit > 0 {
		totalPages = (total + q.Limit - 1) / q.Limit
	}

	return helper.SuccessList(c, "daftar mahasiswa berhasil diambil", students, &model.Meta{
		Page:       q.Page,
		Limit:      q.Limit,
		Total:      total,
		TotalPages: totalPages,
	})
}

func (s *StudentService) Get(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Fail(c, fiber.StatusUnauthorized, "belum terautentikasi")
	}

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.Fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}

	student, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(c, err, "gagal mengambil data mahasiswa")
	}

	// Hak bergantung pada kepemilikan data: pemilik selalu boleh,
	// selain itu perlu permission student:read:any.
	if !CanAccessStudent(current, student.OwnerID, s.perms, "student:read:any") {
		return helper.Fail(c, fiber.StatusForbidden,
			"tidak berhak mengakses data mahasiswa lain")
	}

	return helper.Success(c, fiber.StatusOK, "mahasiswa ditemukan", student)
}

func (s *StudentService) Create(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Fail(c, fiber.StatusUnauthorized, "belum terautentikasi")
	}

	var req model.CreateStudentRequest

	if err := c.BodyParser(&req); err != nil {
		return helper.Fail(
			c,
			fiber.StatusBadRequest,
			"body harus berupa JSON yang valid",
		)
	}

	req.Name = strings.TrimSpace(req.Name)

	// Business rulesnya dipanggil dari student_rules.go, bukan ditulis ulang di sini.
	if errs := ValidateCreate(req); len(errs) > 0 {
		return helper.FailValidation(c, errs)
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
		return translateError(c, err, "gagal menyimpan mahasiswa")
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
		return helper.Fail(c, fiber.StatusUnauthorized, "belum terautentikasi")
	}

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.Fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}

	// Pemilik data perlu diketahui dulu sebelum hak diputuskan:
	// ambil baris lama, lalu cek CanAccessStudent.
	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(c, err, "gagal mengambil data mahasiswa")
	}
	if !CanAccessStudent(current, existing.OwnerID, s.perms, "student:update:any") {
		return helper.Fail(c, fiber.StatusForbidden,
			"tidak berhak mengubah data mahasiswa lain")
	}

	var req model.ReplaceStudentRequest

	if err := c.BodyParser(&req); err != nil {
		return helper.Fail(
			c,
			fiber.StatusBadRequest,
			"body harus berupa JSON yang valid",
		)
	}

	req.Name = strings.TrimSpace(req.Name)

	// Validasi dipanggil dari rules
	if errs := ValidateReplace(req); len(errs) > 0 {
		return helper.FailValidation(c, errs)
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
		return translateError(c, err, "gagal memperbarui mahasiswa")
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
		return helper.Fail(c, fiber.StatusUnauthorized, "belum terautentikasi")
	}

	id, valid := helper.ParamID(c)
	if !valid {
		return helper.Fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}

	var req model.PatchStudentRequest

	if err := c.BodyParser(&req); err != nil {
		return helper.Fail(
			c,
			fiber.StatusBadRequest,
			"body harus berupa JSON yang valid",
		)
	}

	// Mengecek apakah request payload kosong
	if req.Name == nil && req.NIM == nil && req.Grade == nil && req.IsActive == nil {
		return helper.Fail(c, fiber.StatusBadRequest, "tidak ada field yang diubah")
	}

	current, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return translateError(c, err, "gagal mengambil data mahasiswa")
	}

	// Cek hak SETELAH data diambil (owner baru diketahui di sini),
	// SEBELUM perubahan diterapkan.
	if !CanAccessStudent(currentUser, current.OwnerID, s.perms, "student:update:any") {
		return helper.Fail(c, fiber.StatusForbidden,
			"tidak berhak mengubah data mahasiswa lain")
	}

	// Penerapan data dan validasinya dipanggil dari rules.
	// ApplyPatch tidak menyentuh OwnerID sehingga pemilik tidak bisa diganti.
	updated, errs := ApplyPatch(current, req)
	if len(errs) > 0 {
		return helper.FailValidation(c, errs)
	}

	result, err := s.repo.Update(ctx, updated)
	if err != nil {
		return translateError(c, err, "gagal memperbarui mahasiswa")
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
		return helper.Fail(c, fiber.StatusBadRequest, "id harus berupa angka positif")
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return translateError(c, err, "gagal menghapus mahasiswa")
	}

	return helper.NoContent(c)
}

// translateError memetakan error milik repository menjadi status HTTP.
func translateError(c *fiber.Ctx, err error, generalMessage string) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return helper.Fail(c, fiber.StatusNotFound, "mahasiswa tidak ditemukan")

	case errors.Is(err, repository.ErrDuplicate):
		return helper.Fail(c, fiber.StatusConflict, "nim sudah digunakan")

	default:
		return helper.Fail(c, fiber.StatusInternalServerError, generalMessage)
	}
}
