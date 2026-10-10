# Roadmap — zzte realtime & event-driven

> Status jujur per 2026-10-09. "Selesai" = kode ada di patch + lolos build & unit test di sandbox.
> **Belum ada yang diuji ke OLT asli.**

## ✅ Selesai (di patch, sandbox-tested)

### Batch 1 — Realtime troubleshooting
- `GET /live` per PON port: status + Rx/Tx segar (±2–4 dtk/port, estimasi), nama/SN dari cache
- Lock SNMP per-PON (ganti lock se-OLT), `MaxRepetitions` 3 → **20** (bukan 50: agen SNMP C320/C300 rawan — response PDU raksasa bikin CPU control-plane spike; 20 = titik aman, bisa override per OLT via `max_repetitions`)
- Singleflight dedup untuk request ONU detail & /live yang bersamaan
- Poller optical 60 dtk → 10 mnt + anti-overlap (data realtime diambil via /live, bukan poller)
- `ONUListItem` dapat field `txPower`

### Batch 2 — Proteksi OLT
- `HotTracker`: port yang baru dibuka user = "panas", dipoll tiap siklus; port dingin tiap 6 siklus **dengan stagger** (rotasi deterministik per port, bukan borongan — mencegah ledakan beban 6x tiap siklus ke-6 yang memicu degraded mode di port padat, temuan simulasi)
- Circuit-breaker + backoff saat OLT error
- `GetONUList` baca nama/SN dari cache dulu (2 walk SNMP dihemat bila cache ada)

### Batch 3 — Event-driven up/down via syslog (nol beban OLT)
- UDP syslog listener (port 514, configurable), parser ZTE: klasifikasi `dying-gasp` (power) vs `LOS` (fiber)
- Event bus + dedup 60 dtk, mapper IP pengirim → oltID (multi-OLT, otomatis fallback ke host SNMP terdaftar)
- Status ONU di-patch langsung di cache Redis → dashboard segar detik itu juga
- `GET /api/v1/events/stream` (SSE realtime), `GET /api/v1/events/recent`
- Alert engine anti-spam (agregasi 90 dtk, mass-down ≥5 → 1 alert, cooldown 60 mnt/ONU, severity critical/warning/info) → `GET /api/v1/alerts`
- **Verifikasi ulang sebelum alert (anti false-alarm flapping):** di momen batch-flush, setiap calon alert di-verify via satu SNMP GET terarah ke OID status ONU (`...3.28.2.1.4`); yang sudah kembali online dibuang. Mass-down yang sebagian pulih turun jadi alert per-ONU untuk yang masih down. Aktif via `event.alert.reverify` (default `true`); SNMP error → trust syslog (alert tetap keluar). Di-test dengan 6 unit test baru di `internal/event/alert_test.go`.

## ⏳ Menunggu (butuh aksi manusia)

- [ ] **Apply patch** ke repo (`git apply --check` → `git apply`), `go build ./...`
- [ ] **Config OLT**: arahkan syslog ke IP:port zzte; pastikan alarm `onu-dying-gasp`, `onu-deregister`, `onu-link-lost` aktif
- [ ] **Config zzte**: `event.syslog.enabled: true` (default `false`, aman bila OLT belum dikonfigurasi)
- [ ] **Uji OLT asli**: cabut-colok 1 ONU → event masuk? klasifikasi cause benar? (kirim contoh baris syslog asli bila ada yang tak ter-parse — pola bisa beda antar firmware)
- [ ] **Uji beban**: 4+ user simultan di PON berbeda + sama; pantau CPU/memory OLT sebelum naikkan konkurensi
- [ ] **Integrasi frontend**: konsumsi `GET /api/v1/alerts` (polling) atau SSE `/api/v1/events/stream` (`event: alert`)

## 🔮 Berikutnya (belum dikerjakan)

- **History & trending redaman** per ONU (grafik naik/turun, alert prediktif 3 dB/minggu warning, 5 dB/minggu critical) — pembeda utama vs NMS generik
- Notifikasi WhatsApp (opsional, di atas `/alerts`)
- Vendor OLT selain ZTE (arsitektur sudah vendor-agnostic di level event)
- Kalibrasi skala nilai optik & OID per versi firmware

## Prinsip yang dipegang

1. **Jangan bebani OLT.** Status up/down = event-driven (OLT didengarkan, tidak ditanya). Polling hanya untuk data optik, ringan & terjadwal.
2. **Multi-OLT sejak awal.** Semua event membawa oltID.
3. **Anti-spam.** Teknisi dapat 1 info yang jelas, bukan 50 notifikasi.
