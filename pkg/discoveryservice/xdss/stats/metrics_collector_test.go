package stats

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	kv "github.com/patrickmn/go-cache"
	prometheus_testutil "github.com/prometheus/client_golang/prometheus/testutil"
)

func TestStats_Collect(t *testing.T) {
	tests := []struct {
		name       string
		cacheItems map[string]kv.Item
		ts         time.Time
		want       io.Reader
	}{
		{
			name: "Exposes info metrics",
			cacheItems: map[string]kv.Item{
				"node:endpoint:1:pod-xxxx:info": {Object: int64(104), Expiration: int64(defaultExpiration)},
				"node:endpoint:2:pod-xxxx:info": {Object: int64(200), Expiration: int64(defaultExpiration)},
				"node:endpoint:3:pod-xxxx:info": {Object: int64(500), Expiration: int64(defaultExpiration)},
				"node:cluster:1:pod-xxxx:info":  {Object: int64(300), Expiration: int64(defaultExpiration)},
				"node:cluster:2:pod-xxxx:info":  {Object: int64(400), Expiration: int64(defaultExpiration)},
			},
			ts: time.UnixMilli(100),
			want: strings.NewReader(heredoc.Doc(`
				# HELP marin3r_xdss_discovery_info Information about the version a certain resource type is at
				# TYPE marin3r_xdss_discovery_info untyped
				marin3r_xdss_discovery_info{node_id="node",pod_name="pod-xxxx",resource_type="cluster",version="2"} 0
				marin3r_xdss_discovery_info{node_id="node",pod_name="pod-xxxx",resource_type="endpoint",version="3"} 0
			`)),
		},
		{
			name: "Exposes request/ack/nack counters",
			cacheItems: map[string]kv.Item{
				"node:" + "endpoint" + ":*:pod-bbbb:request_counter": {Object: int64(5), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-cccc:request_counter": {Object: int64(1), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-dddd:request_counter": {Object: int64(1), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-aaaa:request_counter": {Object: int64(2), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-aaaa:nack_counter":    {Object: int64(1), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-bbbb:nack_counter":    {Object: int64(10), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-cccc:nack_counter":    {Object: int64(10), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-dddd:nack_counter":    {Object: int64(10), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-aaaa:ack_counter":     {Object: int64(30), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-bbbb:ack_counter":     {Object: int64(23), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-cccc:ack_counter":     {Object: int64(15), Expiration: int64(0)},
				"node:" + "endpoint" + ":*:pod-dddd:ack_counter":     {Object: int64(1), Expiration: int64(0)},
			},
			ts: time.UnixMilli(100),
			want: strings.NewReader(heredoc.Doc(`
				# HELP marin3r_xdss_discovery_ack_total Number of discovery ACK responses
				# TYPE marin3r_xdss_discovery_ack_total counter
				marin3r_xdss_discovery_ack_total{node_id="node",pod_name="pod-aaaa",resource_type="endpoint"} 30
				marin3r_xdss_discovery_ack_total{node_id="node",pod_name="pod-bbbb",resource_type="endpoint"} 23
				marin3r_xdss_discovery_ack_total{node_id="node",pod_name="pod-cccc",resource_type="endpoint"} 15
				marin3r_xdss_discovery_ack_total{node_id="node",pod_name="pod-dddd",resource_type="endpoint"} 1
				# HELP marin3r_xdss_discovery_nack_total Number of discovery NACK responses
				# TYPE marin3r_xdss_discovery_nack_total counter
				marin3r_xdss_discovery_nack_total{node_id="node",pod_name="pod-aaaa",resource_type="endpoint"} 1
				marin3r_xdss_discovery_nack_total{node_id="node",pod_name="pod-bbbb",resource_type="endpoint"} 10
				marin3r_xdss_discovery_nack_total{node_id="node",pod_name="pod-cccc",resource_type="endpoint"} 10
				marin3r_xdss_discovery_nack_total{node_id="node",pod_name="pod-dddd",resource_type="endpoint"} 10
				# HELP marin3r_xdss_discovery_requests_total Number of discovery requests
				# TYPE marin3r_xdss_discovery_requests_total counter
				marin3r_xdss_discovery_requests_total{node_id="node",pod_name="pod-aaaa",resource_type="endpoint"} 2
				marin3r_xdss_discovery_requests_total{node_id="node",pod_name="pod-bbbb",resource_type="endpoint"} 5
				marin3r_xdss_discovery_requests_total{node_id="node",pod_name="pod-cccc",resource_type="endpoint"} 1
				marin3r_xdss_discovery_requests_total{node_id="node",pod_name="pod-dddd",resource_type="endpoint"} 1
			`)),
		},
		{
			name: "Ignores per version stats",
			cacheItems: map[string]kv.Item{
				"node:" + "endpoint" + ":xxxx:pod-xxxx:ack_counter":   {Object: int64(1), Expiration: int64(0)},
				"node:" + "endpoint" + ":xxxx:pod-xxxxc:nack_counter": {Object: int64(13), Expiration: int64(0)},
			},
			ts:   time.UnixMilli(100),
			want: strings.NewReader(""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewWithItems(tt.cacheItems, tt.ts)
			if err := prometheus_testutil.CollectAndCompare(s, tt.want); err != nil {
				t.Error(err)
			}
		})
	}
}
