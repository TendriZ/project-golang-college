#!/usr/bin/env python3
"""Alat uji beban Modul 9 untuk api-student — pembimbing tugas akhir.

Salinan alat uji beban latihan-fiber yang diarahkan ke endpoint baru:

    python tools/loadtest/loadtest.py -mode assign
    python tools/loadtest/loadtest.py -mode same-student
    python tools/loadtest/loadtest.py -mode switch
    python tools/loadtest/loadtest.py -mode switch-full
    python tools/loadtest/loadtest.py -mode quota

Server harus sedang berjalan. Alat ini menyiapkan sendiri data uji
(mahasiswa NIM 990001..990030 dan pembimbing bernama "Loadtest ..."),
lalu membersihkannya sebelum setiap percobaan.

Mode assign adalah bukti D.3: 30 penetapan serentak ke pembimbing
berkuota 3 harus menghasilkan TEPAT 3 keberhasilan.
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

TOKEN_TTL_SECONDS = 3600
STUDENT_NIM_START = 990001
STUDENT_NIM_END = 990030
ASSIGN_COUNT = 30
TARGET_QUOTA = 3

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
        "-d", env.get("DB_NAME", "db_students"),
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


def psql_exec(sql: str, env: dict) -> None:
    psql(sql, env)


def psql_one(sql: str, env: dict) -> str:
    rows = psql(sql, env)
    if not rows:
        raise RuntimeError("query tidak mengembalikan baris: " + sql)
    return rows[0]


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


def load_admin(env: dict, secret: str, issuer: str) -> dict:
    rows = psql(
        "SELECT id, username, role FROM users "
        "WHERE role = 'admin' ORDER BY id LIMIT 1",
        env,
    )
    if not rows:
        raise RuntimeError("tidak ada user ber-role admin")
    user_id, username, role = rows[0].split("|")
    user = {"id": int(user_id), "username": username, "role": role}
    user["token"] = make_token(user, secret, issuer)
    return user


def http_json(method: str, url: str, token: str, body=None):
    headers = {
        "Authorization": "Bearer " + token,
        "Accept": "application/json",
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


def deadlock_count(env: dict) -> int:
    row = psql_one(
        "SELECT deadlocks FROM pg_stat_database "
        "WHERE datname = current_database()",
        env,
    )
    return int(row)


def prepare_students(count: int, env: dict) -> list[int]:
    if count > STUDENT_NIM_END - STUDENT_NIM_START + 1:
        raise RuntimeError("jumlah mahasiswa uji melebihi rentang NIM")

    psql_exec(
        "INSERT INTO students (nim, name, grade, is_active, owner_id) "
        "SELECT "
        f"{STUDENT_NIM_START} + g - 1, "
        "'Loadtest Student ' || LPAD(g::TEXT, 2, '0'), "
        "0, TRUE, NULL "
        f"FROM generate_series(1, {STUDENT_NIM_END - STUDENT_NIM_START + 1}) AS g "
        "ON CONFLICT (nim) DO NOTHING",
        env,
    )

    psql_exec(
        "DELETE FROM supervisions WHERE student_id IN ("
        "SELECT id FROM students WHERE nim BETWEEN "
        f"{STUDENT_NIM_START} AND {STUDENT_NIM_END})",
        env,
    )
    recount_all(env)

    rows = psql(
        "SELECT id FROM students WHERE nim BETWEEN "
        f"{STUDENT_NIM_START} AND {STUDENT_NIM_END} "
        f"ORDER BY nim LIMIT {count}",
        env,
    )
    ids = [int(row) for row in rows]
    if len(ids) < count:
        raise RuntimeError(f"mahasiswa uji kurang: butuh {count}, ada {len(ids)}")
    return ids


def recount_all(env: dict) -> None:
    psql_exec(
        "UPDATE supervisors s SET supervised_count = ("
        "SELECT COUNT(*) FROM supervisions v WHERE v.supervisor_id = s.id)",
        env,
    )


def fresh_supervisor(name: str, quota: int, env: dict) -> int:
    psql_exec(
        "DELETE FROM supervisions WHERE supervisor_id IN ("
        "SELECT id FROM supervisors WHERE name = '" + name + "')",
        env,
    )
    psql_exec(
        "DELETE FROM supervisors WHERE name = '" + name + "'",
        env,
    )
    row = psql_one(
        "INSERT INTO supervisors (name, quota) VALUES "
        f"('{name}', {quota}) RETURNING id",
        env,
    )
    return int(row)


def supervisor_state(supervisor_id: int, env: dict) -> tuple[int, int, int, int]:
    row = psql_one(
        "SELECT s.quota, s.supervised_count, s.version, "
        "COUNT(v.id) FROM supervisors s "
        "LEFT JOIN supervisions v ON v.supervisor_id = s.id "
        f"WHERE s.id = {supervisor_id} "
        "GROUP BY s.id, s.quota, s.supervised_count, s.version",
        env,
    )
    quota, count, version, rows = row.split("|")
    return int(quota), int(count), int(version), int(rows)


def reset_supervisor(supervisor_id: int, quota: int, env: dict) -> None:
    psql_exec(
        "DELETE FROM supervisions WHERE supervisor_id = "
        f"{supervisor_id}",
        env,
    )
    psql_exec(
        "UPDATE supervisors SET quota = "
        f"{quota}, supervised_count = 0, version = 1 WHERE id = {supervisor_id}",
        env,
    )


def print_state(supervisor_id: int, env: dict) -> bool:
    quota, count, _, rows = supervisor_state(supervisor_id, env)
    consistent = count == rows and count <= quota
    label = "KONSISTEN" if consistent else "TIDAK KONSISTEN"
    print(
        f"  pembimbing id={supervisor_id}  kuota={quota}  "
        f"supervised_count={count}  baris supervisions={rows}  -> {label}"
    )
    if count > quota:
        print(
            f"  supervised_count MELEBIHI kuota sebanyak {count - quota} "
            "-> TIDAK KONSISTEN"
        )
    return consistent


def verify_no_duplicate_students(env: dict) -> bool:
    row = psql_one(
        "SELECT COUNT(*) FROM ("
        "SELECT student_id FROM supervisions "
        "GROUP BY student_id HAVING COUNT(*) > 1"
        ") AS ganda",
        env,
    )
    if int(row) == 0:
        print("  tidak ada mahasiswa dengan lebih dari satu pembimbing -> KONSISTEN")
        return True
    print(f"  ADA {row} mahasiswa dengan lebih dari satu pembimbing -> TIDAK KONSISTEN")
    return False


def run_assign(base: str, env: dict, admin: dict, repeat: int) -> bool:
    students = prepare_students(ASSIGN_COUNT, env)
    target = fresh_supervisor("Loadtest Target", TARGET_QUOTA, env)

    print(
        f"{ASSIGN_COUNT} mahasiswa ditetapkan serentak ke pembimbing "
        f"id={target} (kuota {TARGET_QUOTA})..."
    )

    all_passed = True
    for run in range(1, repeat + 1):
        reset_supervisor(target, TARGET_QUOTA, env)

        start = time.perf_counter()
        tally = fire(
            ASSIGN_COUNT,
            lambda index: http_json(
                "POST",
                f"{base}/api/v1/supervisors/{target}/assign",
                admin["token"],
                {"student_id": students[index]},
            ),
        )
        elapsed = time.perf_counter() - start

        print(f"--- percobaan {run}/{repeat} ---")
        print("hasil request:")
        print_tally(tally)
        print(f"waktu total : {elapsed * 1000:.0f}ms")
        consistent = print_state(target, env)
        unique = verify_no_duplicate_students(env)

        expected = (
            tally.get("201 OK", 0) == TARGET_QUOTA
            and tally.get("409 SUPERVISOR_FULL", 0) == ASSIGN_COUNT - TARGET_QUOTA
        )
        passed = expected and consistent and unique
        all_passed = all_passed and passed
        print(f"percobaan {run}: {'LULUS' if passed else 'GAGAL'}")

        if run < repeat:
            print()

    print(
        f"kesimpulan: {('SEMUA percobaan lulus' if all_passed else 'ADA percobaan gagal')}"
    )
    return all_passed


def run_switch(base: str, env: dict, admin: dict, repeat: int) -> bool:
    count = 20
    students = prepare_students(count, env)
    sup_a = fresh_supervisor("Loadtest A", 50, env)
    sup_b = fresh_supervisor("Loadtest B", 50, env)

    print(
        f"{count} mahasiswa pindah pembimbing berlawanan arah secara serentak "
        f"(id={sup_a} <-> id={sup_b})..."
    )

    all_passed = True
    for run in range(1, repeat + 1):
        psql_exec(
            "DELETE FROM supervisions WHERE supervisor_id IN "
            f"({sup_a}, {sup_b})",
            env,
        )
        values = ", ".join(
            f"({student}, {sup_a if index % 2 == 0 else sup_b})"
            for index, student in enumerate(students)
        )
        psql_exec(
            "INSERT INTO supervisions (student_id, supervisor_id) VALUES "
            + values,
            env,
        )
        recount_all(env)

        before = deadlock_count(env)
        start = time.perf_counter()
        tally = fire(
            count,
            lambda index: http_json(
                "POST",
                f"{base}/api/v1/supervisions/switch",
                admin["token"],
                {
                    "student_id": students[index],
                    "supervisor_id": sup_b if index % 2 == 0 else sup_a,
                },
            ),
        )
        elapsed = time.perf_counter() - start
        after = deadlock_count(env)

        print(f"--- percobaan {run}/{repeat} ---")
        print("hasil request:")
        print_tally(tally)
        print(f"waktu total : {elapsed * 1000:.0f}ms")
        print(f"deadlock terdeteksi PostgreSQL selama uji: {after - before}")

        a_quota, a_count, _, a_rows = supervisor_state(sup_a, env)
        b_quota, b_count, _, b_rows = supervisor_state(sup_b, env)
        print(
            f"  pembimbing A id={sup_a}  kuota={a_quota}  "
            f"supervised_count={a_count}  baris={a_rows}"
        )
        print(
            f"  pembimbing B id={sup_b}  kuota={b_quota}  "
            f"supervised_count={b_count}  baris={b_rows}"
        )
        unique = verify_no_duplicate_students(env)

        passed = (
            tally.get("200 OK", 0) == count
            and after == before
            and a_count == a_rows
            and b_count == b_rows
            and a_count + b_count == count
            and unique
        )
        all_passed = all_passed and passed
        print(f"percobaan {run}: {'LULUS' if passed else 'GAGAL'}")

        if run < repeat:
            print()

    print(
        f"kesimpulan: {('SEMUA percobaan lulus' if all_passed else 'ADA percobaan gagal')}"
    )
    return all_passed


def run_same_student(base: str, env: dict, admin: dict, repeat: int) -> bool:
    """20 request untuk SATU mahasiswa yang sama, pembimbing berkuota 1.

    Dengan kunci row students, request pertama menetapkan pembimbing dan
    request berikutnya (yang menunggu kunci) melihat penetapan itu lalu
    mendapat ALREADY_SUPERVISED. Tanpa kunci, sebagian request sudah
    terlanjur melewati pemeriksaan sebelum request pertama commit, lalu
    menghabiskan slot dan mendapat SUPERVISOR_FULL.
    """
    students = prepare_students(1, env)
    student = students[0]
    target = fresh_supervisor("Loadtest Same Student", 1, env)

    print(
        f"20 penetapan serentak untuk mahasiswa id={student} ke "
        f"pembimbing id={target} (kuota 1)..."
    )

    all_passed = True
    for run in range(1, repeat + 1):
        psql_exec(
            "DELETE FROM supervisions WHERE student_id = "
            f"{student}",
            env,
        )
        psql_exec(
            "UPDATE supervisors SET supervised_count = 0, quota = 1, "
            f"version = 1 WHERE id = {target}",
            env,
        )

        start = time.perf_counter()
        tally = fire(
            20,
            lambda index: http_json(
                "POST",
                f"{base}/api/v1/supervisors/{target}/assign",
                admin["token"],
                {"student_id": student},
            ),
        )
        elapsed = time.perf_counter() - start

        print(f"--- percobaan {run}/{repeat} ---")
        print("hasil request:")
        print_tally(tally)
        print(f"waktu total : {elapsed * 1000:.0f}ms")

        row = psql_one(
            "SELECT COUNT(*) FROM supervisions "
            f"WHERE student_id = {student}",
            env,
        )
        print(f"  baris supervisions untuk mahasiswa id={student}: {row}")
        consistent = print_state(target, env)

        passed = (
            tally.get("201 OK", 0) == 1
            and tally.get("409 ALREADY_SUPERVISED", 0) == 19
            and int(row) == 1
            and consistent
        )
        all_passed = all_passed and passed
        print(f"percobaan {run}: {'LULUS' if passed else 'GAGAL'}")

        if run < repeat:
            print()

    print(
        f"kesimpulan: {('SEMUA percobaan lulus' if all_passed else 'ADA percobaan gagal')}"
    )
    return all_passed


def run_switch_full(base: str, env: dict, admin: dict) -> bool:
    students = prepare_students(2, env)
    old = fresh_supervisor("Loadtest Old", 10, env)
    full = fresh_supervisor("Loadtest Full", 1, env)

    psql_exec(
        "INSERT INTO supervisions (student_id, supervisor_id) VALUES "
        f"({students[0]}, {full}), ({students[1]}, {old})",
        env,
    )
    recount_all(env)

    print(
        "pembimbing tujuan penuh (kuota 1 sudah terisi). "
        "Mahasiswa kedua mencoba pindah ke sana..."
    )
    status, code, _ = http_json(
        "POST",
        f"{base}/api/v1/supervisions/switch",
        admin["token"],
        {"student_id": students[1], "supervisor_id": full},
    )
    label = result_label((status, code, ""))
    print(f"hasil request: {label}")

    full_state = supervisor_state(full, env)
    old_state = supervisor_state(old, env)
    print(
        f"  pembimbing penuh id={full}  kuota={full_state[0]}  "
        f"supervised_count={full_state[1]}  baris={full_state[3]}"
    )
    print(
        f"  pembimbing lama id={old}  kuota={old_state[0]}  "
        f"supervised_count={old_state[1]}  baris={old_state[3]}"
    )

    row = psql_one(
        "SELECT supervisor_id FROM supervisions "
        f"WHERE student_id = {students[1]}",
        env,
    )
    stayed = int(row) == old
    print(
        "  mahasiswa tetap di pembimbing lama -> "
        + ("YA (ATOMIK)" if stayed else "TIDAK (GAGAL ATOMIK)")
    )

    passed = (
        label == "409 SUPERVISOR_FULL"
        and full_state[1] == 1
        and full_state[3] == 1
        and old_state[1] == 1
        and old_state[3] == 1
        and stayed
    )
    print(f"hasil: {'LULUS' if passed else 'GAGAL'}")
    return passed


def run_quota(base: str, env: dict, admin: dict) -> bool:
    students = prepare_students(3, env)
    supervisor = fresh_supervisor("Loadtest Quota", 5, env)

    values = ", ".join(f"({student}, {supervisor})" for student in students)
    psql_exec(
        "INSERT INTO supervisions (student_id, supervisor_id) VALUES " + values,
        env,
    )
    recount_all(env)

    def patch(quota: int, version: int):
        return http_json(
            "PATCH",
            f"{base}/api/v1/supervisors/{supervisor}",
            admin["token"],
            {"quota": quota, "version": version},
        )

    print("PATCH kuota=4, version=1 (harus 200, version menjadi 2)...")
    first = patch(4, 1)
    print("  " + result_label(first))

    print("PATCH kuota=3, version=1 (version lama, harus 409 VERSION_CONFLICT)...")
    second = patch(3, 1)
    print("  " + result_label(second))

    print("PATCH kuota=2, version=2 (di bawah 3 bimbingan, harus 409 QUOTA_BELOW_SUPERVISED)...")
    third = patch(2, 2)
    print("  " + result_label(third))

    quota, count, version, rows = supervisor_state(supervisor, env)
    print(
        f"  keadaan akhir: kuota={quota}  supervised_count={count}  "
        f"version={version}  baris={rows}"
    )

    passed = (
        result_label(first) == "200 OK"
        and result_label(second) == "409 VERSION_CONFLICT"
        and result_label(third) == "409 QUOTA_BELOW_SUPERVISED"
        and quota == 4
        and count == 3
        and version == 2
        and rows == 3
    )
    print(f"hasil: {'LULUS' if passed else 'GAGAL'}")
    return passed


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Uji beban pembimbing tugas akhir (Modul 9 D.3)"
    )
    parser.add_argument(
        "-mode", "--mode",
        default="assign",
        choices=["assign", "same-student", "switch", "switch-full", "quota"],
        help="mode uji: assign, same-student, switch, switch-full, quota",
    )
    parser.add_argument(
        "-base", "--base",
        default=None,
        help="base URL server (default http://127.0.0.1:$APP_PORT)",
    )
    parser.add_argument(
        "-repeat", "--repeat",
        type=int,
        default=3,
        help="jumlah percobaan berurutan (default 3)",
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
    base = args.base or (
        "http://127.0.0.1:" + env.get("APP_PORT", "3000")
    )
    base = base.rstrip("/")

    print("catatan: alat ini menyiapkan dan mereset data uji secara otomatis")
    print(f"server: {base}")
    print()

    try:
        admin = load_admin(env, secret, issuer)
        print(f"admin: {admin['username']} (id={admin['id']})")
        print()

        if args.mode == "assign":
            ok = run_assign(base, env, admin, args.repeat)
        elif args.mode == "same-student":
            ok = run_same_student(base, env, admin, args.repeat)
        elif args.mode == "switch":
            ok = run_switch(base, env, admin, args.repeat)
        elif args.mode == "switch-full":
            ok = run_switch_full(base, env, admin)
        else:
            ok = run_quota(base, env, admin)
    except Exception as exc:  # noqa: BLE001
        print("gagal:", exc, file=sys.stderr)
        return 1

    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
