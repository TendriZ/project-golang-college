package service

import (
	"api-student/app/model"
	"api-student/helper"
)

// CanAccessStudent memutuskan apakah seseorang boleh menyentuh data student tertentu.
//
// Dua jalur yang diizinkan:
// 1. Kepemilikan (ownership) — owner_id sama dengan id pemanggil.
// 2. Permission — role-nya memang berhak atas data siapa pun (permission :any).
//
// Urutannya disengaja: pemeriksaan kepemilikan didahulukan karena paling
// murah dan paling sering benar untuk role user. Bila keduanya gagal,
// jawabannya false (fail closed).
//
// Fungsi ini murni: tidak mengimpor fiber maupun repository, sehingga
// dapat diuji tanpa server dan tanpa database.
func CanAccessStudent(
	current model.AuthUser,
	ownerID int,
	perms *helper.PermissionSet,
	anyPermission string,
) bool {
	if current.UserID == ownerID {
		return true
	}
	return perms.Can(current.Role, anyPermission)
}
