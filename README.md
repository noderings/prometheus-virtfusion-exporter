# prometheus-virtfusion-exporter

A [Prometheus](https://prometheus.io/) exporter for [VirtFusion](https://virtfusion.com/). It reads hypervisor, capacity and guest data from the VirtFusion Admin API and exposes it as `vf_*` metrics.

It was built for [NodeRings](https://noderings.com/), but it works with any VirtFusion control panel.

## Features

- Capacity per hypervisor: allocated vs. maximum CPU, memory and disk, so you can track overallocation.
- Live guest metrics (CPU, memory, disk I/O, network, filesystem usage) from VirtFusion `remoteState`.
- Monthly traffic usage and limits per guest.
- Scrapes never hit the API. Background workers refresh an in-memory snapshot, and `/metrics` only serves that cache.
- Ships as a single static binary or a multi-arch (amd64/arm64) container image that runs as a non-root user.

## Quick start

### Docker

```bash
docker run -d -p 9178:9178 \
  -e VIRTFUSION_URL=https://cp.example.com \
  -e VIRTFUSION_TOKEN=<api-token> \
  harbor.noderings.com/nrings/prometheus-virtfusion-exporter:latest
```

### Binary

```bash
make build
./prometheus-virtfusion-exporter \
  --virtfusion-url=https://cp.example.com \
  --virtfusion-token-file=/var/run/secrets/vf/token
```

Create the API token in VirtFusion under **Admin → API**. The exporter only sends `GET` requests.

### Prometheus

```yaml
scrape_configs:
  - job_name: virtfusion
    static_configs:
      - targets: ["exporter-host:9178"]
```

## Configuration

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--listen` | `LISTEN_ADDR` | `:9178` | Listen address |
| `--virtfusion-url` | `VIRTFUSION_URL` | | Control panel URL, without `/api/v1` |
| `--virtfusion-token-file` | | | File holding the API bearer token |
| | `VIRTFUSION_TOKEN` | | API bearer token. `--virtfusion-token-file` takes precedence. |
| `--refresh-inventory` | | `30s` | Inventory refresh interval |
| `--refresh-capacity` | | `60s` | Capacity refresh interval |
| `--refresh-guest` | | `45s` | Guest metrics refresh interval |
| `--refresh-traffic` | | `10m` | Traffic refresh interval |
| `--guest-concurrency` | | `8` | Maximum concurrent `remoteState` requests |
| `--guest-timeout` | | `25s` | Timeout for each guest `remoteState` request |
| `--only-name-prefix` | | `NR-` | Only export per-guest series for servers whose name starts with this prefix (case-insensitive). Set it to `""` to export every guest. |
| `--insecure-skip-tls` | | `false` | Skip TLS verification (testing only) |

## Endpoints

| Path | Description |
|------|-------------|
| `/metrics` | Prometheus metrics, served from the in-memory snapshot |
| `/healthz` | Liveness check |

## How it works

| Worker | Interval | VirtFusion API calls |
|--------|----------|----------------------|
| inventory | 30s | `GET /connect`, `GET /servers?type=simple` (paginated) |
| capacity | 60s | `GET /compute/hypervisors` (paginated), then one `GET /compute/hypervisors/groups/{id}/resources` per group |
| guest | 45s | `GET /servers/{id}?remoteState=true` for each built guest that matches the name prefix |
| traffic | 10m | `GET /servers/{id}/traffic` for the same guests |

Capacity covers every guest on a hypervisor, because VirtFusion already sums the group resources payload. The exporter therefore never fetches `remoteState` for guests outside the name prefix. Suspended and not-yet-built guests are skipped.

## Metrics

### Hypervisor and capacity

| Metric | Labels | Description |
|--------|--------|-------------|
| `vf_up` | | 1 when `GET /connect` succeeds |
| `vf_hypervisor_up` | `hypervisor_id`, `group_id`, `name` | Hypervisor is enabled |
| `vf_cpu_allocated` / `vf_cpu_max` | `group_id`, `hypervisor_id` | vCPUs sold / sellable |
| `vf_memory_allocated_bytes` / `vf_memory_max_bytes` | `group_id`, `hypervisor_id` | Memory sold / sellable |
| `vf_disk_allocated_bytes` / `vf_disk_max_bytes` | `group_id`, `hypervisor_id` | Disk sold / sellable |

### Guests

| Metric | Labels | Description |
|--------|--------|-------------|
| `vf_server_info` | `name`, `server_id`, `uuid`, `hypervisor_id` | Maps a guest to its hypervisor |
| `vf_guest_up` | `name`, `server_id`, `uuid` | 1 when `remoteState` reports live stats |
| `vf_cpu_usage_ratio` | `name`, `server_id`, `uuid` | CPU usage, 0 to 1 |
| `vf_memory_usage_bytes` / `vf_memory_size_bytes` | `name`, `server_id`, `uuid` | Memory used / total |
| `vf_disk_read_bytes` / `vf_disk_write_bytes` | `name`, `server_id`, `uuid` | Disk I/O counters |
| `vf_network_receive_bytes` / `vf_network_transmit_bytes` | `name`, `server_id`, `uuid` | Network counters |
| `vf_fs_used_bytes` / `vf_fs_size_bytes` | `name`, `server_id`, `uuid`, `mount` | Filesystem usage, from the guest agent |
| `vf_traffic_used_bytes` / `vf_traffic_limit_bytes` | `name` | Traffic used / allowed this period |

### Exporter

| Metric | Labels | Description |
|--------|--------|-------------|
| `vf_guest_scrape_seconds` | | Time spent collecting guest metrics (histogram) |
| `vf_remote_state_errors_total` | `reason` | Failed `remoteState` fetches |
| `vf_exporter_last_success_timestamp` | `collector` | Unix time of each worker's last successful run |

## Development

```bash
make test        # go test ./...
make test-race   # go test -race
make lint        # golangci-lint
make build       # static binary
make docker-push IMG=registry.example.com/you/prometheus-virtfusion-exporter TAG=dev
```

Requires Go 1.25+.

## License

Apache License 2.0. See [LICENSE](LICENSE).
