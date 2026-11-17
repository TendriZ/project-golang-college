package service

import (
	"errors"
	"strings"
	"testing"

	"api-student/app/model"
)

func TestBuildMessage(t *testing.T) {
	event := model.QuotaChangedEvent{
		SupervisorID: 3, SupervisorName: "Dr. Ani Rahmawati", Quota: 7,
	}
	recipient := model.QuotaRecipient{
		UserID: 11, Username: "sari", Email: "sari@student.unair.ac.id",
	}

	t.Run("event kuota berubah", func(t *testing.T) {
		msg, err := BuildMessage(model.EventQuotaChanged, event, recipient)
		if err != nil {
			t.Fatalf("error tidak diharapkan: %v", err)
		}
		wantSubject := "KRS: kuota pembimbing Dr. Ani Rahmawati berubah menjadi 7"
		if msg.Subject != wantSubject {
			t.Errorf("subject = %q, seharusnya %q", msg.Subject, wantSubject)
		}
		if msg.To != recipient.Email {
			t.Errorf("to = %q, seharusnya %q", msg.To, recipient.Email)
		}
		if !strings.Contains(msg.Body, recipient.Username) {
			t.Errorf("body tidak menyebut username: %q", msg.Body)
		}
		if !strings.Contains(msg.Body, event.SupervisorName) {
			t.Errorf("body tidak menyebut nama pembimbing: %q", msg.Body)
		}
	})

	t.Run("event tidak dikenal", func(t *testing.T) {
		_, err := BuildMessage("supervisor.unknown", event, recipient)
		if !errors.Is(err, errUnknownEvent) {
			t.Errorf("error = %v, seharusnya membungkus errUnknownEvent", err)
		}
	})
}
