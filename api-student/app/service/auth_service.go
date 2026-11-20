package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"api-student/app/model"
	"api-student/app/repository"
	"api-student/helper"
)

const refreshTokenBytes = 32

type AuthService struct {
	users      repository.UserRepository
	tokens     repository.TokenRepository
	jwt        *helper.JWTManager
	refreshTTL time.Duration
	perms      *helper.PermissionSet
}

func NewAuthService(
	users repository.UserRepository,
	tokens repository.TokenRepository,
	jwtManager *helper.JWTManager,
	refreshTTL time.Duration,
	perms *helper.PermissionSet,
) *AuthService {
	return &AuthService{users: users, tokens: tokens, jwt: jwtManager, refreshTTL: refreshTTL, perms: perms}
}

// Register godoc
//
// @Summary      Mendaftarkan akun baru
// @Description  Role akun baru selalu "user". Password minimal 8 karakter dan memuat huruf serta angka.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      model.RegisterRequest  true  "Data akun baru"
// @Success      201   {object}  model.WebResponse{data=model.User}
// @Failure      400   {object}  model.ErrorResponse  "Body bukan JSON yang valid"
// @Failure      409   {object}  model.ErrorResponse  "Username sudah dipakai"
// @Failure      422   {object}  model.ErrorResponse  "Validasi gagal; detail per field ada di fields"
// @Router       /auth/register [post]
func (s *AuthService) Register(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	hashed, err := helper.HashPassword(req.Password)
	if err != nil {
		return helper.Internal(err)
	}

	// Role SELALU "user" — tidak pernah diambil dari req (cegah mass assignment).
	created, err := s.users.Create(ctx, model.User{
		Username: req.Username,
		Email:    req.Email,
		Password: hashed,
		Role:     "user",
		IsActive: true,
	})
	if err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return helper.Conflict("username sudah dipakai")
		}
		return helper.Internal(err)
	}

	return helper.Created(c, "pendaftaran berhasil", created, "/api/v1/auth/me")
}

// Login godoc
//
// @Summary      Login dan dapatkan pasangan token
// @Description  Pesan kegagalan sengaja sama untuk username tidak ada dan password salah.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      model.LoginRequest  true  "Username dan password"
// @Success      200   {object}  model.WebResponse{data=model.TokenPair}
// @Failure      400   {object}  model.ErrorResponse
// @Failure      401   {object}  model.ErrorResponse  "Username atau password salah"
// @Failure      429   {object}  model.ErrorResponse  "Terlalu banyak percobaan login"
// @Router       /auth/login [post]
func (s *AuthService) Login(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	if errs := helper.ValidateStruct(req); errs != nil {
		return helper.Validation(errs)
	}

	user, err := s.users.FindByUsername(ctx, strings.TrimSpace(req.Username))
	if err != nil {
		// Username tidak ada — tetap jalankan pemeriksaan palsu + pesan SAMA.
		helper.VerifyDummyPassword(req.Password)
		return helper.Unauthorized("username atau password salah")
	}

	if !helper.VerifyPassword(user.Password, req.Password) {
		return helper.Unauthorized("username atau password salah")
	}

	if !user.IsActive {
		return helper.Forbidden("akun dinonaktifkan")
	}

	pair, err := s.issueTokenPair(ctx, user)
	if err != nil {
		return helper.Internal(err)
	}

	return helper.Success(c, fiber.StatusOK, "login berhasil", pair)
}

// Refresh godoc
//
// @Summary      Perbarui access token
// @Description  Refresh token lama langsung dicabut (rotasi); token yang dipakai dua kali ditolak.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      model.RefreshRequest  true  "Refresh token yang masih berlaku"
// @Success      200   {object}  model.WebResponse{data=model.TokenPair}
// @Failure      400   {object}  model.ErrorResponse
// @Failure      401   {object}  model.ErrorResponse  "Refresh token tidak valid atau kedaluwarsa"
// @Router       /auth/refresh [post]
func (s *AuthService) Refresh(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.RefreshRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}
	if strings.TrimSpace(req.RefreshToken) == "" {
		return helper.BadRequest("refresh_token wajib diisi")
	}

	hash := helper.SHA256Hex(req.RefreshToken)
	stored, err := s.tokens.FindActive(ctx, hash)
	if err != nil {
		return helper.Unauthorized("refresh token tidak valid atau sudah kedaluwarsa")
	}

	user, err := s.users.FindByID(ctx, stored.UserID)
	if err != nil || !user.IsActive {
		return helper.Unauthorized("akun tidak dapat dipakai")
	}

	// ROTASI: token lama langsung dicabut, diganti yang baru.
	if err := s.tokens.Revoke(ctx, hash); err != nil {
		return helper.Internal(err)
	}

	pair, err := s.issueTokenPair(ctx, user)
	if err != nil {
		return helper.Internal(err)
	}

	return helper.Success(c, fiber.StatusOK, "token berhasil diperbarui", pair)
}

// Logout godoc
//
// @Summary      Cabut refresh token
// @Description  Selalu menjawab 200, termasuk bila refresh token sudah tidak berlaku.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      model.RefreshRequest  true  "Refresh token yang akan dicabut"
// @Success      200   {object}  model.WebResponse
// @Failure      400   {object}  model.ErrorResponse
// @Router       /auth/logout [post]
func (s *AuthService) Logout(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	var req model.RefreshRequest
	if err := c.BodyParser(&req); err != nil {
		return helper.BadRequest("body harus berupa JSON yang valid")
	}

	if strings.TrimSpace(req.RefreshToken) != "" {
		_ = s.tokens.Revoke(ctx, helper.SHA256Hex(req.RefreshToken))
	}

	return helper.Success(c, fiber.StatusOK, "logout berhasil", nil)
}

// Me godoc
//
// @Summary      Profil pengguna yang sedang login
// @Description  Menyertakan daftar permission untuk kebutuhan tampilan frontend; pengamanan tetap di middleware.
// @Tags         auth
// @Produce      json
// @Success      200  {object}  model.WebResponse{data=model.Profile}
// @Failure      401  {object}  model.ErrorResponse
// @Security     BearerAuth
// @Router       /auth/me [get]
func (s *AuthService) Me(c *fiber.Ctx) error {
	ctx, cancel := helper.RequestContext(c)
	defer cancel()

	authUser, ok := helper.CurrentUser(c)
	if !ok {
		return helper.Unauthorized("belum terautentikasi")
	}

	user, err := s.users.FindByID(ctx, authUser.UserID)
	if err != nil {
		return helper.Unauthorized("user tidak ditemukan")
	}

	// Daftar permission hanya untuk kemudahan tampilan frontend
	// (menyembunyikan tombol). Pengamanan sesungguhnya tetap di
	// middleware dan service — bukan di sini.
	return helper.Success(c, fiber.StatusOK, "profil berhasil diambil", model.Profile{
		User:        user,
		Permissions: s.perms.PermissionsOf(user.Role),
	})
}

func (s *AuthService) issueTokenPair(ctx context.Context, user model.User) (model.TokenPair, error) {
	accessToken, err := s.jwt.GenerateAccess(user)
	if err != nil {
		return model.TokenPair{}, err
	}

	refreshToken, err := helper.RandomToken(refreshTokenBytes)
	if err != nil {
		return model.TokenPair{}, err
	}

	err = s.tokens.Save(ctx, model.RefreshToken{
		UserID:    user.ID,
		TokenHash: helper.SHA256Hex(refreshToken),
		ExpiresAt: time.Now().Add(s.refreshTTL),
	})
	if err != nil {
		return model.TokenPair{}, err
	}

	return model.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.jwt.AccessTTL().Seconds()),
	}, nil
}
