package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	Up = prometheus.NewDesc("vf_up", "VirtFusion API reachable via GET /connect", nil, nil)

	HypervisorUp = prometheus.NewDesc("vf_hypervisor_up",
		"Hypervisor enabled and reachable",
		[]string{"hypervisor_id", "group_id", "name"}, nil)

	CPUAllocated = prometheus.NewDesc("vf_cpu_allocated",
		"CPU cores allocated to all guests on this hypervisor (NR and non-NR)",
		[]string{"group_id", "hypervisor_id"}, nil)
	CPUMax = prometheus.NewDesc("vf_cpu_max",
		"Max CPU cores on this hypervisor",
		[]string{"group_id", "hypervisor_id"}, nil)

	MemoryAllocatedBytes = prometheus.NewDesc("vf_memory_allocated_bytes",
		"Memory allocated to all guests on this hypervisor (bytes)",
		[]string{"group_id", "hypervisor_id"}, nil)
	MemoryMaxBytes = prometheus.NewDesc("vf_memory_max_bytes",
		"Max memory on this hypervisor (bytes)",
		[]string{"group_id", "hypervisor_id"}, nil)

	DiskAllocatedBytes = prometheus.NewDesc("vf_disk_allocated_bytes",
		"Disk allocated to all guests on this hypervisor (bytes)",
		[]string{"group_id", "hypervisor_id"}, nil)
	DiskMaxBytes = prometheus.NewDesc("vf_disk_max_bytes",
		"Max disk on this hypervisor (bytes)",
		[]string{"group_id", "hypervisor_id"}, nil)

	ServerInfo = prometheus.NewDesc("vf_server_info",
		"NodeRings VirtFusion server (name prefix NR-)",
		[]string{"name", "server_id", "uuid", "hypervisor_id"}, nil)

	GuestUp = prometheus.NewDesc("vf_guest_up",
		"1 if GET /servers/{id}?remoteState=true returned live guest stats",
		[]string{"name", "server_id", "uuid"}, nil)

	CPUUsageRatio = prometheus.NewDesc("vf_cpu_usage_ratio",
		"CPU usage 0-1",
		[]string{"name", "server_id", "uuid"}, nil)

	MemoryUsageBytes = prometheus.NewDesc("vf_memory_usage_bytes",
		"Guest memory usage (bytes)",
		[]string{"name", "server_id", "uuid"}, nil)
	MemorySizeBytes = prometheus.NewDesc("vf_memory_size_bytes",
		"Guest memory total (bytes)",
		[]string{"name", "server_id", "uuid"}, nil)

	DiskReadBytes = prometheus.NewDesc("vf_disk_read_bytes",
		"Disk read bytes (counter)",
		[]string{"name", "server_id", "uuid"}, nil)
	DiskWriteBytes = prometheus.NewDesc("vf_disk_write_bytes",
		"Disk write bytes (counter)",
		[]string{"name", "server_id", "uuid"}, nil)

	NetworkReceiveBytes = prometheus.NewDesc("vf_network_receive_bytes",
		"Network receive bytes (counter)",
		[]string{"name", "server_id", "uuid"}, nil)
	NetworkTransmitBytes = prometheus.NewDesc("vf_network_transmit_bytes",
		"Network transmit bytes (counter)",
		[]string{"name", "server_id", "uuid"}, nil)

	FSUsedBytes = prometheus.NewDesc("vf_fs_used_bytes",
		"Filesystem used bytes",
		[]string{"name", "server_id", "uuid", "mount"}, nil)
	FSSizeBytes = prometheus.NewDesc("vf_fs_size_bytes",
		"Filesystem total bytes",
		[]string{"name", "server_id", "uuid", "mount"}, nil)

	TrafficUsedBytes = prometheus.NewDesc("vf_traffic_used_bytes",
		"Monthly traffic used (bytes)",
		[]string{"name"}, nil)
	TrafficLimitBytes = prometheus.NewDesc("vf_traffic_limit_bytes",
		"Monthly traffic limit (bytes)",
		[]string{"name"}, nil)

	GuestScrapeSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "vf_guest_scrape_seconds",
		Help:    "Time spent collecting guest remoteState metrics",
		Buckets: prometheus.DefBuckets,
	})

	RemoteStateErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "vf_remote_state_errors_total",
		Help: "Errors fetching remoteState from VirtFusion",
	}, []string{"reason"})

	ExporterLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vf_exporter_last_success_timestamp",
		Help: "Unix timestamp of last successful collection",
	}, []string{"collector"})
)

func init() {
	prometheus.MustRegister(GuestScrapeSeconds)
	prometheus.MustRegister(RemoteStateErrorsTotal)
	prometheus.MustRegister(ExporterLastSuccess)
}
