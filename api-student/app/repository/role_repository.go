package repository

import (
	"context"
	"fmt"
)

// RoleRepository membaca pemetaan role ke permission dari database.
type RoleRepository interface {
	WithTx(tx DBTX) RoleRepository
	LoadPermissions(ctx context.Context) (map[string][]string, error)
}

type rolePostgresRepository struct{ db DBTX }

func NewRoleRepository(db DBTX) RoleRepository {
	return &rolePostgresRepository{db: db}
}

func (r *rolePostgresRepository) WithTx(tx DBTX) RoleRepository {
	return &rolePostgresRepository{db: tx}
}

// LoadPermissions mengambil SELURUH pasangan role dan permission sekaligus.
//
// LEFT JOIN dipakai dengan sengaja: role yang belum punya permission apa pun
// tetap harus muncul, supaya sistem tahu role tersebut memang dikenal
// meskipun daftar permission-nya kosong.
func (r *rolePostgresRepository) LoadPermissions(
	ctx context.Context,
) (map[string][]string, error) {
	rows, err := r.db.Query(ctx,
		`SELECT r.name, COALESCE(rp.permission_name, '')
		 FROM roles r
		 LEFT JOIN role_permissions rp ON rp.role_name = r.name
		 ORDER BY r.name, rp.permission_name`)
	if err != nil {
		return nil, fmt.Errorf("mengambil permission: %w", err)
	}
	defer rows.Close()
	result := map[string][]string{}
	for rows.Next() {
		var role, permission string
		if err := rows.Scan(&role, &permission); err != nil {
			return nil, fmt.Errorf("membaca row permission: %w", err)
		}
		if _, ok := result[role]; !ok {
			result[role] = []string{}
		}
		if permission != "" {
			result[role] = append(result[role], permission)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("membaca hasil query permission: %w", err)
	}
	return result, nil
}
