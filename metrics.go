package main

// Prometheus 指标模式:`<binary> metrics` 常驻运行,HTTP 暴露 /metrics,
// 每次抓取实时采集主机指标。监听地址由环境变量 METRICS_ADDR 指定
// (缺省 :9101),可选 METRICS_TOKEN 做 Bearer 鉴权。
// 指标名在三个 jiankong 仓库保持一致,共用同一个 Grafana 看板。

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// metric 是一条待导出的指标样本。
type metric struct {
	name   string
	help   string
	typ    string // gauge / counter
	labels map[string]string
	value  float64
}

// collectMetrics 采集全部指标;单个采集器失败只跳过对应指标,不整体报错
// (单个指标缺失在 Prometheus 里比整个抓取失败更好处理)。
func collectMetrics() []metric {
	var out []metric
	add := func(name, help, typ string, labels map[string]string, v float64) {
		out = append(out, metric{name, help, typ, labels, v})
	}

	add("jiankong_build_info", "构建信息,值为 1", "gauge",
		map[string]string{"version": version}, 1)

	if hi, err := host.Info(); err == nil && hi != nil {
		add("jiankong_host_info", "主机信息,值为 1", "gauge", map[string]string{
			"hostname": hi.Hostname, "os": hi.OS, "platform": hi.Platform,
			"virtualization": hi.VirtualizationSystem,
		}, 1)
		add("jiankong_uptime_seconds", "已运行秒数", "gauge", nil, float64(hi.Uptime))
		add("jiankong_boot_time_seconds", "开机时间(Unix 秒)", "gauge", nil, float64(hi.BootTime))
	}

	if pcts, err := cpu.Percent(300*time.Millisecond, false); err == nil && len(pcts) > 0 {
		add("jiankong_cpu_usage_percent", "CPU 使用率(%)", "gauge", nil, pcts[0])
	}

	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		add("jiankong_memory_used_bytes", "已用内存(字节)", "gauge", nil, float64(vm.Used))
		add("jiankong_memory_total_bytes", "总内存(字节)", "gauge", nil, float64(vm.Total))
		add("jiankong_memory_usage_percent", "内存使用率(%)", "gauge", nil, vm.UsedPercent)
	}
	if sm, err := mem.SwapMemory(); err == nil && sm != nil && sm.Total > 0 {
		add("jiankong_swap_used_bytes", "已用交换空间(字节)", "gauge", nil, float64(sm.Used))
		add("jiankong_swap_total_bytes", "总交换空间(字节)", "gauge", nil, float64(sm.Total))
	}

	// 磁盘:每个物理分区的挂载点各一组指标。
	seen := map[string]bool{}
	if parts, err := disk.Partitions(false); err == nil {
		for _, p := range parts {
			if p.Mountpoint == "" || seen[p.Mountpoint] {
				continue
			}
			seen[p.Mountpoint] = true
			u, err := disk.Usage(p.Mountpoint)
			if err != nil || u == nil || u.Total == 0 {
				continue
			}
			lbl := map[string]string{"mountpoint": p.Mountpoint}
			add("jiankong_disk_used_bytes", "磁盘已用(字节)", "gauge", lbl, float64(u.Used))
			add("jiankong_disk_total_bytes", "磁盘总量(字节)", "gauge", lbl, float64(u.Total))
			add("jiankong_disk_usage_percent", "磁盘使用率(%)", "gauge", lbl, u.UsedPercent)
		}
	}

	// 网络:按网卡给出累计收发字节(计数器,Prometheus 侧用 rate() 算速率)。
	if ios, err := net.IOCounters(true); err == nil {
		for _, io := range ios {
			lbl := map[string]string{"device": io.Name}
			add("jiankong_network_receive_bytes_total", "累计接收字节", "counter", lbl, float64(io.BytesRecv))
			add("jiankong_network_transmit_bytes_total", "累计发送字节", "counter", lbl, float64(io.BytesSent))
		}
	}

	if la, err := load.Avg(); err == nil && la != nil {
		add("jiankong_load1", "1 分钟平均负载", "gauge", nil, la.Load1)
		add("jiankong_load5", "5 分钟平均负载", "gauge", nil, la.Load5)
		add("jiankong_load15", "15 分钟平均负载", "gauge", nil, la.Load15)
	}

	return out
}

// renderPrometheus 把样本渲染成 Prometheus 文本输出格式。
// 同名指标只输出一次 HELP/TYPE,样本顺序稳定(按名+标签排序)。
func renderPrometheus(ms []metric) string {
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].name != ms[j].name {
			return ms[i].name < ms[j].name
		}
		return labelString(ms[i].labels) < labelString(ms[j].labels)
	})
	var b strings.Builder
	seen := map[string]bool{}
	for _, m := range ms {
		if !seen[m.name] {
			seen[m.name] = true
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", m.name, m.help, m.name, m.typ)
		}
		fmt.Fprintf(&b, "%s%s %s\n", m.name, labelString(m.labels), formatFloat(m.value))
	}
	return b.String()
}

func labelString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(labels[k])
		parts = append(parts, fmt.Sprintf("%s=%q", k, v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}

// metricsHandler 返回 /metrics 处理函数;token 非空时要求 Bearer 鉴权。
func metricsHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		start := time.Now()
		body := renderPrometheus(collectMetrics())
		body += fmt.Sprintf("# HELP jiankong_scrape_duration_seconds 本次采集耗时(秒)\n# TYPE jiankong_scrape_duration_seconds gauge\njiankong_scrape_duration_seconds %s\n",
			formatFloat(time.Since(start).Seconds()))
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

// serveMetrics 启动指标 HTTP 服务(阻塞)。
func serveMetrics() {
	addr := os.Getenv("METRICS_ADDR")
	if addr == "" {
		addr = ":9101"
	}
	token := os.Getenv("METRICS_TOKEN")
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", metricsHandler(token))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("指标模式启动: http://%s/metrics (鉴权: %s)", addr, map[bool]string{true: "开", false: "关"}[token != ""])
	log.Fatal(srv.ListenAndServe())
}
