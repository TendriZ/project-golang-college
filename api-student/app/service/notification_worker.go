package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"api-student/app/model"
	"api-student/app/repository"
	"api-student/helper"
)

// NotificationWorker mengerjakan task "notification:send".
//
// Bagi worker, Asynq adalah framework-nya — sama seperti Fiber bagi API.
// Karena itu method di sini menerima *asynq.Task, seperti method service
// lain menerima *fiber.Ctx.
type NotificationWorker struct {
	notifications repository.NotificationRepository
	tx            *repository.TxManager
	mailer        helper.Mailer
	logger        *slog.Logger
}

func NewNotificationWorker(
	notifications repository.NotificationRepository,
	tx *repository.TxManager,
	mailer helper.Mailer,
	logger *slog.Logger,
) *NotificationWorker {
	return &NotificationWorker{notifications: notifications, tx: tx,
		mailer: mailer, logger: logger}
}

// HandleSend mengembalikan error untuk meminta Asynq melakukan retry nanti,
// atau error yang membungkus asynq.SkipRetry untuk menyatakan "jangan
// di-retry, task ini tidak mungkin berhasil".
//
// Satu task hanya berisi SATU penerima. Itulah yang membatasi kerusakan
// ketika pengiriman gagal: retry task milik penerima ke-7 tidak menyentuh
// email penerima ke-1 sampai ke-6.
//
// Asynq menjamin setiap task dijalankan PALING SEDIKIT satu kali, bukan
// TEPAT satu kali. Task yang sama bisa datang dua kali, misalnya bila
// worker mati setelah mengirim email tetapi sebelum sempat melapor ke
// Redis. Karena itu handler ini harus idempoten: menjalankannya dua kali
// untuk task yang sama harus berakibat sama dengan menjalankannya sekali.
func (w *NotificationWorker) HandleSend(ctx context.Context, t *asynq.Task) error {
	var job model.QuotaChangedJob
	if err := json.Unmarshal(t.Payload(), &job); err != nil {
		return fmt.Errorf("%w: payload task rusak: %v", asynq.SkipRetry, err)
	}
	msg, err := BuildMessage(job.EventType, job.Event, job.Recipient)
	if err != nil {
		return fmt.Errorf("%v: %w", asynq.SkipRetry, err)
	}

	alreadySent := false
	err = w.tx.WithTxOnce(ctx, func(tx repository.DBTX) error {
		// Claim dan pengiriman berada di dalam SATU transaction:
		//   - pengiriman gagal  -> claim ikut dibatalkan -> attempt
		//     berikutnya masih dapat mengirim;
		//   - pengiriman sukses -> claim di-commit -> attempt berikutnya
		//     melihat claim ini dan tidak mengirim lagi.
		// Claim HARUS memakai transaction yang sedang berjalan. Tanpa
		// WithTx, Claim membuka transaction sendiri dan langsung commit:
		// bila pengiriman email lalu gagal, claim tetap tersimpan dan
		// attempt berikutnya mengira email sudah terkirim.
		//
		// WithTxOnce, bukan WithTx: fn ini mengirim email — efek samping
		// di luar database. Retry otomatis TxManager akan mengulang
		// pengiriman itu juga. Pengulangan diserahkan kepada Asynq, yang
		// tahu task mana yang gagal.
		claimed, err := w.notifications.WithTx(tx).Claim(ctx, model.Notification{
			OutboxID: job.OutboxID, UserID: job.Recipient.UserID, Channel: "email",
			Subject: msg.Subject, Body: msg.Body,
		})
		if err != nil {
			return err
		}
		if !claimed {
			alreadySent = true
			return nil
		}
		return w.mailer.Send(ctx, msg)
	})
	if errors.Is(err, helper.ErrPermanentDelivery) {
		// SkipRetry harus dibungkus dengan %w agar errors.Is di Asynq
		// mendeteksinya: kegagalan permanen langsung masuk archived,
		// tidak dibuang-buang waktu untuk 6 kali percobaan.
		return fmt.Errorf("%v: %w", asynq.SkipRetry, err)
	}
	if err != nil {
		return err
	}

	retry, _ := asynq.GetRetryCount(ctx)
	w.logger.Info("notification_processed",
		slog.Int64("outbox_id", job.OutboxID),
		slog.Int("user_id", job.Recipient.UserID),
		slog.String("event", job.EventType),
		slog.Int("retry", retry),
		slog.Bool("already_sent", alreadySent))
	return nil
}
