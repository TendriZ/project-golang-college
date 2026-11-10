-- Migration 006: permission portofolio
--   psql -U postgres -d db_students -f migrations/006_portfolio_permissions.sql
--
-- Portofolio disimpan di MongoDB, tetapi hak aksesnya tetap dikelola di
-- PostgreSQL bersama permission lain. Satu sistem hak akses untuk seluruh
-- aplikasi, apa pun database tempat datanya disimpan.

INSERT INTO permissions (name, description) VALUES
    ('portfolio:read:any',   'Melihat portofolio mahasiswa mana pun'),
    ('portfolio:update:any', 'Mengganti portofolio mahasiswa mana pun'),
    ('portfolio:manage:any', 'Menambah dan menghapus proyek di portofolio mana pun')
ON CONFLICT (name) DO NOTHING;

-- Admin dan staff boleh mengelola portofolio siapa pun.
INSERT INTO role_permissions (role_name, permission_name) VALUES
    ('admin', 'portfolio:read:any'),
    ('admin', 'portfolio:update:any'),
    ('admin', 'portfolio:manage:any'),
    ('staff', 'portfolio:read:any'),
    ('staff', 'portfolio:manage:any')
ON CONFLICT DO NOTHING;

-- Role 'user' sengaja tidak diberi permission :any —
-- haknya ke portofolio mengalir dari kepemilikan (student_id = id pengguna).
