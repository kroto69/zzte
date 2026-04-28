# OLT ZTE C320 Multi-OLT Monitor — Project Memory

## Overview
REST API backend untuk monitoring multiple OLT ZTE C320. SNMP-based data retrieval, auto-detect firmware V1/V2, Redis caching, per-OLT goroutine pollers, telnet provisioning.

## Tech Stack
- Go 1.25 (module: `olt-monitor`)
- Chi Router v5 (HTTP)
- GoSNMP v1.37 (SNMP client)
- Go-Redis v9 (caching)
- Viper (config)
- Zerolog (logging)
- ziutek/telnet (telnet provisioning)
- golang-jwt/j5 (auth)
- golang.org/x/sync/singleflight (dedup SNMP requests)
- golang.org/x/crypto/bcrypt (password hashing)

## Project Structure
```
cmd/main.go              — entrypoint: config→Redis→register OLTs→start pollers→HTTP server→graceful shutdown
config/olt_config.yaml   — YAML config (server, redis, pollers, OLTs, users)
internal/
  cache/redis.go         — RedisCache: TTL constants, key patterns, get/set/invalidate per type
  config/
    config.go            — Config struct, viper defaults, env prefix OLT_
    password.go          — bcrypt hashing, auto-hash plaintext passwords
    olt_runtime.go       — Env var overrides for per-OLT SNMP/telnet credentials
  domain/
    olt.go               — SNMPConfig, TelnetConfig, PONInfo, OLTConfig, OLTInstance
    onu.go               — ONU (full), ONUListItem (light), ONUNameEntry, status/offline constants
    provisioning.go      — ProvisioningRequest, ProvisioningLog, ProvisioningResponse
    search.go            — SearchItem
    errors.go            — Sentinel errors (ErrOLTNotFound, ErrONUNotFound, etc.)
    activity.go          — Activity audit log model
  handler/
    router.go            — Chi router, middleware stack, route wiring
    onu_handler.go       — GET ONU list (30s timeout+504), ONU detail, PON list
    olt_handler.go       — CRUD OLT, test connection
    auth_handler.go      — JWT login
    user_handler.go      — User CRUD
    system_handler.go    — System info endpoints
    search_handler.go    — Search + index management
    control_handler.go   — Poller trigger, cache invalidation
    provisioning_handler.go — Provision ONU via telnet
    activity_handler.go  — Activity log read
    activity_helper.go   — Shared activity logging helper
    response.go          — JSON response helpers (Success, Error, NotFound, etc.)
  service/
    olt_manager.go       — OLTManager singleton: register/unregister, SNMP lock, client mgmt
    onu_service.go       — ONUService: GetONUList (3-level fallback), GetONUDetail (batch GET), GetPONList
    system_service.go    — SystemService: CPU/uptime/memory via SNMP, cached 5min
    telnet_service.go    — TelnetSession: connect, send commands, reboot ONU, fetch VLAN profiles
    provisioning_service.go — ProvisionONU workflow, preview, find next ONU ID
    provisioning_templates.go — 4 templates (zte_v1/v2, huawei_v1/v2), VLAN profile resolution
    indexer_service.go   — Background search index sync, WalkOLT
    activity_service.go  — Redis LPush+LTrim with in-memory fallback
  snmp/
    client.go            — Client: wraps gosnmp, mutex-protected, timeout 15s, retries 1, MaxReps 3
    oids.go              — All OID constants, BuildONUOID, BuildWalkOID
    adapter.go           — FirmwareAdapter: V1/V2 power conversion (raw-15000)/500
    ifindex.go           — CalculateIfIndex/ParseIfIndex: (1<<28)+(slot<<16)+(port<<8)
    parsers.go           — PDU→Go type parsers, ParseSerialNumber (hybrid ASCII+Hex)
  poller/
    optical.go           — OpticalPoller: per-OLT goroutine, discoverPONs→pollPON, semaphore(2), PON list cache 24h
    names.go             — NamesPoller: per-OLT goroutine, 30s startup delay, global interval only, Name+SN cache
  server/server.go       — HTTP server: ReadTimeout 30s, WriteTimeout 30s, IdleTimeout 120s
```

## Cache Architecture
| Key Pattern | TTL | Source |
|---|---|---|
| `olt:{oltId}:info` | 5min | on-demand |
| `olt:{oltId}:firmware` | 5min | on-demand |
| `olt:{oltId}:board:{b}:pon:{p}:list` | adaptive ~74-120s | optical poller / on-demand |
| `olt:{oltId}:board:{b}:pon:{p}:names` | 10h | names poller / on-demand |
| `olt:{oltId}:onu:{b}:{p}:{id}` | adaptive ~2-4min | on-demand |
| `olt:{oltId}:health` | 5min | on-demand |
| `olt:{oltId}:board:{b}:pon:list` | 5min on-demand / 24h from poller | optical poller / on-demand |
| `search:index` | no expiry | indexer |
| `activity:log` | Redis list, max 500 entries | activity service |

Adaptive TTL: `base + (duration × 2)`, max `base × 5`. Longer SNMP walk → longer cache.

## API Routes
### Public
- `GET /health`

### Auth (JWT)
- `POST /api/v1/auth/login`

### Read (JWT required)
- `GET /api/v1/olts` — list all OLTs
- `GET /api/v1/olt/{oltId}` — OLT info
- `GET /api/v1/olt/{oltId}/board/{board}/pon/{pon}` — ONU list (?fresh=true skip cache)
- `GET /api/v1/olt/{oltId}/board/{board}/pon/{pon}/onu/{onuId}` — ONU detail
- `GET /api/v1/olt/{oltId}/board/{board}/pon` — PON list
- `POST /api/v1/onu/reboot` — reboot ONU
- `GET /api/v1/system/olts` — all OLT system info
- `GET /api/v1/system/olt/{oltId}` — single OLT system info
- `GET /api/v1/search` — search ONUs
- `GET /api/v1/search/stats` — search index stats

### Write (JWT required)
- `POST /api/v1/olt/test-connection` — test SNMP connection
- `POST /api/v1/olt` — register OLT
- `PUT /api/v1/olt/{oltId}` — update OLT
- `DELETE /api/v1/olt/{oltId}` — delete OLT
- `POST /api/v1/search/sync` — trigger index sync
- `GET /api/v1/search/config` — get search config
- `POST /api/v1/search/config` — update search config
- `GET /api/v1/provisioning/unconfigured` — list unconfigured ONUs
- `POST /api/v1/provisioning/preview` — preview provisioning
- `POST /api/v1/provisioning/execute` — execute provisioning
- `GET /api/v1/users` — list users
- `POST /api/v1/users` — create user
- `PUT /api/v1/users/{username}` — update user
- `DELETE /api/v1/users/{username}` — delete user
- `GET /api/v1/activity` — activity log

## Poller System
| Poller | Data | Default Interval | Notes |
|---|---|---|---|
| Optical | Status + RX Power per PON | 60s global, OLT PollInterval override | per-OLT goroutine, semaphore(2), 200ms PON delay |
| Names | Name + Serial Number per PON | 8h global (always, no OLT override) | per-OLT goroutine, 30s startup delay after optical |

## SNMP Details
- ifIndex formula: `(1<<28) + (slot<<16) + (port<<8)`
- Power conversion V1/V2: `(raw - 15000) / 500` (dBm)
- Default V1 fallback: `(raw - 10000) / 100`
- Serial number: 8-byte hybrid ASCII+Hex parsing
- Client defaults: timeout 15s, retries 1, MaxRepetitions 3

## 3-Level Fallback (GetONUList)
1. Cache complete (Name filled) → return (<100ms)
2. Cache incomplete + Names cache → merge → return (fast)
3. Cache empty → on-demand SNMP walk + save to list & names cache (7-12s)

## Environment Variables
| Variable | Default | Description |
|---|---|---|
| OLT_SERVER_PORT | 8081 | HTTP port |
| OLT_SERVER_HOST | 0.0.0.0 | Bind host |
| OLT_SERVER_LOG_LEVEL | info | Log level |
| OLT_JWT_SECRET | generated | JWT signing secret |
| OLT_REDIS_HOST | localhost | Redis host |
| OLT_REDIS_PORT | 6379 | Redis port |
| OLT_REDIS_PASSWORD | "" | Redis password |
| OLT_REDIS_DB | 0 | Redis DB |
| OLT_{ID}_SNMP_COMMUNITY | — | Per-OLT SNMP community override |
| OLT_{ID}_TELNET_USER | — | Per-OLT telnet user override |
| OLT_{ID}_TELNET_PASSWORD | — | Per-OLT telnet password override |

## Performance Fixes Applied (Session 2026-04-28)
1. **PON list cache at startup**: optical poller saves `[]PONInfo` per board to Redis (24h TTL) — GET /pon always hits cache
2. **TTLHealth 30s→5min**: system info cache less aggressive refresh
3. **Names poller 30s startup delay**: `time.Sleep(30s)` before first poll — avoids double-hit with optical poller at startup
4. **ONU list handler timeout**: `context.WithTimeout(30s)` + 504 Gateway Timeout on deadline
5. **Names poller interval fix**: always uses global `NamesPoller.Interval` (8h), never `olt.Config.PollInterval`
