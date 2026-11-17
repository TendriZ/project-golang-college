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

// List godoc
//
// @Summary      Daftar mahasiswa (cursor pagination)
// @Description  Mengembalikan maksimal `limit` baris dan penanda has_more. Kirim ulang next_cursor sebagai `cursor` untuk halaman berikutnya. Meminta Accept: text/csv menghasilkan unduhan CSV.
// @Tags         students
// @Produce      json
// @Produce      text/csv
// @Param        limit      query     int     false  "Jumlah baris per halaman (1-100, bawaan 10)"
// @Param        search     query     string  false  "Cari pada nama atau NIM"
// @Param        is_active  query     bool    false  "Saring status aktif"
// @Param        min_grade  query     number  false  "Nilai minimum"
// @Param        max_grade  query     number  false  "Nilai maksimum"
// @Param        cursor     query     string  false  "Nilai next_cursor dari halaman sebelumnya"
// @Success      200        {object}  model.StudentPage
// @Failure      400        {object}  model.ErrorResponse  "Cursor atau filter tidak valid"
// @Failure      401        {object}  model.ErrorResponse
// @Failure      403        {object}  model.ErrorResponse  "Tidak memiliki permission student:list"
// @Failure      406        {object}  model.ErrorResponse  "Format yang diminta tidak didukung"
// @Security     BearerAuth
// @Router       /students [get]
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

	return c.Status(fiber.StatusOK).JSON(model.StudentPage{
		Success: true,
		Message: "daftar mahasiswa berhasil diambil",
		Data:    rows,
		Meta:    *meta,
	})
}

// Get godoc
//
// @Summary      Detail satu mahasiswa
// @Description  Pemilik data selalu boleh; pengguna lain perlu permission student:read:any.
// @Tags         students
// @Produce      json
// @Param        id   path      int  true  "ID mahasiswa"
// @Success      200  {object}  model.WebResponse{data=model.Student}
// @Failure      400  {object}  model.ErrorResponse
// @Failure      401  {object}  model.ErrorResponse
// @Failure      403  {object}  model.ErrorResponse  "Bukan pemilik dan tanpa permission :any"
// @Failure      404  {object}  model.ErrorResponse
// @Security     BearerAuth
// @Router       /students/{id} [get]
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

// Create godoc
//
// @Summary      Membuat mahasiswa
// @Description  owner_id selalu diambil dari token, bukan dari body (mencegah mass assignment).
// @Tags         students
// @Accept       json
// @Produce      json
// @Param        body  body      model.CreateStudentRequest  true  "Data mahasiswa"
// @Success      201   {object}  model.WebResponse{data=model.Student}
// @Failure      400   {object}  model.ErrorResponse
// @Failure      401   {object}  model.ErrorResponse
// @Failure      403   {object}  model.ErrorResponse  "Tidak memiliki permission student:create"
// @Failure      409   {object}  model.ErrorResponse  "NIM sudah dipakai"
// @Failure      422   {object}  model.ErrorResponse  "Validasi gagal"
// @Security     BearerAuth
// @Router       /students [post]
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

// Replace godoc
//
// @Summary      Mengganti seluruh isi mahasiswa
// @Description  PUT bersifat idempoten: field yang tidak dikirim dikembalikan ke nilai nol.
// @Tags         students
// @Accept       json
// @Produce      json
// @Param        id    path      int                         true  "ID mahasiswa"
// @Param        body  body      model.ReplaceStudentRequest  true  "Isi lengkap mahasiswa"
// @Success      200   {object}  model.WebResponse{data=model.Student}
// @Failure      400   {object}  model.ErrorResponse
// @Failure      401   {object}  model.ErrorResponse
// @Failure      403   {object}  model.ErrorResponse
// @Failure      404   {object}  model.ErrorResponse
// @Failure      409   {object}  model.ErrorResponse  "NIM sudah dipakai mahasiswa lain"
// @Failure      422   {object}  model.ErrorResponse
// @Security     BearerAuth
// @Router       /students/{id} [put]
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

// Patch godoc
//
// @Summary      Mengubah sebagian field mahasiswa
// @Description  Hanya field yang dikirim yang berubah. Body kosong ditolak dengan 400.
// @Tags         students
// @Accept       json
// @Produce      json
// @Param        id    path      int                       true  "ID mahasiswa"
// @Param        body  body      model.PatchStudentRequest  true  "Field yang diubah"
// @Success      200   {object}  model.WebResponse{data=model.Student}
// @Failure      400   {object}  model.ErrorResponse  "Body kosong atau bukan JSON"
// @Failure      401   {object}  model.ErrorResponse
// @Failure      403   {object}  model.ErrorResponse
// @Failure      404   {object}  model.ErrorResponse
// @Failure      409   {object}  model.ErrorResponse  "NIM sudah dipakai mahasiswa lain"
// @Failure      422   {object}  model.ErrorResponse
// @Security     BearerAuth
// @Router       /students/{id} [patch]
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

// Delete godoc
//
// @Summary      Menghapus mahasiswa
// @Tags         students
// @Produce      json
// @Param        id   path  int  true  "ID mahasiswa"
// @Success      204  "Tidak ada body"
// @Failure      401  {object}  model.ErrorResponse
// @Failure      403  {object}  model.ErrorResponse  "Tidak memiliki permission student:delete"
// @Failure      404  {object}  model.ErrorResponse
// @Security     BearerAuth
// @Router       /students/{id} [delete]
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
