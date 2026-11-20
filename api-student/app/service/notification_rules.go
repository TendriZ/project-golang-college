package service

import (
	"errors"
	"fmt"

	"api-student/app/model"
)

var errUnknownEvent = errors.New("jenis event tidak dikenal")

// BuildMessage menyusun pesan dari sebuah event dan penerimanya. Fungsi
// murni: tidak menyentuh database, Redis, maupun email, sehingga mudah diuji.
//
// Subjek mengikuti pola notifikasi Modul 11, tetapi memakai istilah domain
// api-student: bimbingan, bukan KRS.
func BuildMessage(
	eventType string, e model.QuotaChangedEvent, recipient model.QuotaRecipient,
) (model.Message, error) {
	switch eventType {
	case model.EventQuotaChanged:
		subject := fmt.Sprintf("Bimbingan: kuota pembimbing %s berubah menjadi %d",
			e.SupervisorName, e.Quota)
		body := fmt.Sprintf(
			"Halo %s, kuota bimbingan %s sekarang %d. Perubahan ini berlaku untuk seluruh mahasiswa binaan.",
			recipient.Username, e.SupervisorName, e.Quota)
		return model.Message{To: recipient.Email, Subject: subject, Body: body}, nil
	default:
		return model.Message{}, fmt.Errorf("%w: %q", errUnknownEvent, eventType)
	}
}
