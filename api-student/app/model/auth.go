package model

import "time"

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	// TIDAK ADA field Role — mencegah mass assignment ("role":"admin" dari client)
}

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