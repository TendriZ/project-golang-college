-- Urutan column pada index HARUS sama persis dengan ORDER BY pada
-- query FindAfterCursor (created_at DESC, id DESC). Bila tidak,
-- PostgreSQL tetap dapat memakai index untuk sebagian kasus, tetapi
-- tidak dapat memakainya untuk melompat langsung ke posisi cursor —
-- dan seluruh keuntungan keyset pagination hilang tanpa satu pun
-- pesan kesalahan.
CREATE INDEX IF NOT EXISTS students_created_at_id_desc_idx
 ON students (created_at DESC, id DESC);
