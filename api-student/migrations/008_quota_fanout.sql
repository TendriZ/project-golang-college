-- Migration 008: idempotency key per penerima untuk event berfan-out
--   psql -U postgres -d db_students -f migrations/008_quota_fanout.sql
--
-- Padanan migration 009 pada Modul 11 (latihan-fiber). Nomor 008 dipakai
-- karena proyek api-student baru memiliki migration sampai 007.
--
-- Latar belakang:
-- Sebelum ini, UNIQUE (outbox_id) berarti satu baris outbox hanya boleh
-- menghasilkan SATU notifikasi. Itu benar untuk event KRS yang selalu
-- milik satu mahasiswa. Perubahan kuota pembimbing berbeda: SATU event
-- harus menghasilkan BANYAK notifikasi — satu untuk setiap mahasiswa
-- binaan pembimbing tersebut. Bila constraint lama dibiarkan, notifikasi
-- kedua dan seterusnya akan ditolak ON CONFLICT dan tidak pernah terkirim.
--
-- Pilihan pengganti: UNIQUE (outbox_id, user_id).
--   - outbox_id tetap menjadi identitas EVENT (satu perubahan kuota);
--   - user_id menjadi identitas PENERIMA di dalam event itu.
-- Dengan begitu idempotency tetap terjaga per penerima: task yang sama
-- datang dua kali untuk user yang sama tidak mengirim email kedua,
-- sedangkan penerima yang berbeda tetap masing-masing mendapat satu email.
--
-- Kolom channel sengaja TIDAK ikut masuk key. Notifikasi email adalah
-- satu-satunya channel saat ini; bila kelak ada channel lain, key dapat
-- diperluas menjadi (outbox_id, user_id, channel) lewat migration baru.
ALTER TABLE notifications
    DROP CONSTRAINT IF EXISTS notifications_outbox_unique;

ALTER TABLE notifications
    ADD CONSTRAINT notifications_outbox_user_unique
    UNIQUE (outbox_id, user_id);
