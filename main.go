package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	stdnet "net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	gonet "github.com/shirou/gopsutil/v4/net"
)

/* ---------- 版本（构建时 -ldflags "-X main.version=vX.Y.Z" 注入） ---------- */
var version = "dev"

/* ---------- 配置 ---------- */
type Config struct {
	DingWebhook string `env:"DING_WEBHOOK,required"`
	DingSecret  string `env:"DING_SECRET"`
	ReportTime  string `env:"REPORT_TIME" envDefault:"-"`
	CPUAlert    int    `env:"CPU_THRESHOLD" envDefault:"80"`
	MemAlert    int    `env:"MEM_THRESHOLD" envDefault:"80"`
	DiskAlert   int    `env:"DISK_THRESHOLD" envDefault:"80"`
	CustomTitle string `env:"CUSTOM_TITLE" envDefault:"服务器状态日报"`
}

func loadConfig() *Config {
	c := &Config{}
	if err := env.Parse(c); err != nil {
		log.Fatal(err)
	}
	return c
}

/* ---------- 钉钉推送 ---------- */
func sendDingMarkdown(webhook, secret, title, text string) error {
	var finalURL string
	if secret != "" {
		ts := time.Now().UnixMilli()
		finalURL = fmt.Sprintf("%s&timestamp=%d&sign=%s",
			webhook, ts, url.QueryEscape(signDing(secret, ts)))
	} else {
		finalURL = webhook
	}
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  text,
		},
	}
	b, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(finalURL, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("dingtalk http %d: %s", resp.StatusCode, body)
	}
	// 钉钉返回 HTTP 200 不代表推送成功：机器人拒绝（关键词/签名/频率等）时 errcode 非 0
	var ack struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &ack); err == nil && ack.ErrCode != 0 {
		return fmt.Errorf("dingtalk errcode %d: %s", ack.ErrCode, ack.ErrMsg)
	}
	return nil
}

func signDing(secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d\n%s", ts, secret)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

/* ---------- 主机信息 ---------- */
func firstPrivateIPv4() string {
	ifaces, _ := stdnet.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&stdnet.FlagUp == 0 || iface.Flags&stdnet.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*stdnet.IPNet); ok && ipnet.IP.To4() != nil {
				ip := ipnet.IP.To4()
				if ip[0] == 10 ||
					(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
					(ip[0] == 192 && ip[1] == 168) {
					return ip.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

func detectVirtualization() string {
	if path, err := exec.LookPath("systemd-detect-virt"); err == nil {
		if out, err := exec.Command(path).Output(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "none" {
				return v
			}
		}
	}
	if b, _ := os.ReadFile("/proc/1/cgroup"); strings.Contains(string(b), "docker") ||
		strings.Contains(string(b), "lxc") {
		return "container"
	}
	if b, _ := os.ReadFile("/proc/sys/kernel/osrelease"); strings.Contains(strings.ToLower(string(b)), "microsoft") {
		return "wsl"
	}
	if b, _ := os.ReadFile("/sys/class/dmi/id/product_name"); len(b) > 0 {
		name := strings.ToLower(strings.TrimSpace(string(b)))
		switch {
		case strings.Contains(name, "kvm"), strings.Contains(name, "qemu"):
			return "kvm"
		case strings.Contains(name, "vmware"):
			return "vmware"
		case strings.Contains(name, "virtualbox"):
			return "virtualbox"
		case strings.Contains(name, "microsoft corporation"):
			return "hyperv"
		}
	}
	return "physical"
}

func osVersion() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "Unknown"
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(line[len("PRETTY_NAME="):], `"`)
		}
	}
	return "Unknown"
}

/* ---------- 工具 ---------- */
func icon(v float64, threshold int) string {
	if v > float64(threshold) {
		return "⚠️"
	}
	return "✅"
}

func human(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

/* ---------- 主流程 ---------- */
func buildReport(cfg *Config) string {
	v, _ := mem.VirtualMemory()
	if v == nil {
		v = &mem.VirtualMemoryStat{}
	}
	d, _ := disk.Usage("/")
	if d == nil {
		d = &disk.UsageStat{}
	}
	c, _ := cpu.Percent(time.Second, false)
	n, _ := gonet.IOCounters(false)
	h, _ := host.Info()
	var uptimeSecs uint64
	if h != nil {
		uptimeSecs = h.Uptime
	}

	cpuPercent := 0.0
	if len(c) > 0 {
		cpuPercent = c[0]
	}

	hostname, _ := os.Hostname()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 🖥️ %s  \n\n", cfg.CustomTitle))
	sb.WriteString(fmt.Sprintf("- 🏷️ **主机名**: %s  \n", hostname))
	sb.WriteString(fmt.Sprintf("- 🌐 **内网IP**: %s  \n", firstPrivateIPv4()))
	sb.WriteString(fmt.Sprintf("- 🕒 **推送时间**: %s  \n", time.Now().Format("2006-01-02 15:04:05")))
	if cfg.ReportTime != "-" && cfg.ReportTime != "" {
		sb.WriteString(fmt.Sprintf("- ⏰ **计划时刻**: %s  \n", cfg.ReportTime))
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("- %s **CPU**: %.1f%%  \n", icon(cpuPercent, cfg.CPUAlert), cpuPercent))
	sb.WriteString(fmt.Sprintf("- %s **内存**: %s / %s (%.1f%%)  \n",
		icon(v.UsedPercent, cfg.MemAlert), human(v.Used), human(v.Total), v.UsedPercent))
	sb.WriteString(fmt.Sprintf("- %s **磁盘**: %s / %s (%.1f%%)  \n",
		icon(d.UsedPercent, cfg.DiskAlert), human(d.Used), human(d.Total), d.UsedPercent))
	if len(n) > 0 {
		sb.WriteString(fmt.Sprintf("- 📊 **网络**: ↓%.2f GB  ↑%.2f GB  \n",
			float64(n[0].BytesRecv)/1024/1024/1024,
			float64(n[0].BytesSent)/1024/1024/1024))
	} else {
		sb.WriteString("- 📊 **网络**: -  \n")
	}
	sb.WriteString("\n---\n")
	sb.WriteString(fmt.Sprintf("- 🖥️ **系统**: %s (%s)  \n", osVersion(), detectVirtualization()))
	sb.WriteString(fmt.Sprintf("- ⏱️ **运行**: %s  \n",
		(time.Duration(uptimeSecs) * time.Second).Round(time.Second).String()))
	return sb.String()
}

func main() {
	log.SetOutput(os.Stdout)
	log.Printf("jiankong-dingtalk %s 启动", version)
	cfg := loadConfig()
	report := buildReport(cfg)
	if err := sendDingMarkdown(cfg.DingWebhook, cfg.DingSecret, cfg.CustomTitle, report); err != nil {
		log.Fatal(err)
	}
	log.Println("推送完成,正常退出")
}
