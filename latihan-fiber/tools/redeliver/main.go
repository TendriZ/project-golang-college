// Alat uji redeliver: membuat N salinan task notifikasi untuk satu baris
// outbox, langsung ke Redis — melewati relay. Dipakai pada uji idempotency
// (Bagian C.5) untuk membuktikan bahwa berapa pun jumlah salinan task,
// hanya satu email yang keluar.
//
//	go run ./tools/redeliver -outbox <id> -copies 5
//
// Setiap salinan memakai TaskID unik (redeliver-<id>-<n>) supaya tidak
// ditolak asynq.ErrTaskIDConflict. Relay tetap menambahkan satu task lagi
// dengan TaskID outbox-<id> begitu worker menyala — itulah mengapa lima
// salinan di sini bisa berubah menjadi enam task di queue.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hibiken/asynq"

	"latihan-fiber/app/model"
	"latihan-fiber/config"
	"latihan-fiber/database"
)

func main() {
	outboxID := flag.Int64("outbox", 0, "id baris outbox yang akan dikirim ulang")
	copies := flag.Int("copies", 1, "jumlah salinan task")
	flag.Parse()

	if *outboxID <= 0 || *copies <= 0 {
		fmt.Fprintln(os.Stderr, "pakai: go run ./tools/redeliver -outbox <id> -copies <n>")
		os.Exit(2)
	}

	config.LoadEnv()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx)
	if err != nil {
		fail("gagal terhubung ke database: %v", err)
	}
	defer pool.Close()

	// Isi task diambil dari baris outbox: id, jenis event, dan payload-nya
	// harus sama persis dengan yang akan dibuat relay.
	var eventType string
	var payload []byte
	err = pool.QueryRow(ctx,
		`SELECT event_type, payload FROM outbox WHERE id = $1`, *outboxID,
	).Scan(&eventType, &payload)
	if err != nil {
		fail("outbox %d tidak dapat dibaca: %v", *outboxID, err)
	}

	job, err := json.Marshal(model.NotificationJob{
		OutboxID:  *outboxID,
		EventType: eventType,
		Payload:   payload,
	})
	if err != nil {
		fail("gagal menyusun payload task: %v", err)
	}

	client := asynq.NewClient(config.RedisOpt())
	defer client.Close()

	for i := 1; i <= *copies; i++ {
		task := asynq.NewTask(model.TaskSendNotification, job,
			asynq.TaskID(fmt.Sprintf("redeliver-%d-%d", *outboxID, i)),
			asynq.Queue(model.QueueNotifications),
			asynq.MaxRetry(5),
			asynq.Timeout(30*time.Second),
			asynq.Retention(24*time.Hour),
		)
		info, err := client.EnqueueContext(ctx, task)
		if err != nil {
			fail("salinan %d gagal di-enqueue: %v", i, err)
		}
		fmt.Printf("salinan %d -> task_id=%s queue=%s\n", i, info.ID, info.Queue)
	}
	fmt.Printf("%d salinan task untuk outbox %d sudah di Redis\n", *copies, *outboxID)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
