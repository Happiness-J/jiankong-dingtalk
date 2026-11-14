package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderPrometheus(t *testing.T) {
	out := renderPrometheus([]metric{
		{name: "jiankong_cpu_usage_percent", help: "CPU", typ: "gauge", value: 12.5},
		{name: "jiankong_disk_usage_percent", help: "磁盘", typ: "gauge",
			labels: map[string]string{"mountpoint": "/data"}, value: 66},
		{name: "jiankong_disk_usage_percent", help: "磁盘", typ: "gauge",
			labels: map[string]string{"mountpoint": "/"}, value: 40},
	})
	if strings.Count(out, "# TYPE jiankong_disk_usage_percent") != 1 {
		t.Errorf("同名指标 HELP/TYPE 应只输出一次:\n%s", out)
	}
	if !strings.Contains(out, `jiankong_disk_usage_percent{mountpoint="/"} 40`) {
		t.Errorf("根挂载点样本缺失:\n%s", out)
	}
	if !strings.Contains(out, "jiankong_cpu_usage_percent 12.5") {
		t.Errorf("CPU 样本格式不对:\n%s", out)
	}
}

func TestCollectMetrics(t *testing.T) {
	ms := collectMetrics()
	if len(ms) == 0 {
		t.Fatal("采集结果为空")
	}
	found := false
	for _, m := range ms {
		if m.name == "jiankong_build_info" {
			found = true
		}
	}
	if !found {
		t.Error("缺少 jiankong_build_info")
	}
}

func TestMetricsHandlerAuth(t *testing.T) {
	h := metricsHandler("s3cret")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("无 token 应返回 401,实际 %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("带正确 token 应返回 200,实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "# HELP jiankong_cpu_usage_percent") &&
		!strings.Contains(rec.Body.String(), "# HELP jiankong_memory_total_bytes") {
		t.Error("响应体不像 Prometheus 指标输出")
	}
}
