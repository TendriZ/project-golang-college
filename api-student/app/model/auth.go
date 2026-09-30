package model

import "time"

type RegisterRequest struct {
	Username string `json:"username" validate:"required,min=3,max=30,username"`
	Email    string `json:"email" validate:"required,email,max=120"`
	Password string `json:"password" validate:"required,max=72,strongpassword"`
	// TIDAK ADA field Role — mencegah mass assignment ("role":"admin" dari client)
}

// Perhatikan max=72 pada password. Angka itu bukan pilihan bebas: bcrypt
// hanya memproses 72 byte pertama dan MENGABAIKAN sisanya tanpa peringatan.
// Tanpa batas ini, dua password berbeda yang sama pada 72 byte pertama
// akan dianggap identik saat login.

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// RefreshToken adalah row pada table refresh_tokens.
type RefreshToken struct {
	ID        int64
	UserID    int
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// AuthUser adalah identitas yang dibawa access token.
type AuthUser struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}
