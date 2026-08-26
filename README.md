# prometheus-virtfusion-exporter

Prometheus exporter for VirtFusion hypervisor metrics. Runs on the NodeRings provider agent, reads inventory and live guest state from the VirtFusion Admin API, and exposes `vf_*` series on `:9178/metrics`.

## Architecture

Background workers refresh an in-memory snapshot. The `/metrics` handler **never** calls the VirtFusion API — it only serialises the cached snapshot.

| Worker | Default interval | VF endpoint |
|--------|-----------------|-------------|
| inventory | 30 s | `GET /connect`, `GET /servers?type=simple` (paginated). Emits `vf_server_info` for `NR-` names only. |
| capacity | 60 s | `GET /compute/hypervisors` (paginated), then **one** `GET /compute/hypervisors/groups/{id}/resources` per unique group. Emits per-hypervisor `vf_*_allocated` / `vf_*_max` (all guests on that host). |
| guest | 45 s | `GET /servers/{id}?remoteState=true` for **running NR-** servers only (skipped if last inventory failed) |
| traffic | 10 min | `GET /servers/{id}/traffic` for the same NR- running set |

Guest and traffic workers scrape **only** servers whose name starts with `NR-` (case-insensitive) **and** look running. `vf_server_info` is also NR-only. Non-NR guests stay on the cheap simple list in memory so they can be skipped — they are **not** fetched with `remoteState`.

Overallocation (sold CPU / memory / disk) comes from `GET /compute/hypervisors/groups/{id}/resources`. That payload is already summed per hypervisor for **every** guest (NodeRings and foreign), so the exporter does not `GET /servers/{id}` for non-NR VMs.

## Usage

```bash
prometheus-virtfusion-exporter \
  --virtfusion-url=https://cp.example.com \
  --virtfusion-token-file=/var/run/secrets/vf/token \
  --listen=:9178
```

Or via environment variables:

```bash
export VIRTFUSION_URL=https://cp.example.com
export VIRTFUSION_TOKEN=<token>
prometheus-virtfusion-exporter
```

## Endpoints

| Path | Description |
|------|-------------|
| `/metrics` | Prometheus metrics (reads in-memory snapshot only) |
| `/healthz` | Health check — process up |

## Configuration

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--listen` | `LISTEN_ADDR` | `:9178` | Listen address |
| `--virtfusion-url` | `VIRTFUSION_URL` | | Control panel URL (no `/api/v1`) |
| `--virtfusion-token-file` | | | Path to file containing bearer token |
| | `VIRTFUSION_TOKEN` | | Bearer token (flag file takes precedence) |
| `--refresh-inventory` | | `30s` | Inventory refresh interval |
| `--refresh-capacity` | | `60s` | Capacity refresh interval |
| `--refresh-guest` | | `45s` | Guest metrics refresh interval |
| `--refresh-traffic` | | `10m` | Traffic stats refresh interval |
| `--guest-concurrency` | | `8` | Max concurrent remoteState requests |
| `--guest-timeout` | | `25s` | Per-guest remoteState timeout |
| `--only-name-prefix` | | `NR-` | Only emit guest series / remoteState for names with this prefix (case-insensitive) |
| `--insecure-skip-tls` | | `false` | Skip TLS verification (lab only) |

## Testing

```bash
go test -v ./...
go test -race -count=1 ./...
```

## Build

```bash
make build
make docker-build
```

## Image

`harbor.noderings.com/nrings/prometheus-virtfusion-exporter`
