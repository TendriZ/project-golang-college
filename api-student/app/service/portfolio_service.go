package service

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
	"api-student/app/repository"
	"api-student/helper"
)

// PortfolioService memakai DUA database sekaligus: data mahasiswa dibaca dari
// PostgreSQL, portofolio disimpan di MongoDB. Service-lah satu-satunya tempat
// yang mengenal keduanya — pola yang sama dengan ReviewService di Modul 10.
type PortfolioService struct {
	portfolios repository.PortfolioRepository
	students   repository.StudentRepository
	perms      *helper.PermissionSet
}

func NewPortfolioService(
	portfolios repository.PortfolioRepository,
	students repository.StudentRepository,
	perms *helper.PermissionSet,
) *PortfolioService {
	return &PortfolioService{
		portfolios: portfolios,
		students:   students,
		perms:      perms,
	}
}

// ---------- GET /students/:id/portfolio ----------
func (s *PortfolioService) Get(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	studentID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id mahasiswa harus berupa angka positif")
	}

	// Mahasiswa harus ada di PostgreSQL — tidak ada FOREIGN KEY lintas
	// database, jadi service harus memeriksanya sendiri.
	if _, err := s.students.FindByID(ctx, studentID); err != nil {
		return translatePortfolioError(err, "mahasiswa")
	}

	// Kepemilikan atau permission :any.
	anyOwner := s.perms.Can(current.Role, "portfolio:read:any")
	if current.UserID != studentID && !anyOwner {
		return helper.Forbidden("tidak berhak mengakses portofolio mahasiswa ini")
	}

	portfolio, err := s.portfolios.FindByStudent(ctx, studentID)
	if err != nil {
		return translatePortfolioError(err, "portofolio")
	}

	return helper.Success(c, fiber.StatusOK, "portofolio ditemukan", portfolio)
}

// ---------- PUT /students/:id/portfolio ----------
// Membuat atau mengganti seluruh portofolio (upsert). Tolak bila mahasiswa
// tidak ada di PostgreSQL.
func (s *PortfolioService) Upsert(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	studentID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id mahasiswa harus berupa angka positif")
	}

	if _, err := s.students.FindByID(ctx, studentID); err != nil {
		return translatePortfolioError(err, "mahasiswa")
	}

	anyOwner := s.perms.Can(current.Role, "portfolio:update:any")
	if current.UserID != studentID && !anyOwner {
		return helper.Forbidden("tidak berhak mengubah portofolio mahasiswa ini")
	}

	var req model.UpdatePortfolioRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	if req.Headline == nil && req.Bio == nil {
		return helper.BadRequest("tidak ada field yang diubah")
	}

	if req.Headline != nil {
		trimmed := strings.TrimSpace(*req.Headline)
		req.Headline = &trimmed
	}
	if req.Bio != nil {
		trimmed := strings.TrimSpace(*req.Bio)
		req.Bio = &trimmed
	}

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	// Ambil portofolio lama untuk mempertahankan projects dan certificates
	// bila PUT hanya mengganti headline/bio. Jika portofolio belum ada,
	// buat yang baru dengan array kosong.
	existing, err := s.portfolios.FindByStudent(ctx, studentID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return translatePortfolioError(err, "portofolio")
	}

	projects := []model.Project{}
	certificates := []model.Certificate{}
	if err == nil {
		projects = existing.Projects
		certificates = existing.Certificates
	}

	headline := ""
	if req.Headline != nil {
		headline = *req.Headline
	}
	bio := ""
	if req.Bio != nil {
		bio = *req.Bio
	}

	updated, err := s.portfolios.Upsert(ctx, model.Portfolio{
		StudentID:    studentID,
		Headline:     headline,
		Bio:          bio,
		Projects:     projects,
		Certificates: certificates,
	})
	if err != nil {
		return translatePortfolioError(err, "portofolio")
	}

	return helper.Success(c, fiber.StatusOK, "portofolio berhasil disimpan", updated)
}

// ---------- POST /students/:id/portfolio/projects ----------
func (s *PortfolioService) AddProject(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	studentID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id mahasiswa harus berupa angka positif")
	}

	if _, err := s.students.FindByID(ctx, studentID); err != nil {
		return translatePortfolioError(err, "mahasiswa")
	}

	anyOwner := s.perms.Can(current.Role, "portfolio:manage:any")
	if current.UserID != studentID && !anyOwner {
		return helper.Forbidden("tidak berhak menambah proyek ke portofolio mahasiswa ini")
	}

	var req model.AddProjectRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	req.Teknologi = NormalizeTags(req.Teknologi)

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	updated, err := s.portfolios.AddProject(ctx, studentID, model.Project{
		Judul:     req.Judul,
		Teknologi: req.Teknologi,
		Tautan:    req.Tautan,
		Tahun:     req.Tahun,
	})
	if err != nil {
		return translatePortfolioError(err, "portofolio")
	}

	return helper.Success(c, fiber.StatusCreated, "proyek berhasil ditambahkan", updated)
}

// ---------- DELETE /students/:id/portfolio/projects/:projectId ----------
func (s *PortfolioService) DeleteProject(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	studentID, valid := helper.ParamID(c)
	if !valid {
		return helper.BadRequest("id mahasiswa harus berupa angka positif")
	}

	projectID := c.Params("projectId")
	if projectID == "" {
		return helper.BadRequest("id proyek harus diisi")
	}

	if _, err := s.students.FindByID(ctx, studentID); err != nil {
		return translatePortfolioError(err, "mahasiswa")
	}

	anyOwner := s.perms.Can(current.Role, "portfolio:manage:any")
	if current.UserID != studentID && !anyOwner {
		return helper.Forbidden("tidak berhak menghapus proyek dari portofolio mahasiswa ini")
	}

	updated, err := s.portfolios.DeleteProject(ctx, studentID, projectID)
	if err != nil {
		return translatePortfolioError(err, "portofolio")
	}

	return helper.Success(c, fiber.StatusOK, "proyek berhasil dihapus", updated)
}

// ---------- GET /portfolios/stats/technologies ----------
func (s *PortfolioService) StatsTechnologies(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	current, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	// Statistik gabungan membutuhkan permission :any.
	if !s.perms.Can(current.Role, "portfolio:read:any") {
		return helper.Forbidden("role Anda tidak memiliki hak portfolio:read:any")
	}

	stats, err := s.portfolios.StatsTechnologies(ctx)
	if err != nil {
		return helper.Internal(err)
	}

	return helper.Success(c, fiber.StatusOK, "statistik teknologi berhasil diambil", stats)
}

func translatePortfolioError(err error, entity string) error {
	switch {
	case errors.Is(err, repository.ErrInvalidID):
		return helper.BadRequest("id tidak valid")

	case errors.Is(err, repository.ErrNotFound):
		return helper.NotFound(entity + " tidak ditemukan")

	case errors.Is(err, repository.ErrProjectLimit):
		return helper.NewError(fiber.StatusConflict, "PROJECT_LIMIT",
			"jumlah proyek sudah mencapai batas")

	default:
		return helper.Internal(err)
	}
}
