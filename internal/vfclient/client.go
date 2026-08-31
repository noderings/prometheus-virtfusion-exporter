package vfclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	pageSize   int
}

func New(baseURL, token string, insecureSkipTLS bool) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureSkipTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		pageSize: 200,
	}
}

func (c *Client) apiURL(path string) string {
	return c.baseURL + "/api/v1" + path
}

func (c *Client) paginate(ctx context.Context, path string, extra url.Values) ([]json.RawMessage, error) {
	var all []json.RawMessage
	page := 1
	for {
		q := url.Values{}
		for k, vs := range extra {
			q[k] = vs
		}
		q.Set("results", strconv.Itoa(c.pageSize))
		q.Set("page", strconv.Itoa(page))
		body, err := c.do(ctx, http.MethodGet, path, q)
		if err != nil {
			return nil, err
		}
		var pr PaginatedResponse
		if err := json.Unmarshal(body, &pr); err != nil {
			return nil, fmt.Errorf("decode page %d of %s: %w", page, path, err)
		}
		var items []json.RawMessage
		if len(pr.Data) > 0 && string(pr.Data) != "null" {
			if err := json.Unmarshal(pr.Data, &items); err != nil {
				return nil, fmt.Errorf("decode data page %d of %s: %w", page, path, err)
			}
		}
		all = append(all, items...)
		last := pr.Meta.LastPage
		if last <= 0 || page >= last {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	u := c.apiURL(path)
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d from %s %s: %s", resp.StatusCode, method, path, truncate(body, 256))
	}
	return body, nil
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

// Connect checks API connectivity: GET /connect.
func (c *Client) Connect(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/connect", nil)
	return err
}

type PaginatedResponse struct {
	Data json.RawMessage `json:"data"`
	Meta struct {
		CurrentPage int `json:"current_page"`
		LastPage    int `json:"last_page"`
	} `json:"meta"`
}

// ListServers returns all servers (paginated, type=simple). Cheap identity
// listing — no CPU/disk fields and no libvirt remoteState.
func (c *Client) ListServers(ctx context.Context) ([]json.RawMessage, error) {
	return c.paginate(ctx, "/servers", url.Values{"type": {"simple"}})
}

// GetServerRemoteState returns GET /servers/{id}?remoteState=true.
func (c *Client) GetServerRemoteState(ctx context.Context, serverID int) (json.RawMessage, error) {
	path := fmt.Sprintf("/servers/%d", serverID)
	q := url.Values{"remoteState": {"true"}}
	body, err := c.do(ctx, http.MethodGet, path, q)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode server %d: %w", serverID, err)
	}
	return envelope.Data, nil
}

type Hypervisor struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	GroupID int    `json:"group_id"`
	Enabled bool   `json:"enabled"`
	Group   *struct {
		ID int `json:"id"`
	} `json:"group"`
}

func (h Hypervisor) ResolvedGroupID() int {
	if h.GroupID != 0 {
		return h.GroupID
	}
	if h.Group != nil {
		return h.Group.ID
	}
	return 0
}

// ListHypervisors returns GET /compute/hypervisors (paginated).
func (c *Client) ListHypervisors(ctx context.Context) ([]Hypervisor, error) {
	raw, err := c.paginate(ctx, "/compute/hypervisors", nil)
	if err != nil {
		return nil, err
	}
	out := make([]Hypervisor, 0, len(raw))
	for _, item := range raw {
		var h Hypervisor
		if err := json.Unmarshal(item, &h); err != nil {
			return nil, fmt.Errorf("decode hypervisor: %w", err)
		}
		out = append(out, h)
	}
	return out, nil
}

type ResourcePool struct {
	Allocated float64 `json:"allocated"`
	Max       float64 `json:"max"`
	Units     string  `json:"units"`
}

type GroupResources struct {
	CPUCores     ResourcePool   `json:"cpuCores"`
	Memory       ResourcePool   `json:"memory"`
	LocalStorage ResourcePool   `json:"localStorage"`
	OtherStorage []ResourcePool `json:"otherStorage"`
}

// DiskAllocated is local storage plus other datastores (all guests on the host).
func (r GroupResources) DiskAllocated() float64 {
	n := r.LocalStorage.Allocated
	for _, o := range r.OtherStorage {
		n += o.Allocated
	}
	return n
}

func (r GroupResources) DiskMax() float64 {
	n := r.LocalStorage.Max
	for _, o := range r.OtherStorage {
		n += o.Max
	}
	return n
}

// HypervisorResources is one host row from GET …/groups/{id}/resources.
// allocated/max already include every guest on that hypervisor (NR and not).
type HypervisorResources struct {
	Hypervisor struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		Enabled      bool   `json:"enabled"`
		Commissioned bool   `json:"commissioned"`
	} `json:"hypervisor"`
	Resources GroupResources `json:"resources"`
}

// GetGroupResources returns per-hypervisor allocated/max for the group
// (paginated). This is the cheap overallocation source — VF already summed
// every VM on each host. Do not GET /servers/{id} for non-NR guests.
func (c *Client) GetGroupResources(ctx context.Context, groupID int) ([]HypervisorResources, error) {
	path := fmt.Sprintf("/compute/hypervisors/groups/%d/resources", groupID)
	raw, err := c.paginate(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	out := make([]HypervisorResources, 0, len(raw))
	for _, item := range raw {
		var row HypervisorResources
		if err := json.Unmarshal(item, &row); err != nil {
			return nil, fmt.Errorf("decode group %d resources: %w", groupID, err)
		}
		out = append(out, row)
	}
	return out, nil
}

type TrafficMonthly struct {
	RX    float64 `json:"rx"`
	TX    float64 `json:"tx"`
	Total float64 `json:"total"`
	Limit float64 `json:"limit"`
}

// GetServerTraffic returns GET /servers/{id}/traffic.
func (c *Client) GetServerTraffic(ctx context.Context, serverID int) (*TrafficMonthly, error) {
	path := fmt.Sprintf("/servers/%d/traffic", serverID)
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data struct {
			Monthly []TrafficMonthly `json:"monthly"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode traffic server %d: %w", serverID, err)
	}
	if len(envelope.Data.Monthly) == 0 {
		return &TrafficMonthly{}, nil
	}
	return &envelope.Data.Monthly[0], nil
}
