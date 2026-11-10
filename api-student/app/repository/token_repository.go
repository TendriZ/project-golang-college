package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"api-student/app/model"
)

type TokenRepository interface {
	WithTx(tx DBTX) TokenRepository

	Save(ctx context.Context, t model.RefreshToken) error
	FindActive(ctx context.Context, tokenHash string) (model.RefreshToken, error)
	Revoke(ctx context.Context, tokenHash string) error
	RevokeAllForUser(ctx context.Context, userID int) error
}

type tokenPostgresRepository struct {
	db DBTX
}

func NewTokenRepository(db DBTX) TokenRepository {
	return &tokenPostgresRepository{db: db}
}

func (r *tokenPostgresRepository) WithTx(tx DBTX) TokenRepository {
	return &tokenPostgresRepository{db: tx}
}

func (r *tokenPostgresRepository) Save(ctx context.Context, t model.RefreshToken) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)`,
		t.UserID, t.TokenHash, t.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("menyimpan refresh token: %w", err)
	}
	return nil
}

func (r *tokenPostgresRepository) FindActive(
	ctx context.Context, tokenHash string,
) (model.RefreshToken, error) {
	var t model.RefreshToken
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		 FROM refresh_tokens
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()`,
		tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.RefreshToken{}, ErrNotFound
		}
		return model.RefreshToken{}, fmt.Errorf("mengambil refresh token: %w", err)
	}
	return t, nil
}

func (r *tokenPostgresRepository) Revoke(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW()
		 WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash,
	)
	if err != nil {
		return fmt.Errorf("mencabut refresh token: %w", err)
	}
	return nil
}

func (r *tokenPostgresRepository) RevokeAllForUser(
	ctx context.Context, userID int,
) error {
	_, err := r.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW()
		 WHERE user_id = $1 AND revoked_at IS NULL`, userID,
	)
	if err != nil {
		return fmt.Errorf("mencabut seluruh refresh token user: %w", err)
	}
	return nil
}
