-- Migration 007: transactional outbox dan catatan notifikasi
--   psql -U postgres -d db_students -f migrations/007_outbox.sql
--
-- Padanan migration 008_outbox.sql pada latihan-fiber. Nomor 007 dipakai
-- karena proyek api-student baru memiliki migration sampai 006.
-- ---------------------------------------------------------------------
-- outbox — "kotak surat keluar" milik database
--
-- Setiap event penting dicatat di sini DI DALAM transaction yang sama
-- dengan perubahan datanya. Karena satu transaction, hanya ada dua
-- kemungkinan: perubahan data dan catatan event-nya sama-sama tersimpan,
-- atau sama-sama batal.
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS outbox (
    id           BIGSERIAL   PRIMARY KEY,
    event_type   VARCHAR(50) NOT NULL,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- NULL berarti belum di-publish ke queue.
    published_at TIMESTAMPTZ NULL
);

-- Relay hanya pernah mencari baris yang belum di-publish. Partial index
-- membuat index ini tetap kecil walaupun tabel outbox tumbuh besar.
CREATE INDEX IF NOT EXISTS outbox_unpublished_idx
    ON outbox (id) WHERE published_at IS NULL;

-- ---------------------------------------------------------------------
-- notifications — bukti bahwa sebuah event SUDAH dikirimkan
--
-- Pada tahap ini idempotency key-nya masih UNIQUE (outbox_id): satu event
-- menghasilkan tepat satu notifikasi. Migration 008 menggantinya ketika
-- satu event harus menghasilkan banyak notifikasi (perubahan kuota).
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notifications (
    id         BIGSERIAL    PRIMARY KEY,
    outbox_id  BIGINT       NOT NULL,
    user_id    INTEGER      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel    VARCHAR(20)  NOT NULL,
    subject    VARCHAR(200) NOT NULL,
    body       TEXT         NOT NULL,
    sent_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT notifications_outbox_unique UNIQUE (outbox_id)
);

CREATE INDEX IF NOT EXISTS notifications_user_idx
    ON notifications (user_id, outbox_id DESC);

-- ---------------------------------------------------------------------
-- Permission untuk endpoint monitoring queue.
-- ---------------------------------------------------------------------
INSERT INTO permissions (name, description) VALUES
    ('queue:monitor', 'Melihat keadaan outbox dan queue task')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_name, permission_name) VALUES
    ('admin', 'queue:monitor')
ON CONFLICT DO NOTHING;
