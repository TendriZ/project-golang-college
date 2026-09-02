CREATE TABLE IF NOT EXISTS students (
    id SERIAL PRIMARY KEY,
    nim INT NOT NULL,
    name VARCHAR(100) NOT NULL,
    grade NUMERIC(5,2) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Menjaga agar NIM tidak ganda di level database
CREATE UNIQUE INDEX IF NOT EXISTS students_nim_key ON students (nim);

-- Mempercepat pencarian (ILIKE) pada nama
CREATE INDEX IF NOT EXISTS students_name_lower_idx ON students (LOWER(name));