# bServer

Platform manajemen banyak server melalui **SSH** — portable, berjalan di PC kamu sendiri, **tanpa autentikasi user**. Semua dilayani secara lokal di `127.0.0.1`, tidak ada listener publik.

![Lisensi](https://img.shields.io/badge/license-MIT-blue.svg)

## Fitur

- **Vault terenkripsi** — Argon2id + AES-256-GCM; kredensial tidak pernah tersimpan plaintext. Auto-lock.
- **Inventaris server** — CRUD server + grup + tag, pencarian, import `~/.ssh/config` & JSON, export JSON.
- **Autentikasi SSH** — password, private key (dengan passphrase), atau SSH agent; **jump host 1 hop**.
- **TOFU host key** — fingerprint diverifikasi, koneksi ditolak bila host key berubah.
- **Terminal** — multi-tab PTY interaktif (xterm.js, `xterm-256color`).
- **Batch command** — eksekusi paralel (konkurensi default 5) + timeout + ringkasan.
- **Quick connect** — hubungkan ke server sekali tanpa menyimpan (kredensial hanya di memory, TTL 3 menit).
- **Snippet library** — simpan perintah favorit, jalankan langsung ke terminal aktif.
- **Broadcast terminal** — kirim satu perintah ke banyak server sekaligus (grid multi-terminal).
- **SFTP** — list, navigasi, upload/download dengan progress, mkdir.
- **Edit file remote** — buka file teks di server, edit, simpan kembali (maks 1 MB).
- **Dashboard metrik** — CPU / RAM / disk / load / uptime, plus **riwayat historis** (sampling 60 dtk, retensi 7 hari).
- **Alert ambang** — notifikasi lokal saat CPU/RAM/disk melewati ambang (dapat diatur).
- **Status systemd/Docker** — lihat service & container di server remote.
- **Log aktivitas** — perintah tercatat, secret diredaksi.
- **Tema** — minimalis, aksen biru `#2563EB`/`#3B82F6`, light/dark, UI Bahasa Indonesia.

## Menjalankan

Binary statis sudah tersedia. Jalankan:

```bash
./dist/bServer-linux-amd64                 # buka browser otomatis (dengan token)
./dist/bServer-linux-amd64 --no-open --port 8321 --data ./data
```

Flag:
- `--port N`  — port HTTP (default `0` = acak).
- `--data DIR` — direktori data (default `<dir-exe>/data`, di samping binary). Dibuat `0700`.
- `--no-open` — jangan buka browser otomatis.
- `-metric-interval Ns` — interval sampling metrik (default `60s`).

Data tersimpan di direktori `data/`: `vault.json` (kunci terenkripsi), `app.db` (SQLite),
`runtime.json` (info sesi, `0600`).

## Build dari sumber

Prasyarat: Go 1.24+, Node 18+.

```bash
# frontend
cd web && npm install && npm run build && cd ..

# backend (binary tunggal, SPA sudah ter-embed)
go build -o dist/bServer-linux-amd64 ./cmd/bserver
```

Atau via Makefile:

```bash
make build      # npm build + go build (linux/amd64)
make run        # jalankan binary dist
make build-all  # cross-compile semua platform (lihat di bawah)
make release    # build-all + kemas tarball/zip ke dist/release/
make demo       # naikkan 3 container SSH demo (docker compose)
make clean      # hapus dist + data
```

## Distribusi multi-platform

Binary statis (pure-Go, SQLite tanpa cgo) — bisa cross-compile dari satu mesin:

| Platform | Artefak |
|----------|---------|
| Linux amd64 / arm64 | `dist/bserver-linux-*` |
| Windows amd64 | `dist/bserver-windows-amd64.exe` |
| macOS amd64 / arm64 | `dist/bserver-darwin-*` |

```bash
make release VERSION=0.1.0
# hasil: dist/release/bserver-<os>-<arch>-<versi>.tar.gz / .zip
```

Catatan: binary macOS perlu tanda tangan/notarisasi (codesign) agar tidak diblokir Gatekeeper saat didistribusikan ke mesin lain.

## Demo 3 server SSH (opsional)

```bash
cd dev && docker compose up -d
# web-01  127.0.0.1:2222
# web-02  127.0.0.1:2223
# db-01   127.0.0.1:2224
# user: bserver   password: bserver123
```

Lalu di aplikasi: tambah server dengan host `127.0.0.1`, port `2222` (dst), auth password.

## Keamanan

- **Vault**: Argon2id (m=64 MiB, t=3, p=4) → AES-256-GCM; verifier + AAD per-record; kunci hanya di RAM, di-zeroize saat lock.
- **Jaringan**: bind `127.0.0.1` saja, port acak, token sesi 32-byte wajib, cek Origin/Host.
- **Host key**: TOFU pinning — perubahan fingerprint ditolak (anti MITM).
- **Single-instance**: data dir dikunci dengan `.lock` (flock) — dua instance tidak bisa berbagi data dir yang sama.
- **Tanpa telemetri**, tanpa akses jaringan keluar selain koneksi SSH ke server kamu.
- Jaga direktori `data/` — di dalamnya ada semua inventaris & kredensial (terenkripsi).

## Struktur

```
cmd/bserver/    entrypoint HTTP + konfigurasi
internal/vault/ enkripsi kredensial (Argon2id + AES-GCM)
internal/store/ SQLite (server, grup, kredensial, log, metrik, alert)
internal/sshx/  koneksi SSH + jump host + TOFU
internal/term/  PTY ⇄ WebSocket
internal/batch/ batch command paralel
internal/sftpx/ SFTP
internal/metrics/ metrik + sampler riwayat
internal/svcs/  status systemd/Docker
internal/httpapi/ API + token middleware
web/            SPA React + TS + xterm.js (di-embed)
dev/            docker compose server SSH demo
scripts/        build & packaging multi-platform
```

## Lisensi

[MIT](./LICENSE) — bebas digunakan, dimodifikasi, dan didistribusikan.
