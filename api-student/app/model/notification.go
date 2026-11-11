package model

import (
	"encoding/json"
	"time"
)

// Nama-nama event yang dicatat ke outbox. Ditulis sebagai konstanta,
// bukan string yang diketik ulang di banyak tempat, karena salah ketik
// pada nama event tidak ditangkap compiler maupun database.
//
// EventQuotaChanged adalah padanan course.quota_changed pada Modul 11;
// domain api-student memakai pembimbing (supervisor), bukan kelas.
const (
	EventQuotaChanged = "supervisor.quota_changed"
)

// TaskSendNotification adalah nama JENIS task (task type); worker memakainya
// untuk memilih handler. QueueNotifications adalah nama QUEUE tempat task
// itu menunggu; worker hanya mengambil task dari queue yang ia dengarkan.
const (
	TaskSendNotification = "notification:send"
	QueueNotifications   = "notifications"
)

// QuotaChangedEvent adalah isi sebuah event perubahan kuota.
//
// Nama pembimbing DISALIN ke dalam event, bukan hanya id-nya. Event adalah
// catatan tentang apa yang terjadi PADA SAAT ITU: bila nama pembimbing
// diubah esok hari, notifikasi untuk event hari ini tetap memakai nama yang
// dilihat mahasiswa ketika perubahan itu terjadi.
type QuotaChangedEvent struct {
	SupervisorID   int    `json:"supervisor_id"`
	SupervisorName string `json:"supervisor_name"`
	Quota          int    `json:"quota"`
}

// QuotaRecipient adalah satu penerima notifikasi perubahan kuota:
// mahasiswa binaan pembimbing tersebut, dilihat lewat akun pemilik datanya.
//
// Username dan Email ikut DISALIN saat relay membaca daftar peserta.
// Alasannya sama seperti nama pembimbing di atas: task tidak boleh berubah
// arti bila data mahasiswa berubah setelah event tercatat.
type QuotaRecipient struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// QuotaChangedJob adalah payload task per penerima. Satu task hanya
// mengerjakan SATU email, sehingga retry task ke-7 tidak mengirim ulang
// email ke-1 sampai ke-6.
//
// OutboxID dan EventType ikut dibawa karena keduanya bagian dari
// idempotency key di tabel notifications: (outbox_id, user_id).
type QuotaChangedJob struct {
	OutboxID  int64             `json:"outbox_id"`
	EventType string            `json:"event_type"`
	Event     QuotaChangedEvent `json:"event"`
	Recipient QuotaRecipient    `json:"recipient"`
}

// OutboxEvent adalah satu baris tabel outbox.
type OutboxEvent struct {
	ID        int64
	EventType string
	Payload   []byte
	CreatedAt time.Time
}

// NotificationJob adalah isi task yang dikirim ke queue.
//
// OutboxID ikut dibawa karena dialah bagian event dari idempotency key:
// worker memakainya untuk mengetahui apakah event ini sudah pernah
// dikirimkan ke penerima tersebut.
//
// Payload dibiarkan sebagai JSON mentah (json.RawMessage). Relay cukup
// meneruskannya apa adanya tanpa perlu tahu bentuk setiap jenis event;
// yang membaca isinya adalah worker, berdasarkan EventType.
type NotificationJob struct {
	OutboxID  int64           `json:"outbox_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

// Message adalah pesan yang siap dikirim, tidak bergantung pada cara
// pengirimannya (email, push notification, dan seterusnya).
type Message struct {
	To      string
	Subject string
	Body    string
}

// Notification adalah notifikasi yang sudah terkirim, seperti yang dilihat
// pengguna pada GET /notifications/mine.
type Notification struct {
	ID       int64     `json:"id"`
	OutboxID int64     `json:"outbox_id"`
	UserID   int       `json:"user_id"`
	Channel  string    `json:"channel"`
	Subject  string    `json:"subject"`
	Body     string    `json:"body"`
	SentAt   time.Time `json:"sent_at"`
}

// QueueStats adalah ringkasan keadaan untuk admin.
type QueueStats struct {
	OutboxPending     int        `json:"outbox_pending"`
	OutboxPublished   int        `json:"outbox_published"`
	OldestPendingAge  string     `json:"oldest_pending_age"`
	NotificationsSent int        `json:"notifications_sent"`
	Queue             *QueueInfo `json:"queue"`
	QueueError        string     `json:"queue_error,omitempty"`
}

// QueueInfo adalah salinan sebagian informasi queue dari Asynq. Ditulis
// ulang di sini agar package model tetap tidak bergantung pada library apa pun.
type QueueInfo struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`
	Active    int    `json:"active"`
	Scheduled int    `json:"scheduled"`
	Retry     int    `json:"retry"`
	Archived  int    `json:"archived"`
	Completed int    `json:"completed"`
	Processed int    `json:"processed_today"`
	Failed    int    `json:"failed_today"`
}
