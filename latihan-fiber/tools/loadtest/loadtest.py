#!/usr/bin/env python3
"""Alat uji beban Modul 9 versi Python.

Dipakai bila `go run` diblokir Application Control / Device Guard.
Perilakunya sama dengan tools/loadtest/main.go:

    python tools/loadtest/loadtest.py -mode crowd
    python tools/loadtest/loadtest.py -mode credits
    python tools/loadtest/loadtest.py -mode swap

Server harus sedang berjalan. Jalankan reset.sql sebelum setiap uji beban.
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from http import HTTPStatus
from pathlib import Path

import jwt  # PyJWT

MAX_CREDITS = 24
TOKEN_TTL_SECONDS = 3600

# Jangan memakai proxy sistem untuk request ke localhost.
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def find_env_file() -> Path:
    candidates = [
        Path(".env"),
        Path(__file__).resolve().parents[2] / ".env",
    ]
    for path in candidates:
        if path.exists():
            return path
    return Path(".env")


def load_env() -> dict:
    env = {}
    path = find_env_file()
    if not path.exists():
        return env

    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        env[key.strip()] = value.strip().strip('"').strip("'")
    return env


def psql(sql: str, env: dict) -> list[str]:
    psql_bin = shutil.which("psql")
    if psql_bin is None:
        raise RuntimeError("psql tidak ditemukan di PATH")

    child_env = os.environ.copy()
    child_env["PGPASSWORD"] = env.get("DB_PASSWORD", "")

    command = [
        psql_bin,
        "-h", env.get("DB_HOST", "localhost"),
        "-p", env.get("DB_PORT", "5432"),
        "-U", env.get("DB_USER", "postgres"),
        "-d", env.get("DB_NAME", "praktikum_backend"),
        "-v", "ON_ERROR_STOP=1",
        "-tA",
        "-F", "|",
        "-c", sql,
    ]

    result = subprocess.run(
        command,
        capture_output=True,
        text=True,
        env=child_env,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(
            "psql gagal: " + (result.stderr.strip() or result.stdout.strip())
        )

    return [line for line in result.stdout.splitlines() if line != ""]


def make_token(user: dict, secret: str, issuer: str) -> str:
    now = int(time.time())
    payload = {
        "username": user["username"],
        "role": user["role"],
        "iss": issuer,
        "sub": str(user["id"]),
        "iat": now,
        "exp": now + TOKEN_TTL_SECONDS,
    }
    return jwt.encode(payload, secret, algorithm="HS256")


def load_users(limit: int, env: dict, secret: str, issuer: str) -> list[dict]:
    rows = psql(
        "SELECT id, username, role FROM users "
        "WHERE username LIKE 'mhs%' ORDER BY username "
        f"LIMIT {limit}",
        env,
    )

    users = []
    for row in rows:
        user_id, username, role = row.split("|")
        user = {"id": int(user_id), "username": username, "role": role}
        user["token"] = make_token(user, secret, issuer)
        users.append(user)

    if len(users) < limit:
        raise RuntimeError(
            f"akun uji kurang: butuh {limit}, tersedia {len(users)} "
            "(jalankan migration 006)"
        )
    return users


def course_by_code(code: str, env: dict) -> int:
    rows = psql(f"SELECT id FROM courses WHERE code = '{code}'", env)
    if not rows:
        raise RuntimeError(f"kelas {code} tidak ditemukan")
    return int(rows[0])


def http_json(method: str, url: str, token: str, body=None):
    headers = {
        "Authorization": "Bearer " + token,
        "Accept": "application/json",
        # Selalu dikirim, termasuk untuk POST tanpa body: route /courses
        # dan /enrollments memakai middleware.RequireJSON.
        "Content-Type": "application/json",
    }
    data = None
    if body is not None:
        data = json.dumps(body).encode("utf-8")

    request = urllib.request.Request(url, data=data, headers=headers, method=method)

    try:
        with OPENER.open(request, timeout=15) as response:
            status = response.status
            raw = response.read()
    except urllib.error.HTTPError as exc:
        status = exc.code
        raw = exc.read()
    except Exception as exc:  # noqa: BLE001 - alat uji, tampilkan apa adanya
        return 0, "", str(exc)

    code = ""
    try:
        payload = json.loads(raw)
        if isinstance(payload, dict):
            code = payload.get("code", "") or ""
    except Exception:  # noqa: BLE001 - body bukan JSON, abaikan
        pass

    return status, code, ""


def result_label(result) -> str:
    status, code, _ = result
    if status == 0:
        return "ERROR"
    if status < 400:
        return f"{status} OK"
    if code:
        return f"{status} {code}"
    try:
        phrase = HTTPStatus(status).phrase
    except ValueError:
        phrase = ""
    return f"{status} {phrase}".strip()


def fire(count: int, job) -> dict:
    """Jalankan count pekerjaan serentak setelah semuanya siap."""
    barrier = threading.Barrier(count)
    results = []
    lock = threading.Lock()

    def runner(index: int) -> None:
        barrier.wait()
        try:
            result = job(index)
        except Exception as exc:  # noqa: BLE001
            result = (0, "", str(exc))
        with lock:
            results.append(result)

    threads = [
        threading.Thread(target=runner, args=(index,)) for index in range(count)
    ]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    tally = {}
    for result in results:
        label = result_label(result)
        tally[label] = tally.get(label, 0) + 1
    return tally


def print_tally(tally: dict) -> None:
    for label in sorted(tally, key=lambda key: (-tally[key], key)):
        print(f"  {label:<32} {tally[label]}")


def print_consistency(codes: list[str], env: dict) -> bool:
    quoted = ", ".join("'" + code + "'" for code in codes)
    rows = psql(
        "SELECT c.code, c.quota, c.enrolled_count, COUNT(e.id) "
        "FROM courses c "
        "LEFT JOIN enrollments e ON e.course_id = c.id "
        f"WHERE c.code IN ({quoted}) "
        "GROUP BY c.id, c.code, c.quota, c.enrolled_count "
        "ORDER BY c.code",
        env,
    )

    all_consistent = True
    for row in rows:
        code, quota, enrolled, count = row.split("|")
        consistent = int(enrolled) == int(count)
        if not consistent:
            all_consistent = False
        status = "KONSISTEN" if consistent else "TIDAK KONSISTEN"
        print(
            f"{code}  kuota={quota}  enrolled_count={enrolled}  "
            f"baris enrollments={count}  -> {status}"
        )
    return all_consistent


def print_student_credits(users: list[dict], env: dict) -> bool:
    ids = ", ".join(str(user["id"]) for user in users)
    rows = psql(
        "SELECT u.username, COALESCE(SUM(c.credits), 0) "
        "FROM users u "
        "LEFT JOIN enrollments e ON e.user_id = u.id "
        "LEFT JOIN courses c ON c.id = e.course_id "
        f"WHERE u.id IN ({ids}) "
        "GROUP BY u.id, u.username "
        "ORDER BY u.username",
        env,
    )

    all_within_limit = True
    for row in rows:
        username, total = row.split("|")
        total = int(total)
        if total > MAX_CREDITS:
            all_within_limit = False
        print(f"  {username}: {total} SKS")

    if all_within_limit:
        print(f"seluruh {len(users)} mahasiswa <= {MAX_CREDITS} SKS -> KONSISTEN")
    else:
        print(f"ADA mahasiswa yang melebihi {MAX_CREDITS} SKS -> TIDAK KONSISTEN")
    return all_within_limit


def deadlock_count(env: dict) -> int:
    rows = psql(
        "SELECT deadlocks FROM pg_stat_database "
        "WHERE datname = current_database()",
        env,
    )
    return int(rows[0])


def run_crowd(base: str, env: dict, secret: str, issuer: str) -> bool:
    users = load_users(60, env, secret, issuer)
    course_id = course_by_code("IF399", env)

    print("60 mahasiswa berebut kelas IF399 secara serentak...")
    start = time.perf_counter()
    tally = fire(
        60,
        lambda index: http_json(
            "POST",
            f"{base}/api/v1/courses/{course_id}/enroll",
            users[index]["token"],
        ),
    )
    elapsed = time.perf_counter() - start

    print("hasil request:")
    print_tally(tally)
    print(f"waktu total : {elapsed * 1000:.0f}ms")

    return print_consistency(["IF399"], env)


def run_credits(base: str, env: dict, secret: str, issuer: str) -> bool:
    users = load_users(10, env, secret, issuer)

    rows = psql(
        "SELECT id FROM courses "
        "WHERE code <> 'IF399' AND credits = 3 "
        "ORDER BY code LIMIT 12",
        env,
    )
    courses = [int(row) for row in rows]
    if len(courses) < 12:
        raise RuntimeError(
            f"butuh 12 kelas 3 SKS, hanya tersedia {len(courses)}"
        )

    print(
        f"10 mahasiswa, masing-masing mendaftar ke {len(courses)} kelas "
        f"3 SKS secara serentak (batas {MAX_CREDITS} SKS)..."
    )

    start = time.perf_counter()
    tally = fire(
        len(users) * len(courses),
        lambda index: http_json(
            "POST",
            f"{base}/api/v1/courses/{courses[index % len(courses)]}/enroll",
            users[index // len(courses)]["token"],
        ),
    )
    elapsed = time.perf_counter() - start

    print("hasil request:")
    print_tally(tally)
    print(f"waktu total : {elapsed * 1000:.0f}ms")

    return print_student_credits(users, env)


def run_swap(base: str, env: dict, secret: str, issuer: str) -> bool:
    users = load_users(40, env, secret, issuer)

    if101 = course_by_code("IF101", env)
    if102 = course_by_code("IF102", env)

    print("menyiapkan 20 pasang mahasiswa di IF101 dan IF102...")
    setup = fire(
        40,
        lambda index: http_json(
            "POST",
            f"{base}/api/v1/courses/"
            f"{if102 if index % 2 == 1 else if101}/enroll",
            users[index]["token"],
        ),
    )

    if setup.get("201 OK", 0) != len(users):
        print("hasil persiapan:")
        print_tally(setup)
        raise RuntimeError("persiapan gagal; jalankan reset.sql lalu coba lagi")

    before = deadlock_count(env)

    print("20 pasang mahasiswa pindah kelas berlawanan arah secara serentak...")
    start = time.perf_counter()
    tally = fire(
        40,
        lambda index: http_json(
            "POST",
            f"{base}/api/v1/enrollments/swap",
            users[index]["token"],
            {"from_course_id": if102, "to_course_id": if101}
            if index % 2 == 1
            else {"from_course_id": if101, "to_course_id": if102},
        ),
    )
    elapsed = time.perf_counter() - start

    after = deadlock_count(env)

    print("hasil request:")
    print_tally(tally)
    print(f"waktu total : {elapsed * 1000:.0f}ms")
    print(f"deadlock terdeteksi PostgreSQL selama uji: {after - before}")

    return print_consistency(["IF101", "IF102"], env)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Uji beban Modul 9 (versi Python, tanpa kompilasi)"
    )
    parser.add_argument(
        "-mode", "--mode",
        default="crowd",
        choices=["crowd", "credits", "swap"],
        help="mode uji: crowd, credits, swap",
    )
    parser.add_argument(
        "-base", "--base",
        default=None,
        help="base URL server (default http://127.0.0.1:$APP_PORT)",
    )
    args = parser.parse_args()

    env = load_env()
    secret = env.get("JWT_SECRET", "")
    if len(secret) < 32:
        print(
            "JWT_SECRET tidak diisi atau kurang dari 32 karakter",
            file=sys.stderr,
        )
        return 1

    issuer = env.get("JWT_ISSUER", "praktikum-backend")
    # 127.0.0.1, bukan "localhost": di Windows, urllib mencoba ::1 lebih
    # dulu dan setiap koneksi menunggu timeout kecil sebelum jatuh ke IPv4.
    # Itu membuat uji 40 request terlihat 2 detik padahal servernya cepat.
    base = args.base or (
        "http://127.0.0.1:" + env.get("APP_PORT", "3000")
    )
    base = base.rstrip("/")

    print("catatan: jalankan reset.sql sebelum setiap uji beban")

    try:
        if args.mode == "crowd":
            ok = run_crowd(base, env, secret, issuer)
        elif args.mode == "credits":
            ok = run_credits(base, env, secret, issuer)
        else:
            ok = run_swap(base, env, secret, issuer)
    except Exception as exc:  # noqa: BLE001
        print("gagal:", exc, file=sys.stderr)
        return 1

    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
