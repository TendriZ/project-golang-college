// Alat uji redeliver: membuat N salinan task notifikasi untuk satu baris
// outbox perubahan kuota, langsung ke Redis — melewati relay.
//
//	go run ./tools/redeliver -outbox <id> -copies 5
//	go run ./tools/redeliver -outbox <id> -copies 3 -user 7
//
// Bila -user tidak diisi, salinan dibuat untuk SETIAP penerima event itu.
// Setiap salinan memakai TaskID unik (redeliver-<outbox>-u<user>-<n>)
// supaya tidak ditolak asynq.ErrTaskIDConflict. Relay tetap menambahkan
// satu task per penerima dengan TaskID outbox-<id>-u<user>.
//
// Gunanya: membuktikan bahwa berapa pun jumlah salinan task untuk
// pasangan (outbox, penerima) yang sama, hanya satu email yang keluar.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hibiken/asynq"

	"api-student/app/model"
	"api-student/app/repository"
	"api-student/config"
	"api-student/database"
)

func main() {
	outboxID := flag.Int64("outbox", 0, "id baris outbox yang akan dikirim ulang")
	copies := flag.Int("copies", 1, "jumlah salinan task PER PENERIMA")
	userID := flag.Int("user", 0, "batasi ke satu user_id (0 = semua penerima)")
	flag.Parse()

	if *outboxID <= 0 || *copies <= 0 {
		fmt.Fprintln(os.Stderr, "pakai: go run ./tools/redeliver -outbox <id> -copies <n> [-user <id>]")
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

	// Isi event diambil dari baris outbox: id, jenis event, dan payload-nya
	// harus sama persis dengan yang akan dibuat relay.
	var eventType string
	var payload []byte
	err = pool.QueryRow(ctx,
		`SELECT event_type, payload FROM outbox WHERE id = $1`, *outboxID,
	).Scan(&eventType, &payload)
	if err != nil {
		fail("outbox %d tidak dapat dibaca: %v", *outboxID, err)
	}
	if eventType != model.EventQuotaChanged {
		fail("outbox %d bukan %s (event_type=%q)",
			*outboxID, model.EventQuotaChanged, eventType)
	}
	var event model.QuotaChangedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		fail("payload outbox %d rusak: %v", *outboxID, err)
	}

	supervisions := repository.NewSupervisionRepository(pool)
	recipients, err := supervisions.FindRecipients(ctx, event.SupervisorID)
	if err != nil {
		fail("daftar penerima tidak dapat dibaca: %v", err)
	}

	selected := 0
	client := asynq.NewClient(config.RedisOpt())
	defer client.Close()

	for _, recipient := range recipients {
		if *userID != 0 && recipient.UserID != *userID {
			continue
		}
		selected++

		job, err := json.Marshal(model.QuotaChangedJob{
			OutboxID:  *outboxID,
			EventType: eventType,
			Event:     event,
			Recipient: recipient,
		})
		if err != nil {
			fail("gagal menyusun payload task untuk user %d: %v", recipient.UserID, err)
		}
		for i := 1; i <= *copies; i++ {
			task := asynq.NewTask(model.TaskSendNotification, job,
				asynq.TaskID(fmt.Sprintf("redeliver-%d-u%d-%d",
					*outboxID, recipient.UserID, i)),
				asynq.Queue(model.QueueNotifications),
				asynq.MaxRetry(5),
				asynq.Timeout(30*time.Second),
				asynq.Retention(24*time.Hour),
			)
			info, err := client.EnqueueContext(ctx, task)
			if err != nil {
				fail("salinan %d untuk user %d gagal di-enqueue: %v",
					i, recipient.UserID, err)
			}
			fmt.Printf("user %d salinan %d -> task_id=%s queue=%s\n",
				recipient.UserID, i, info.ID, info.Queue)
		}
	}

	if selected == 0 {
		if *userID != 0 {
			fail("user %d bukan penerima event outbox %d", *userID, *outboxID)
		}
		fail("event outbox %d tidak memiliki penerima", *outboxID)
	}
	fmt.Printf("%d penerima x %d salinan task untuk outbox %d sudah di Redis\n",
		selected, *copies, *outboxID)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
