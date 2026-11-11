package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"api-student/app/model"
	"api-student/app/repository"
)

// TaskEnqueuer adalah kemampuan melakukan enqueue task.
// *asynq.Client memenuhi interface ini.
type TaskEnqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// errUnprocessableEvent menandai event yang isinya tidak akan pernah bisa
// diproses (payload rusak atau jenis event tidak dikenal). Event seperti
// ini tidak boleh membuat relay berhenti selamanya: RelayOnce akan
// mencatatnya di log lalu menandainya published.
var errUnprocessableEvent = errors.New("event tidak dapat diproses")

// OutboxRelay memindahkan event dari tabel outbox ke queue Redis.
//
// Relay adalah satu-satunya komponen yang membaca outbox. API tidak pernah
// berbicara dengan Redis; ia cukup menulis ke PostgreSQL. Karena itu admin
// tetap dapat mengubah kuota walaupun Redis sedang mati — notifikasinya
// hanya tertunda, tidak hilang.
//
// Untuk event perubahan kuota, relay melakukan FAN-OUT: satu baris outbox
// dibaca menjadi satu task per penerima. Relay juga yang menanyakan daftar
// penerima, bukan API. Dengan begitu transaction PATCH /supervisors/:id
// tetap ringkas: ia hanya menulis satu event.
type OutboxRelay struct {
	outbox       repository.OutboxRepository
	supervisions repository.SupervisionRepository
	tx           *repository.TxManager
	queue        TaskEnqueuer
	logger       *slog.Logger
	interval     time.Duration
	batch        int
}

func NewOutboxRelay(
	outbox repository.OutboxRepository,
	supervisions repository.SupervisionRepository,
	tx *repository.TxManager,
	queue TaskEnqueuer,
	logger *slog.Logger,
	interval time.Duration,
	batch int,
) *OutboxRelay {
	return &OutboxRelay{outbox: outbox, supervisions: supervisions, tx: tx,
		queue: queue, logger: logger, interval: interval, batch: batch}
}

// Run berjalan terus sampai ctx dibatalkan.
func (r *OutboxRelay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		n, err := r.RelayOnce(ctx)
		if err != nil {
			r.logger.Warn("relay_delayed",
				slog.Int("published", n), slog.String("error", err.Error()))
		} else if n > 0 {
			r.logger.Info("relay_done", slog.Int("published", n))
		}
		// Satu batch penuh berarti kemungkinan besar masih ada sisa:
		// langsung lanjutkan tanpa menunggu. Selain itu, tunggu giliran
		// berikutnya.
		if err == nil && n == r.batch && ctx.Err() == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RelayOnce mem-publish paling banyak satu batch. Mengembalikan jumlah
// event yang berhasil di-publish.
func (r *OutboxRelay) RelayOnce(ctx context.Context) (int, error) {
	var published int
	var enqueueErr error
	err := r.tx.WithTxOnce(ctx, func(tx repository.DBTX) error {
		repo := r.outbox.WithTx(tx)
		events, err := repo.FetchUnpublished(ctx, r.batch)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(events))
		for _, e := range events {
			err := r.publish(ctx, tx, e)
			if errors.Is(err, errUnprocessableEvent) {
				// Event rusak tidak akan berubah menjadi baik dengan
				// dicoba berulang kali. Catat, tandai published, dan
				// lanjutkan — jangan biarkan satu baris rusak menyumbat
				// seluruh antrean di belakangnya.
				r.logger.Error("relay_event_dilewati",
					slog.Int64("outbox_id", e.ID),
					slog.String("event", e.EventType),
					slog.String("error", err.Error()))
				ids = append(ids, e.ID)
				continue
			}
			if err != nil {
				// Berhenti pada kegagalan pertama. Bila Redis mati, event
				// berikutnya pun pasti gagal; tidak ada gunanya dicoba satu
				// per satu. Yang belum di-publish tetap NULL dan diambil lagi
				// pada putaran berikutnya.
				enqueueErr = err
				break
			}
			// ID baru masuk daftar SETELAH seluruh task event ini berhasil
			// di-enqueue. Jika dicatat lebih dulu, event yang gagal enqueue
			// ikut ter-tandai published padahal task-nya tidak pernah masuk
			// queue — event-nya hilang untuk selamanya.
			ids = append(ids, e.ID)
		}
		// Hanya event yang SUDAH berhasil di-enqueue yang ditandai.
		if err := repo.MarkPublished(ctx, ids); err != nil {
			return err
		}
		published = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return published, enqueueErr
}

// publish menerjemahkan satu baris outbox menjadi task di Redis.
func (r *OutboxRelay) publish(
	ctx context.Context, tx repository.DBTX, e model.OutboxEvent,
) error {
	switch e.EventType {
	case model.EventQuotaChanged:
		return r.publishQuotaChanged(ctx, tx, e)
	default:
		return fmt.Errorf("%w: jenis event %q tidak dikenal",
			errUnprocessableEvent, e.EventType)
	}
}

// publishQuotaChanged melakukan fan-out satu event menjadi satu task per
// penerima.
//
// Satu task per mahasiswa dipilih, bukan satu task berisi seluruh daftar
// penerima, karena Asynq me-retry SATU TASK secara utuh. Bila 40 email
// dikirim dalam satu task dan email ke-7 gagal, retry akan mengulang task
// itu dari awal — email ke-1 sampai ke-6 terkirim lagi. Dengan satu task
// per penerima, hanya task milik penerima yang gagal yang diulang.
func (r *OutboxRelay) publishQuotaChanged(
	ctx context.Context, tx repository.DBTX, e model.OutboxEvent,
) error {
	var event model.QuotaChangedEvent
	if err := json.Unmarshal(e.Payload, &event); err != nil {
		return fmt.Errorf("%w: payload outbox %d rusak: %v",
			errUnprocessableEvent, e.ID, err)
	}

	recipients, err := r.supervisions.WithTx(tx).FindRecipients(ctx, event.SupervisorID)
	if err != nil {
		return err
	}

	for _, recipient := range recipients {
		job := model.QuotaChangedJob{
			OutboxID:  e.ID,
			EventType: e.EventType,
			Event:     event,
			Recipient: recipient,
		}
		payload, err := json.Marshal(job)
		if err != nil {
			return fmt.Errorf("menyusun payload task outbox %d untuk user %d: %w",
				e.ID, recipient.UserID, err)
		}
		task := asynq.NewTask(model.TaskSendNotification, payload,
			// TaskID diturunkan dari pasangan (outbox, penerima). Inilah
			// yang membuat enqueue ulang aman:
			//   - relay mati setelah enqueue tetapi sebelum commit ->
			//     putaran berikutnya meng-enqueue task yang sama, Redis
			//     menolaknya dengan ErrTaskIDConflict (dianggap berhasil);
			//   - penerima yang berbeda tetap mendapat TaskID berbeda,
			//     sehingga tidak saling menolak.
			asynq.TaskID(fmt.Sprintf("outbox-%d-u%d", e.ID, recipient.UserID)),
			asynq.Queue(model.QueueNotifications),
			asynq.MaxRetry(5),
			asynq.Timeout(30*time.Second),
			// Task yang selesai disimpan 24 jam. Selama itu, TaskID-nya
			// tetap tercatat sehingga perlindungan di atas tetap berlaku.
			asynq.Retention(24*time.Hour),
		)
		_, err = r.queue.EnqueueContext(ctx, task)
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			// Sudah pernah di-enqueue pada putaran sebelumnya. Bagi relay,
			// itu sama dengan berhasil.
			continue
		}
		if err != nil {
			return fmt.Errorf("enqueue outbox %d untuk user %d: %w",
				e.ID, recipient.UserID, err)
		}
	}
	return nil
}
