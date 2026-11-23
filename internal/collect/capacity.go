package collect

import (
	"context"
	"strconv"
	"time"

	"github.com/noderings/prometheus-virtfusion-exporter/internal/metrics"
	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog/log"
)

type CapacityCollector struct {
	client VFClient
}

func NewCapacityCollector(client VFClient) *CapacityCollector {
	return &CapacityCollector{client: client}
}

const (
	mbToBytes = 1024 * 1024
	gbToBytes = 1024 * 1024 * 1024
)

// Collect fetches hypervisor inventory and per-host allocated/max from the
// group resources API. allocated already includes every guest on that host
// (NodeRings and foreign) — the VirtFusion analogue of summing PVE
// cluster.resources without per-VM HTTP. Live host Use is not collected:
// the Global API does not expose hypervisor-total CPU/memory/disk usage.
func (cc *CapacityCollector) Collect(ctx context.Context) ([]*dto.MetricFamily, error) {
	var families []*dto.MetricFamily

	hypervisors, err := cc.client.ListHypervisors(ctx)
	if err != nil {
		log.Error().Err(err).Msg("list hypervisors failed")
		return nil, err
	}

	seenGroups := map[int]bool{}
	upEmitted := map[int]bool{}

	for _, h := range hypervisors {
		gid := h.ResolvedGroupID()
		up := float64(0)
		if h.Enabled {
			up = 1
		}
		families = append(families, gaugeFamily(metrics.HypervisorUp, up,
			strconv.Itoa(h.ID),
			strconv.Itoa(gid),
			h.Name,
		))
		upEmitted[h.ID] = true

		if gid == 0 || seenGroups[gid] {
			continue
		}
		seenGroups[gid] = true

		rows, err := cc.client.GetGroupResources(ctx, gid)
		if err != nil {
			log.Error().Err(err).Int("group_id", gid).Msg("get group resources failed")
			continue
		}

		g := strconv.Itoa(gid)
		for _, row := range rows {
			hid := strconv.Itoa(row.Hypervisor.ID)
			res := row.Resources
			families = append(families,
				gaugeFamily(metrics.CPUAllocated, res.CPUCores.Allocated, g, hid),
				gaugeFamily(metrics.CPUMax, res.CPUCores.Max, g, hid),
				gaugeFamily(metrics.MemoryAllocatedBytes, res.Memory.Allocated*mbToBytes, g, hid),
				gaugeFamily(metrics.MemoryMaxBytes, res.Memory.Max*mbToBytes, g, hid),
				gaugeFamily(metrics.DiskAllocatedBytes, res.DiskAllocated()*gbToBytes, g, hid),
				gaugeFamily(metrics.DiskMaxBytes, res.DiskMax()*gbToBytes, g, hid),
			)
			if !upEmitted[row.Hypervisor.ID] {
				hvUp := float64(0)
				if row.Hypervisor.Enabled {
					hvUp = 1
				}
				families = append(families, gaugeFamily(metrics.HypervisorUp, hvUp,
					hid, g, row.Hypervisor.Name,
				))
			}
		}
	}

	metrics.ExporterLastSuccess.WithLabelValues("capacity").Set(float64(time.Now().Unix()))
	return families, nil
}
