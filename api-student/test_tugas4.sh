#!/usr/bin/env bash
# Tugas 4 - 8 Pengujian Keamanan (Modul 5 hal.25-26)
# Jalankan:  bash test_tugas4.sh
# Prasyarat: go run .  (server di :3000), DB sudah migrate 002_auth.sql
set -u

B=http://localhost:3000/api/v1
J="Content-Type: application/json"

echo "=== 0. Setup: register & login ambil token valid ==="
# bersihkan user lama jika perlu (TRUNCATE sudah di psql)
curl -s -X POST $B/auth/register -H "$J" -d '{"username":"sari","email":"sari@example.com","password":"rahasia123"}' | cat
echo ""
# login valid - simpan token
LOGIN_RES=$(curl -s -X POST $B/auth/login -H "$J" -d '{"username":"sari","password":"rahasia123"}')
echo "$LOGIN_RES" | cat
# extract dengan python (tanpa jq agar kompatibel)
ACCESS=$(echo "$LOGIN_RES" | python3 -c "import sys,json; print(json.load(sys.stdin).get('data',{}).get('access_token',''))")
REFRESH=$(echo "$LOGIN_RES" | python3 -c "import sys,json; print(json.load(sys.stdin).get('data',{}).get('refresh_token',''))")
echo "ACCESS=$ACCESS"
echo "REFRESH=$REFRESH"
echo ""

echo "=== 1. Akses /students tanpa token -> 401 + WWW-Authenticate ==="
curl -i $B/students/ -H "$J"
echo -e "\n"

echo "=== 2. Token diubah 1 karakter -> 401 access token tidak valid ==="
# ubah huruf terakhir
TAMPERED="${ACCESS%?}X"
curl -i $B/students/ -H "Authorization: Bearer $TAMPERED"
echo -e "\n"

echo "=== 3. Token alg:none -> 401 (algorithm confusion tertutup) ==="
# ambil payload valid, ganti header jadi none, hapus signature
PAYLOAD=$(echo "$ACCESS" | cut -d. -f2)
NONE_TOKEN="eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.${PAYLOAD}."
echo "NONE_TOKEN=$NONE_TOKEN"
curl -i $B/students/ -H "Authorization: Bearer $NONE_TOKEN"
echo -e "\n"

echo "=== 4. Login username tidak ada vs password salah -> pesan SAMA PERSIS ==="
echo "-- a) username tidak ada --"
curl -s -X POST $B/auth/login -H "$J" -d '{"username":"tidakada","password":"salahsekali9"}' | cat
echo ""
echo "-- b) username ada, password salah --"
curl -s -X POST $B/auth/login -H "$J" -d '{"username":"sari","password":"salahsekali9"}' | cat
echo ""
echo "Harus sama: {\"success\":false,\"message\":\"username atau password salah\"}"
echo ""

echo "=== 5. 6x login gagal berturut-turut -> 5x 401 lalu 429 + Retry-After ==="
for i in 1 2 3 4 5 6; do
  echo -n "Percobaan $i: "
  curl -s -i -X POST $B/auth/login -H "$J" -d '{"username":"sari","password":"salahsekali9"}' | grep -E "HTTP/|Retry-After"
done
echo ""

echo "=== 6. Kirim role:admin saat register -> tetap user (anti mass assignment) ==="
curl -s -X POST $B/auth/register -H "$J" -d '{"username":"eviladmin","email":"evil@example.com","password":"rahasia123","role":"admin"}' | cat
echo ""
echo "Cek role di response harus \"role\":\"user\" bukan admin"
echo ""

echo "=== 7. Response register & login TIDAK memuat field password ==="
echo "-- register response --"
curl -s -X POST $B/auth/register -H "$J" -d '{"username":"cekpass","email":"cekpass@example.com","password":"rahasia123"}' | python3 -c "import sys,json; d=json.load(sys.stdin); print('password in response?', 'password' in str(d).lower()); print(d)"
echo ""
echo "-- login response --"
echo "$LOGIN_RES" | python3 -c "import sys,json; d=json.load(sys.stdin); print('password in response?', 'password' in str(d).lower()); print(d)"
echo ""

echo "=== 8. Refresh token rotasi -> pakai token lama kedua kali -> 401 ==="
echo "-- refresh pertama (200) --"
REFRESH_RES1=$(curl -s -X POST $B/auth/refresh -H "$J" -d "{\"refresh_token\":\"$REFRESH\"}")
echo "$REFRESH_RES1" | cat
echo ""
echo "-- refresh kedua dengan token LAMA yang sama (harus 401) --"
curl -i -X POST $B/auth/refresh -H "$J" -d "{\"refresh_token\":\"$REFRESH\"}"
echo ""
echo "Jika 401, rotasi bekerja."

echo ""
echo "=== BONUS: /health tetap publik (200) & /auth/me butuh token ==="
curl -i $B/health
echo ""
curl -i $B/auth/me -H "Authorization: Bearer $ACCESS"
echo ""
