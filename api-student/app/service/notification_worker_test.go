package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/hibiken/asynq"

	"api-student/app/model"
)

func newTask(t *testing.T, eventType string, event model.QuotaChangedEvent) *asynq.Task {
	t.Helper()
	payload, err := json.Marshal(model.QuotaChangedJob{
		OutboxID:  1,
		EventType: eventType,
		Event:     event,
		Recipient: model.QuotaRecipient{
			UserID: 11, Username: "sari", Email: "sari@student.unair.ac.id",
		},
	})
	if err != nil {
		t.Fatalf("menyusun job: %v", err)
	}
	return asynq.NewTask(model.TaskSendNotification, payload)
}

// Seluruh skenario di bawah ini berhenti SEBELUM worker menyentuh
// transaction dan mailer. Karena itu notifications, tx, dan mailer boleh
// nil: test ini murni unit test, tanpa database dan tanpa Redis.
func TestHandleSend_PermanentFailures(t *testing.T) {
	worker := NewNotificationWorker(
		nil, nil, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	tests := []struct {
		name string
		task *asynq.Task
	}{
		{
			name: "payload task bukan JSON",
			task: asynq.NewTask(model.TaskSendNotification, []byte("bukan json")),
		},
		{
			name: "jenis event tidak dikenal",
			task: newTask(t, "supervisor.unknown", model.QuotaChangedEvent{SupervisorID: 3}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := worker.HandleSend(context.Background(), tt.task)
			// Asynq memeriksa SkipRetry dengan errors.Is, maka test juga
			// harus memeriksa dengan cara yang sama.
			if !errors.Is(err, asynq.SkipRetry) {
				t.Errorf("error = %v, seharusnya membungkus asynq.SkipRetry", err)
			}
		})
	}
}
