-- Migration 005: pembimbing tugas akhir untuk entity students
-- psql -U postgres -d db_students -f migrations/005_supervisors.sql
--
-- supervisors  : dosen pembimbing beserta kuota bimbingannya
-- supervisions : penetapan seorang mahasiswa ke seorang pembimbing
--
-- Aturan "satu mahasiswa hanya boleh punya SATU pembimbing" dijamin
-- database lewat UNIQUE (student_id). Aplikasi boleh melakukan pemeriksaan
-- lebih dulu agar pesannya ramah, tetapi constraint inilah jaring pengaman
-- terakhir bila dua request datang bersamaan.

CREATE TABLE IF NOT EXISTS supervisors (
    id               SERIAL       PRIMARY KEY,
    name             VARCHAR(120) NOT NULL,
    quota            INTEGER      NOT NULL CHECK (quota > 0),
    supervised_count INTEGER      NOT NULL DEFAULT 0,
    version          INTEGER      NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- Jaring pengaman yang sama seperti courses_enrolled_within_quota.
    -- Bila kode aplikasi benar, constraint ini tidak pernah terpicu.
    CONSTRAINT supervisors_supervised_within_quota
        CHECK (supervised_count >= 0 AND supervised_count <= quota)
);

CREATE TABLE IF NOT EXISTS supervisions (
    id            BIGSERIAL   PRIMARY KEY,
    student_id    INTEGER     NOT NULL REFERENCES students(id)
                              ON DELETE CASCADE,
    supervisor_id INTEGER     NOT NULL REFERENCES supervisors(id)
                              ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Inilah yang menjamin satu mahasiswa hanya punya satu pembimbing.
    CONSTRAINT supervisions_student_unique UNIQUE (student_id)
);

CREATE INDEX IF NOT EXISTS supervisions_supervisor_idx
    ON supervisions (supervisor_id);

-- ---------------------------------------------------------------
-- Permission baru untuk mengelola penetapan pembimbing.
-- Dibaca sekali saat startup oleh RoleRepository.LoadPermissions,
-- sehingga server perlu dijalankan ulang setelah migration ini.
-- ---------------------------------------------------------------
INSERT INTO permissions (name, description) VALUES
    ('supervision:manage',
     'Menetapkan dan mengganti pembimbing tugas akhir mahasiswa')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_name, permission_name) VALUES
    ('admin', 'supervision:manage')
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------
-- Data awal untuk demonstrasi dan uji beban.
-- Hanya disisipkan bila tabel masih kosong, agar migration idempoten.
-- ---------------------------------------------------------------
INSERT INTO supervisors (name, quota)
SELECT v.name, v.quota
FROM (VALUES
    ('Dr. Ani Rahmawati', 3),
    ('Prof. Budi Santoso', 5),
    ('Dr. Citra Lestari', 5)
) AS v(name, quota)
WHERE NOT EXISTS (SELECT 1 FROM supervisors);
