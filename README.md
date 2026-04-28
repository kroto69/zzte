# OLT ZTE C320 Multi-OLT Monitoring API

REST API backend untuk monitoring multiple OLT ZTE C320 dengan fitur auto-detect firmware (V1/V2), SNMP-based ONU data retrieval, strategi anti-tabrakan data multi-OLT, dan provisioning berbasis template.

## Tech Stack

- Go 1.21+
- Chi Router (HTTP)
- GoSNMP (SNMP client)
- Redis (caching)
- Viper (configuration)
- Zerolog (logging)

## Quick Start

### Dengan Docker

```bash
# Build dan jalankan
docker-compose up -d

# Cek logs
docker-compose logs -f olt-monitor
```

### Tanpa Docker

```bash
# Install dependencies
go mod download

# Jalankan Redis
redis-server

# Jalankan aplikasi
go run cmd/main.go
```

## Konfigurasi

Edit `config/olt_config.yaml`:

```yaml
server:
  host: 0.0.0.0
  port: 8081
  log_level: info
  jwt_secret: "" # gunakan env OLT_JWT_SECRET

redis:
  host: localhost
  port: 6379
  db: 0

search:
  enabled: false    # background indexer (default: off)
  interval: 0        # interval dalam menit

optical_poller:
  enabled: true       # polling status + rx power
  interval: 60        # detik

names_poller:
  enabled: true       # polling nama + serial number
  interval: 28800      # detik (8 jam)

olts:
  olt_1:
    name: "ZTE C320"
    host: "10.5.0.5"
    port: 161
    community: "public"
    timeout: 5
    retries: 2
    poll_interval: 120   # detik, 0 = pakai global
    telnet:
      user: "zte"
      password: "zte"
      port: 23
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| OLT_SERVER_PORT | 8081 | HTTP port |
| OLT_SERVER_HOST | 0.0.0.0 | HTTP bind host |
| OLT_SERVER_LOG_LEVEL | info | Log level |
| OLT_JWT_SECRET | generated per process | JWT signing secret |
| OLT_REDIS_HOST | localhost | Redis host |
| OLT_REDIS_PORT | 6379 | Redis port |
| OLT_REDIS_PASSWORD | "" | Redis password |
| OLT_REDIS_DB | 0 | Redis database |

Per-OLT secret bisa diinject tanpa menulis credential ke YAML:

| Variable | Description |
|----------|-------------|
| `OLT_<OLT_ID>_SNMP_COMMUNITY` | Override SNMP community untuk OLT tertentu |
| `OLT_<OLT_ID>_TELNET_USER` | Override telnet username untuk OLT tertentu |
| `OLT_<OLT_ID>_TELNET_PASSWORD` | Override telnet password untuk OLT tertentu |

Contoh untuk OLT ID `olt_1`:

```bash
export OLT_OLT_1_SNMP_COMMUNITY="private"
export OLT_OLT_1_TELNET_USER="zte"
export OLT_OLT_1_TELNET_PASSWORD="super-secret"
```

## Architecture

### Poller System

Aplikasi menggunakan 2 poller background, masing-masing **goroutine per-OLT** dengan interval yang bisa diatur per OLT via `poll_interval`. Setiap OLT punya ticker sendiri sehingga OLT besar bisa diset lebih lambat tanpa menghambat OLT kecil.

| Poller | Data | Interval Default | Koneksi |
|--------|------|----------|---------|
| **Optical Poller** | Status + RX Power per PON | 60 detik (global) | Terpisah (no lock) |
| **Names Poller** | Nama + Serial Number per PON | 8 jam (global) | Terpisah (no lock) |

**Per-OLT interval**: Set `poll_interval` (detik) di konfigurasi OLT untuk override interval global. Jika `poll_interval: 0` atau tidak diset, pakai global.

```yaml
olts:
  olt_1:
    poll_interval: 120   # 2 menit — OLT dengan 16 PON
  olt_2:
    poll_interval: 180   # 3 menit — OLT dengan 32 PON
  olt_3:                 # tidak diset — pakai global (60s / 28800s)
```

**Optical Poller** menggunakan scoped BulkWalk per-PON (tidak global) untuk menghindari timeout pada OLT dengan banyak ONU. Semaphore(2) membatasi concurrent PON walk, 200ms jeda antar PON.

**Names Poller** walk Name+SN per-PON. Karena Name/SN jarang berubah, interval 8 jam cukup. Data disimpan di names cache terpisah (TTL 10 jam).

### Cache Architecture

Setiap key di Redis include OLT ID untuk isolasi data:

| Cache | Key Pattern | TTL | Source |
|-------|-------------|-----|--------|
| ONU List | `olt:{oltId}:board:{board}:pon:{pon}:list` | ~74-120s (adaptive) | on-demand atau optical poller |
| Names | `olt:{oltId}:board:{board}:pon:{pon}:names` | 10h | on-demand atau names poller |
| Detail | `olt:{oltId}:onu:{board}:{pon}:{onuId}` | ~2-4min (adaptive) | on-demand GET |
| PON List | `olt:{oltId}:board:{board}:pon:list` | 5min | on-demand |
| OLT Info | `olt:{oltId}:info` | 5min | on-demand |
| Health | `olt:{oltId}:health` | 30s | on-demand |

**Adaptive TTL**: ONUList dan Detail cache menggunakan `baseTTL + (duration × 2)`, max `baseTTL × 5`. Semakin lama SNMP walk, semakin panjang TTL-nya.

**Strategi 3-level fallback** untuk GetONUList:
1. Cache lengkap (Name terisi) → return langsung (fast, < 100ms)
2. Cache incomplete + Names cache → merge → return (fast)
3. Cache kosong/gagal → on-demand SNMP walk → simpan ke list + names cache → return (lambat, 7-12s)

### Graceful Shutdown

Aplikasi menangani SIGINT/SIGTERM untuk graceful shutdown:
1. Berhenti menerima request baru
2. Stop optical poller
3. Stop names poller
4. Tutup koneksi Redis

## API Endpoints

### Authentication

```bash
# Login
curl -X POST http://localhost:8081/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}'

# Response
{
  "success": true,
  "data": { "token": "eyJhbGciOi..." }
}
```

### OLT Management

#### Test Connection

```bash
curl -X POST http://localhost:8081/api/v1/olt/test-connection \
  -H "Content-Type: application/json" \
  -d '{"host":"10.5.0.4","port":161,"community":"public"}'

# Response
{
  "success": true,
  "data": {
    "firmwareVersion": "v2",
    "fullVersion": "V2.1.0"
  }
}
```

#### Register OLT Baru

```bash
curl -X POST http://localhost:8081/api/v1/olt \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "id": "olt_new",
    "name": "OLT New",
    "snmp": {
      "host": "10.5.0.10",
      "port": 161,
      "community": "public"
    }
  }'
```

#### List Semua OLT

```bash
curl http://localhost:8081/api/v1/olts

# Response
{
  "success": true,
  "data": [
    {
      "id": "olt_1",
      "name": "ZTE C320",
      "snmp": {"host":"10.5.0.5","port":161,"community":""},
      "telnet": {"user":"","password":"","port":23}
    }
  ]
}
```

#### Delete OLT

```bash
curl -X DELETE http://localhost:8081/api/v1/olt/olt_new \
  -H "Authorization: Bearer <token>"
```

### ONU Data

#### Get ONU List

```bash
curl http://localhost:8081/api/v1/olt/olt_1/board/1/pon/2

# Response
{
  "success": true,
  "data": [
    {
      "oltId": "olt_1",
      "board": 1,
      "pon": 2,
      "onuId": 1,
      "name": "ONU-0001",
      "serialNumber": "ZTEG12345678",
      "status": "Online",
      "statusCode": 3,
      "rxPower": -18.5
    }
  ]
}
```

**Query Parameters:**

| Param | Type | Description |
|-------|------|-------------|
| `fresh=true` | bool | Skip cache, force on-demand SNMP walk |
| `refresh=true` | bool | Sama seperti `fresh=true` |

#### Get ONU Detail

```bash
curl http://localhost:8081/api/v1/olt/olt_1/board/1/pon/2/onu/1

# Response
{
  "success": true,
  "data": {
    "oltId": "olt_1",
    "board": 1,
    "pon": 2,
    "onuId": 1,
    "name": "ONU-0001",
    "serialNumber": "ZTEG12345678",
    "type": "F660",
    "status": "Online",
    "statusCode": 3,
    "rxPower": -18.5,
    "txPower": 2.3,
    "distanceM": 1250,
    "distanceKm": 1.25,
    "lastOnline": "2024-01-27T10:30:00Z",
    "lastOffline": "2024-01-26T22:15:00Z",
    "offlineReason": "Normal",
    "offlineCode": 0
  }
}
```

### OLT Info & Health

```bash
# OLT Info
curl http://localhost:8081/api/v1/olt/olt_1/info

# Health Check
curl http://localhost:8081/api/v1/olt/olt_1/health
```

## SNMP OID Reference

| Data | OID |
|------|-----|
| Firmware | .1.3.6.1.4.1.3902.1015.2.1.2.2.1.4.1.1.1 |
| PON Description | .1.3.6.1.4.1.3902.1012.3.50.4.1.1.{ifIndex} |
| ONU Name | .1.3.6.1.4.1.3902.1012.3.28.1.1.2.{ifIndex}.{onuId} |
| Serial Number | .1.3.6.1.4.1.3902.1012.3.28.1.1.5.{ifIndex}.{onuId} |
| Type | .1.3.6.1.4.1.3902.1012.3.50.11.2.1.17.{ifIndex}.{onuId} |
| RX Power | .1.3.6.1.4.1.3902.1012.3.50.12.1.1.10.{ifIndex}.{onuId}.1 |
| TX Power | .1.3.6.1.4.1.3902.1012.3.50.12.1.1.9.{ifIndex}.{onuId}.1 |
| Distance | .1.3.6.1.4.1.3902.1012.3.11.4.1.2.{ifIndex}.{onuId} |
| Status | .1.3.6.1.4.1.3902.1012.3.28.2.1.4.{ifIndex}.{onuId} |
| Last Online | .1.3.6.1.4.1.3902.1012.3.28.2.1.5.{ifIndex}.{onuId} |
| Last Offline | .1.3.6.1.4.1.3902.1012.3.28.2.1.6.{ifIndex}.{onuId} |
| Offline Reason | .1.3.6.1.4.1.3902.1012.3.28.2.1.3.{ifIndex}.{onuId} |

### ONU Status Codes

| Value | Arti |
|------:|------|
| 1 | Offline |
| 2 | Ranging |
| 3 | Online |
| 4 | LOS |
| 5 | DyingGasp |
| 6 | PowerOff |
| 7 | Unauthorized |
| 8 | Auto-config |
| 9 | Firmware-upgrade |

**ifIndex Formula**: `(shelf * 2^25) + (slot * 2^16) + (port * 2^8)`

## SNMP Client Tuning

| Parameter | Value | Keterangan |
|-----------|-------|------------|
| MaxRepetitions | 3 | Mengurangi beban OLT, menghindari timeout |
| Timeout | 15s | Cukup untuk walk besar per-PON |
| Retries | 1 | Quick retry, tidak blocking lama |

## Cache Invalidation

- Update OLT config → clear `olt:{oltId}:*`
- Refresh ONU list → clear `olt:{oltId}:board:{board}:pon:{pon}:list`
- Refresh PON list → clear `olt:{oltId}:board:{board}:pon:list`
- `?fresh=true` / `?refresh=true` → skip cache, force on-demand walk

## License

MIT

---

## Docker (Quick Run)

### Build Image
```bash
docker build -t olt-monit .
```

### Run (Without Redis)
```bash
docker run -d --name olt-monit \
  -p 8081:8081 \
  -v $(pwd)/config:/app/config:ro \
  -e TZ=Asia/Jakarta \
  olt-monit
```

### Run (With Redis)
```bash
docker network create olt-net

docker run -d --name olt-redis --network olt-net redis:7-alpine

docker run -d --name olt-monit --network olt-net \
  -p 8081:8081 \
  -v $(pwd)/config:/app/config:ro \
  -e OLT_REDIS_HOST=olt-redis \
  -e OLT_REDIS_PORT=6379 \
  -e TZ=Asia/Jakarta \
  olt-monit
```