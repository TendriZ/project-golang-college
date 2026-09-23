-- ---------------------------------------------------------------
-- roles — daftar role yang diakui sistem
-- ---------------------------------------------------------------
CREATE TABLE IF NOT EXISTS roles (
    name        VARCHAR(20) PRIMARY KEY,
    description VARCHAR(150) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO roles (name, description) VALUES
    ('admin', 'Akses penuh terhadap seluruh data mahasiswa'),
    ('staff', 'Boleh melihat dan menambah data mahasiswa, tidak boleh mengubah/menghapus milik orang lain'),
    ('user',  'Hanya boleh mengelola data mahasiswa miliknya sendiri')
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------
-- permissions
-- ---------------------------------------------------------------
CREATE TABLE IF NOT EXISTS permissions (
    name        VARCHAR(50) PRIMARY KEY,
    description VARCHAR(150) NOT NULL
);

INSERT INTO permissions (name, description) VALUES
    ('student:list',       'Melihat daftar seluruh mahasiswa'),
    ('student:read:any',   'Melihat data mahasiswa mana pun'),
    ('student:create',     'Menambahkan data mahasiswa baru'),
    ('student:update:any', 'Mengubah data mahasiswa mana pun'),
    ('student:delete',     'Menghapus data mahasiswa')
ON CONFLICT (name) DO NOTHING;

-- ---------------------------------------------------------------
-- role_permissions
-- ---------------------------------------------------------------
CREATE TABLE IF NOT EXISTS role_permissions (
    role_name       VARCHAR(20) NOT NULL
        REFERENCES roles(name) ON DELETE CASCADE,
    permission_name VARCHAR(50) NOT NULL
        REFERENCES permissions(name) ON DELETE CASCADE,
    PRIMARY KEY (role_name, permission_name)
);

INSERT INTO role_permissions (role_name, permission_name) VALUES
    ('admin', 'student:list'),
    ('admin', 'student:read:any'),
    ('admin', 'student:create'),
    ('admin', 'student:update:any'),
    ('admin', 'student:delete'),
    ('staff', 'student:list'),
    ('staff', 'student:read:any'),
    ('staff', 'student:create')
ON CONFLICT DO NOTHING;
-- role 'user' sengaja tidak diberi permission apa pun —
-- haknya atas data mahasiswa mengalir dari kepemilikan (owner_id), bukan permission.

-- ---------------------------------------------------------------
-- Kunci column users.role agar hanya berisi role yang dikenal.
-- ---------------------------------------------------------------
UPDATE users SET role = 'user' WHERE role NOT IN (SELECT name FROM roles);

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_fkey;
ALTER TABLE users
    ADD CONSTRAINT users_role_fkey
    FOREIGN KEY (role) REFERENCES roles(name) ON UPDATE CASCADE;

CREATE INDEX IF NOT EXISTS users_role_idx ON users (role);

-- ---------------------------------------------------------------
-- owner_id pada students — menandai siapa yang mendaftarkan data ini.
-- ---------------------------------------------------------------
ALTER TABLE students
    ADD COLUMN IF NOT EXISTS owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS students_owner_id_idx ON students (owner_id);