# OLT Monitor API Documentation

Base URL (default): `http://localhost:8081`
API Prefix: `/api/v1`

## Auth & Authorization
Semua endpoint di bawah `/api/v1` **kecuali** `/auth/login` membutuhkan header:

```
Authorization: Bearer <token>
```

> Catatan: `/auth/change-password` sekarang **diproteksi** oleh middleware dan memakai user dari token aktif.

---

## Health Check

```
GET /health
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "status": "ok"
  }
}
```

---

## Authentication

### Login

```
POST /api/v1/auth/login
```

**Request Body:**
```json
{
  "username": "admin",
  "password": "admin"
}
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "token": "<jwt>",
    "username": "admin",
    "role": "superadmin"
  }
}
```

**Response 401:**
```json
{
  "success": false,
  "error": "Invalid credentials"
}
```

---

### Change Password

```
POST /api/v1/auth/change-password
```

**Request Body:**
```json
{
  "oldPassword": "admin",
  "newPassword": "newpass"
}
```

Header `Authorization: Bearer <token>` wajib dikirim.

**Response 200:**
```json
{
  "success": true,
  "message": "Password changed successfully"
}
```

---

## User Management (Superadmin Only)

### List Users

```
GET /api/v1/users
```

**Response 200:**
```json
{
  "success": true,
  "data": [
    {"username": "admin", "role": "superadmin"},
    {"username": "teknisi", "role": "technician"}
  ]
}
```

---

### Create User

```
POST /api/v1/users
```

**Request Body:**
```json
{
  "username": "teknisi2",
  "password": "secret",
  "role": "technician"
}
```

**Response 201:**
```json
{
  "success": true,
  "data": {
    "username": "teknisi2",
    "role": "technician"
  }
}
```

**Response 409:**
```json
{
  "success": false,
  "error": "User already exists"
}
```

---

### Update User

```
PUT /api/v1/users/{username}
```

**Request Body (role and/or password):**
```json
{
  "password": "newpass",
  "role": "technician"
}
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "username": "teknisi2",
    "role": "technician"
  }
}
```

**Notes:**
- Role must be `superadmin` or `technician`.
- Tidak bisa menurunkan atau menghapus **superadmin terakhir**.

---

### Delete User

```
DELETE /api/v1/users/{username}
```

**Response 200:**
```json
{
  "success": true,
  "message": "User deleted successfully"
}
```

**Notes:**
- Tidak bisa menghapus user yang sedang login.
- Tidak bisa menghapus **superadmin terakhir**.

---

## Activity & Audit (Superadmin Only)

### List Activity Logs

```
GET /api/v1/activity?limit=100
```

**Response 200:**
```json
{
  "success": true,
  "data": [
    {
      "id": "100",
      "time": "2026-02-02T11:55:00Z",
      "user": "admin",
      "role": "superadmin",
      "action": "auth.login",
      "target": "admin",
      "detail": {
        "ip": "203.0.113.10"
      },
      "remoteIp": "203.0.113.10"
    },
    {
      "id": "101",
      "time": "2026-02-02T12:00:00Z",
      "user": "admin",
      "role": "superadmin",
      "action": "olt.update",
      "target": "olt_1",
      "detail": {
        "name": "OLT kita",
        "host": "10.5.0.4",
        "port": 161
      },
      "remoteIp": "127.0.0.1"
    }
  ]
}
```

---

## OLT Management

> Semua response OLT sekarang **meredaksi** field sensitif runtime. `snmp.community`, `telnet.user`, dan `telnet.password` tidak dikembalikan lagi oleh API.

### Test Connection

Test koneksi ke OLT dan auto-detect firmware version.

```
POST /api/v1/olt/test-connection
```

**Request Body:**
```json
{
  "host": "10.5.0.4",
  "port": 161,
  "community": "public"
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| host | string | ✓ | - | IP address OLT |
| port | int | | 161 | SNMP port |
| community | string | | "public" | SNMP community |

**Response 200:**
```json
{
  "success": true,
  "data": {
    "firmwareVersion": "v1",
    "fullVersion": "V1.2.5P3"
  }
}
```

**Response 500:**
```json
{
  "success": false,
  "error": "Failed to test OLT connection"
}
```

---

### Register OLT

Register OLT baru.

```
POST /api/v1/olt
```

**Request Body:**
```json
{
  "id": "olt_kita_01",
  "name": "OLT kita 1",
  "snmp": {
    "host": "10.5.0.4",
    "port": 161,
    "community": "public",
    "timeout": 5,
    "retries": 2
  },
  "telnet": {
    "user": "zte",
    "password": "zte",
    "port": 23
  }
}
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| id | string | ✓ | - | Unique identifier |
| name | string | | "" | Display name |
| snmp.host | string | ✓ | - | IP address OLT |
| snmp.port | int | | 161 | SNMP port |
| snmp.community | string | | "public" | SNMP community |
| snmp.timeout | int | | 5 | Timeout in seconds |
| snmp.retries | int | | 2 | Number of retries |
| telnet.user | string | | "" | Telnet username |
| telnet.password | string | | "" | Telnet password |
| telnet.port | int | | 23 | Telnet port |

**Response 201:**
```json
{
  "success": true,
  "data": {
    "id": "olt_kita_01",
    "name": "OLT kita 1",
    "snmp": {
      "host": "10.5.0.4",
      "port": 161,
      "community": "",
      "timeout": 5,
      "retries": 2
    },
    "telnet": {
      "user": "",
      "password": "",
      "port": 23
    },
    "config": {
      "id": "olt_kita_01",
      "name": "OLT kita 1",
      "snmp": {"host":"10.5.0.4","port":161,"community":"","timeout":5,"retries":2},
      "telnet": {"user":"","password":"","port":23},
      "isOnline": false,
      "lastCheck": "0001-01-01T00:00:00Z"
    }
  }
}
```

**Response 409 (Conflict):**
```json
{
  "success": false,
  "error": "OLT with this ID already exists"
}
```

---

### List All OLTs

```
GET /api/v1/olts
```

**Response 200:**
```json
{
  "success": true,
  "data": [
    {
      "id": "olt_kita_01",
      "name": "OLT kita 1",
      "snmp": {"host":"10.5.0.4","port":161,"community":""},
      "telnet": {"user":"","password":"","port":23}
    }
  ]
}
```

---

### Get Single OLT

```
GET /api/v1/olt/{oltId}
```

| Parameter | Type | Description |
|-----------|------|-------------|
| oltId | string | OLT identifier |

**Response 200:**
```json
{
  "success": true,
  "data": {
    "id": "olt_kita_01",
    "name": "OLT kita 1",
    "snmp": {"host":"10.5.0.4","port":161,"community":""},
    "telnet": {"user":"","password":"","port":23}
  }
}
```

**Response 404:**
```json
{
  "success": false,
  "error": "OLT not found"
}
```

---

### Update OLT

```
PUT /api/v1/olt/{oltId}
```

**Request Body:** sama seperti Register OLT (id akan diambil dari URL).

**Response 200:**
```json
{
  "success": true,
  "data": {
    "id": "olt_kita_01",
    "name": "OLT kita 1",
    "snmp": {"host":"10.5.0.4","port":161,"community":""},
    "telnet": {"user":"","password":"","port":23}
  }
}
```

---

### Delete OLT

```
DELETE /api/v1/olt/{oltId}
```

| Parameter | Type | Description |
|-----------|------|-------------|
| oltId | string | OLT identifier |

**Response 200:**
```json
{
  "success": true,
  "message": "OLT deleted successfully"
}
```

**Response 404:**
```json
{
  "success": false,
  "error": "OLT not found"
}
```

---

## ONU Data

### Get ONU List

```
GET /api/v1/olt/{oltId}/board/{board}/pon/{pon}
```

Returns lightweight list of ONUs on a PON port (only essential fields).

**Query Parameters:**

| Param | Type | Description |
|-------|------|-------------|
| `fresh` | bool | Skip cache, force on-demand SNMP walk (`true`/`1`) |
| `refresh` | bool | Same as `fresh` — skip cache and walk SNMP directly |

**URL Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| oltId | string | OLT identifier |
| board | int | Board/slot number (>= 1) |
| pon | int | PON port number (>= 1) |

**Response 200:**
```json
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

**ONUListItem Fields:**

| Field | Type | Description |
|-------|------|-------------|
| oltId | string | OLT identifier |
| board | int | Board/Shelf number |
| pon | int | PON port number |
| onuId | int | ONU index on PON |
| name | string | ONU name (may be empty if not yet cached by Names Poller) |
| serialNumber | string | ONU serial number (may be empty if not yet cached) |
| status | string | Human-readable status |
| statusCode | int | Numeric status code (see Status Codes table) |
| rxPower | float | RX power in dBm |

**Cache Behavior (3-level fallback):**
1. Complete list cache (Name populated) → returns in < 100ms
2. Incomplete list + Names cache → merge, fast return
3. No cache → on-demand SNMP walk (~7-12s), save to both caches

---

### Get ONU Detail

```
GET /api/v1/olt/{oltId}/board/{board}/pon/{pon}/onu/{onuId}
```

Returns complete ONU information with all fields. Uses batch `GetMultiple` for fast retrieval.

**Query Parameters:**

| Param | Type | Description |
|-------|------|-------------|
| `fresh` | bool | Skip cache, force on-demand SNMP GET |
| `refresh` | bool | Same as `fresh` |

| Parameter | Type | Description |
|-----------|------|-------------|
| oltId | string | OLT identifier |
| board | int | Board/slot number |
| pon | int | PON port number |
| onuId | int | ONU ID (>= 1) |

**Response 200:**
```json
{
  "success": true,
  "data": {
    "oltId": "olt_kita_01",
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
    "offlineCode": 0,
    "wanIp": "10.10.10.10"
  }
}
```

**Cache Key:** `olt:{oltId}:onu:{board}:{pon}:{onuId}`
**Cache TTL:** ~2-4 minutes (adaptive: `baseTTL + duration×2`, max `baseTTL×5`)

---

### Get PON List

```
GET /api/v1/olt/{oltId}/board/{board}/pon
```

**Response 200:**
```json
{
  "success": true,
  "data": [
    {
      "board": 2,
      "pon": 1,
      "description": "GPON-1"
    }
  ]
}
```

---

## Control

### Reboot ONU (Telnet)

```
POST /api/v1/onu/reboot
```

**Request Body:**
```json
{
  "olt_id": "olt_kita_01",
  "board": 2,
  "pon": 7,
  "onu_id": 1
}
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "status": "success",
    "message": "Reboot command sent successfully. ONU will reconnect shortly."
  }
}
```

---

## System

### Get System Info (All OLTs)

```
GET /api/v1/system/olts
```

**Response 200:**
```json
{
  "success": true,
  "data": [
    {
      "oltId": "olt_kita_01",
      "name": "OLT kita 1",
      "host": "10.5.0.4",
      "sysDescr": "ZTE C320 ...",
      "sysName": "OLT-kita",
      "uptime": "2d 4h 12m",
      "uptimeTicks": 18792000,
      "cpuUsage": 12,
      "memoryUsage": 34,
      "isOnline": true
    }
  ]
}
```

---

### Get System Info (Single OLT)

```
GET /api/v1/system/olt/{oltId}
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "oltId": "olt_kita_01",
    "name": "OLT kita 1",
    "host": "10.5.0.4",
    "sysDescr": "ZTE C320 ...",
    "sysName": "OLT-kita",
    "uptime": "2d 4h 12m",
    "uptimeTicks": 18792000,
    "cpuUsage": 12,
    "memoryUsage": 34,
    "isOnline": true
  }
}
```

---

## Search

### Search ONU (Global)

```
GET /api/v1/search?q={query}
```

**Notes:** `q` minimal 2 karakter. Maksimal 50 hasil.

**Response 200:**
```json
{
  "success": true,
  "data": [
    {
      "oltId": "olt_kita_01",
      "board": 2,
      "pon": 7,
      "onuId": 1,
      "name": "ONU-0001",
      "serialNumber": "ZTEG12345678",
      "status": "Online"
    }
  ]
}
```

---

### Search Stats

```
GET /api/v1/search/stats
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "total": 1200,
    "online": 980,
    "offline": 180,
    "los": 40
  }
}
```

---

### Force Sync Index

```
POST /api/v1/search/sync
```

**Response 200:**
```json
{
  "success": true,
  "message": "Background sync triggered"
}
```

---

### Get Search Config

```
GET /api/v1/search/config
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "enabled": true
  }
}
```

---

### Update Search Config

```
POST /api/v1/search/config
```

**Request Body:**
```json
{
  "enabled": true
}
```

**Response 200:**
```json
{
  "success": true,
  "message": "Search configuration updated"
}
```

---

## Provisioning

### Get Unconfigured ONUs (Telnet)

```
GET /api/v1/provisioning/unconfigured?olt_id={oltId}
```

**Response 200:**
```json
{
  "success": true,
  "data": [
    {
      "board": 2,
      "pon": 7,
      "onuId": 1,
      "sn": "ZTEG12345678",
      "type": "ZTE",
      "state": "Unconfigured"
    }
  ]
}
```

---

### Execute Provisioning (Telnet)

```
POST /api/v1/provisioning/execute
```

**Request Body (minimal):**
```json
{
  "oltId": "olt_kita_01",
  "board": 2,
  "pon": 7,
  "onuId": 1,
  "sn": "ZTEG12345678",
  "templateId": "zte_v2"
}
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "success": true,
    "message": "Provisioning Successful",
    "logs": [
      {
        "step": "Init",
        "command": "-",
        "status": "info",
        "message": "Starting provisioning...",
        "time": "10:30:00"
      }
    ]
  }
}
```

---

### Preview Provisioning Script

```
POST /api/v1/provisioning/preview
```

**Request Body:** sama dengan execute provisioning.

**Response 200:**
```json
{
  "success": true,
  "data": {
    "commands": [
      "conf t",
      "interface gpon-olt_1/2/7",
      "onu 1 type ZTE-F609 sn ZTEG12345678"
    ],
    "script": "conf t\ninterface gpon-olt_1/2/7\nonu 1 type ZTE-F609 sn ZTEG12345678"
  }
}
```

---

## Cache & Poller Architecture

### Redis Cache Keys

| Cache | Key Pattern | TTL | Refreshed By |
|-------|-------------|-----|---------------|
| ONU List | `olt:{oltId}:board:{board}:pon:{pon}:list` | ~74-120s (adaptive) | API request, Optical Poller |
| Names | `olt:{oltId}:board:{board}:pon:{pon}:names` | 10h | API request, Names Poller |
| Detail | `olt:{oltId}:onu:{board}:{pon}:{onuId}` | ~2-4min (adaptive) | API request |
| OLT Info | `olt:{oltId}:info` | 5min | API request |
| Health | `olt:{oltId}:health` | 30s | API request |
| Search Index | `olt:global:index` | — | Indexer (if enabled) |

### Adaptive TTL

List and Detail caches use `baseTTL + (walk_duration × 2)` with a max of `baseTTL × 5`. Slower SNMP walks get longer TTL to reduce unnecessary re-walks.

### Background Pollers

| Poller | Data | Interval | Connection |
|--------|------|----------|-------------|
| Optical Poller | Status + RX Power per PON | 60s | Separate (`GetNewClient`) |
| Names Poller | Name + Serial Number per PON | 8h | Separate (`GetNewClient`) |

Both pollers:
- Walk per-PON with scoped BulkWalk (not global) to avoid timeouts on large OLTs
- Use separate SNMP connections (`GetNewClient`) to not block API requests
- Optical Poller merges Name/SN from Names cache before writing to list cache
- Names Poller saves to Names cache only

### Cache Invalidation

- Update OLT config → clear `olt:{oltId}:*`
- Delete OLT → clear `olt:{oltId}:*`
- `?fresh=true` / `?refresh=true` → skip cache, force on-demand walk
- ONU Reboot → invalidate specific ONU detail cache

---

## Data Reference

### ONU Status Codes

| Code | Status | Description |
|------|--------|-------------|
| 1 | Offline | ONU offline (umum) |
| 2 | Ranging | Proses ranging/registrasi |
| 3 | Online | ONU online / normal |
| 4 | LOS | Loss of Signal (kabel putus/redaman tinggi) |
| 5 | DyingGasp | ONU mati karena kehilangan daya listrik |
| 6 | PowerOff | ONU mati (power off) |
| 7 | Unauthorized | Gagal otentikasi (SN/Password salah) |
| 8 | AutoConfig | Auto‑configuration berjalan |
| 9 | FirmwareUpgrade | Firmware upgrade berlangsung |

### Offline Reason Codes

| Code | Reason | Description |
|------|--------|-------------|
| 0 | Normal | Shutdown normal |
| 2 | LOS | Loss of Signal |
| 6 | DyingGasp | Power failure |
| 7 | ManualShutdown | Dimatikan manual |

### Power Conversion

Raw SNMP value ke dBm: `(raw - 10000) / 100`

Contoh: raw = 7850 -> (7850 - 10000) / 100 = **-21.5 dBm**

### ifIndex Calculation

```
ifIndex = (shelf * 2^25) + (slot * 2^16) + (port * 2^8)
```

Contoh untuk shelf=1, slot=2, port=7:
```
ifIndex = (1 * 33554432) + (2 * 65536) + (7 * 256)
        = 33554432 + 131072 + 1792
        = 33687296
```

---

## Error Responses

| Status | Description |
|--------|-------------|
| 400 | Bad Request — invalid parameters or missing required fields |
| 401 | Unauthorized — missing or invalid JWT token |
| 403 | Forbidden — insufficient role permissions |
| 404 | Not Found — resource tidak ditemukan |
| 409 | Conflict — resource sudah ada |
| 429 | Too Many Requests — rate limited atau akun terkunci (5 menit setelah 5x gagal login) |
| 500 | Internal Server Error — SNMP/Telnet/Redis failure |

**Error Format (umum):**
```json
{
  "success": false,
  "error": "Error message here"
}
```
