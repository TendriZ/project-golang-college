package config

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"latihan-fiber/app/model"
)

// TestWorkerConfig_QueueName mengunci nama queue tempat worker mendengarkan.
//
// Ini regresi dari bug yang ditemukan saat menjalankan Docker: Queues diisi
// TaskSendNotification ("notification:send", jenis task) alih-alih
// QueueNotifications ("notifications", nama queue). Relay menaruh task di
// queue "notifications", worker menunggu di "notification:send", sehingga
// task menumpuk di Redis dan tabel notifications tidak pernah terisi.
func TestWorkerConfig_QueueName(t *testing.T) {
	cfg := workerConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, ok := cfg.Queues[model.QueueNotifications]; !ok {
		t.Errorf("worker tidak mendengarkan queue %q; Queues = %v",
			model.QueueNotifications, cfg.Queues)
	}

	if _, salah := cfg.Queues[model.TaskSendNotification]; salah {
		t.Errorf("worker mendengarkan %q (jenis task), bukan nama queue; Queues = %v",
			model.TaskSendNotification, cfg.Queues)
	}
}

// TestWorkerConfig_RetryDelay memastikan jadwal retry tetap pendek. Bila
// fungsi ini hilang, Asynq memakai bawaan yang jauh lebih lama sehingga
// praktikum terasa "macet" tanpa penjelasan.
func TestWorkerConfig_RetryDelay(t *testing.T) {
	cfg := workerConfig(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if cfg.RetryDelayFunc == nil {
		t.Fatal("RetryDelayFunc nil")
	}

	for n := 0; n < 3; n++ {
		want := time.Duration((n+1)*2) * time.Second
		if got := cfg.RetryDelayFunc(n, nil, nil); got != want {
			t.Errorf("RetryDelayFunc(%d) = %v, seharusnya %v", n, got, want)
		}
	}
}
