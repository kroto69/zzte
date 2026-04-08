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
  jwt_secret: "" # disarankan via env OLT_JWT_SECRET

redis:
  host: localhost
  port: 6379
  db: 0

olts:
  olt_xx_01:
    name: "OLT xx"
    host: "10.10.20.2"
    port: 161
    community: "" # gunakan env OLT_OLT_XX_01_SNMP_COMMUNITY
    telnet:
      user: ""     # gunakan env OLT_OLT_XX_01_TELNET_USER
      password: "" # gunakan env OLT_OLT_XX_01_TELNET_PASSWORD
      port: 23
```

> Root project ini sekarang **backend-only**. Frontend statis bawaan sudah dihapus agar source tree fokus ke API dan service layer.

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

Per-OLT secret runtime bisa diinject tanpa menulis credential ke YAML:

| Variable | Description |
|----------|-------------|
| `OLT_<OLT_ID>_SNMP_COMMUNITY` | Override SNMP community untuk OLT tertentu |
| `OLT_<OLT_ID>_TELNET_USER` | Override telnet username untuk OLT tertentu |
| `OLT_<OLT_ID>_TELNET_PASSWORD` | Override telnet password untuk OLT tertentu |

Contoh untuk OLT ID `olt_xx_01`:

```bash
export OLT_OLT_XX_01_SNMP_COMMUNITY="private"
export OLT_OLT_XX_01_TELNET_USER="zte"
export OLT_OLT_XX_01_TELNET_PASSWORD="super-secret"
```

## Strategi Multi-OLT (Anti Tabrakan Data)

### Cache Key Pattern

Setiap key di Redis include OLT ID untuk isolasi data:

```
olt:{oltId}:info                           → OLT metadata (TTL: 5 min)
olt:{oltId}:firmware                       → Firmware version (TTL: 5 min)
olt:{oltId}:board:{board}:pon:{pon}:list   → List ONU (TTL: 60 sec)
olt:{oltId}:onu:{board}:{pon}:{onuId}      → Detail ONU (TTL: 2 min)
olt:{oltId}:health                         → Health status (TTL: 30 sec)
```

### Data Isolation Flow

```
Request: GET /olt/olt_xx_01/board/2/pon/7

[1] Extract oltId = "olt_xx_01"
[2] Get SNMP client dari OLTManager.GetClient("olt_xx_01")
[3] Check cache: olt:olt_xx_01:board:2:pon:7:list
[4] If miss:
    - Calculate ifIndex = (1*2^25) + (2*2^16) + (7*2^8)
    - Query SNMP OID per ONU
    - Save to cache dengan key: olt:olt_xx_01:board:2:pon:7:list
[5] Return JSON
```

### Cache Invalidation

- Update OLT config → clear `olt:{oltId}:*`
- Refresh ONU list → clear `olt:{oltId}:board:{board}:pon:{pon}:list`

## API Examples

### Test Connection

```bash
curl -X POST http://localhost:8081/api/v1/olt/test-connection \
  -H "Content-Type: application/json" \
  -d '{"host":"10.5.0.4","port":161,"community":"public"}'

# Response
{
  "success": true,
  "data": {
    "firmwareVersion": "v1",
    "fullVersion": "V1.2.5P3"
  }
}
```

### Register OLT Baru

```bash
curl -X POST http://localhost:8081/api/v1/olt \
  -H "Content-Type: application/json" \
  -d '{
    "id": "olt_new",
    "name": "OLT New",
    "snmp": {
      "host": "10.5.0.10",
      "port": 161,
      "community": "public"
    }
  }'

# Response (201 Created)
{
  "success": true,
  "data": {
    "id": "olt_new",
    "name": "OLT New",
    "snmp": {
      "host": "10.5.0.10",
      "port": 161,
      "community": "",
      "timeout": 5,
      "retries": 2
    },
    "telnet": {
      "user": "",
      "password": "",
      "port": 23
    }
  }
}
```

### List Semua OLT

```bash
curl http://localhost:8081/api/v1/olts

# Response
{
  "success": true,
  "data": [
    {
      "id": "olt_xx_01",
      "name": "OLT xx 1",
      "snmp": {"host":"10.5.0.4","port":161,"community":""},
      "telnet": {"user":"","password":"","port":23}
    }
  ]
}
```

### Get ONU List

```bash
curl http://localhost:8081/api/v1/olt/olt_xx_01/board/2/pon/7

# Response
{
  "success": true,
  "data": [
    {
      "oltId": "olt_xx_01",
      "board": 2,
      "pon": 7,
      "onuId": 1,
      "name": "ONU-0001",
      "serialNumber": "ZTEG12345678",
      "type": "F660",
      "status": "Online",
      "rxPower": -18.5,
      "txPower": 2.3,
      "distanceM": 1250,
      "distanceKm": 1.25
    }
  ]
}
```

### Get ONU Detail

```bash
curl http://localhost:8081/api/v1/olt/olt_xx_01/board/2/pon/7/onu/1

# Response
{
  "success": true,
  "data": {
    "oltId": "olt_xx_01",
    "board": 2,
    "pon": 7,
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

### Delete OLT

```bash
curl -X DELETE http://localhost:8081/api/v1/olt/olt_new

# Response
{
  "success": true,
  "message": "OLT deleted successfully"
}
```

## SNMP OID Reference

| Data | OID |
|------|-----|
| Firmware | .1.3.6.1.4.1.3902.1015.2.1.2.2.1.4.1.1.1 |
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

### ONU Status Codes (OID: .1.3.6.1.4.1.3902.1012.3.28.2.1.4.{ifIndex}.{onuId})

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

## Configuration

Configuration is loaded from `olt_config.yaml` or environment variables.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OLT_SERVER_PORT` | Port to run server on | `8081` |
| `OLT_SERVER_HOST` | Host to bind to | `0.0.0.0` |
| `OLT_REDIS_HOST` | Redis host | `localhost` |
| `OLT_SEARCH_ENABLED` | Enable Background Indexer | `true` |
| `OLT_SEARCH_INTERVAL` | Sync interval in minutes | `10` |

### Background Search Indexer

The application runs a background process to index ONUs for the search feature.
- **Default**: Enabled, runs every 10 minutes.
- **Control**: You can enable/disable it in `olt_config.yaml`, via Environment Variable, or using the API at runtime.

**Manual Sync:**
POST `/api/v1/search/sync` to trigger an immediate update.

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
