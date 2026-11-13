# jiankong-dingtalk

把一台 Linux 服务器的状态做成日报，通过钉钉机器人推送到群里。单文件 Go 程序、无常驻进程，配合 systemd 定时器每天定时发送一次。

推送内容：主机名、内网 IP、CPU / 内存 / 磁盘使用率（可设阈值，超限显示 ⚠️）、网络流量、系统版本与虚拟化类型、已运行时长。

## 配置（环境变量）

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `DING_WEBHOOK` | 是 | - | 钉钉机器人 Webhook 地址 |
| `DING_SECRET` | 否 | - | 机器人加签密钥（SEC 开头） |
| `CUSTOM_TITLE` | 否 | 服务器状态日报 | 推送标题 |
| `CPU_THRESHOLD` | 否 | 80 | CPU 使用率告警阈值（%） |
| `MEM_THRESHOLD` | 否 | 80 | 内存使用率告警阈值（%） |
| `DISK_THRESHOLD` | 否 | 80 | 磁盘使用率告警阈值（%） |
| `REPORT_TIME` | 否 | - | 计划推送时刻，仅在日报中展示，实际调度由 systemd 定时器决定 |

## 使用

### 进项目根目录
```
cd jiankong-dingtalk
```
### 拉依赖
```
go mod tidy
```
### 编译
```
CGO_ENABLED=0 go build -ldflags "-s -w" -o jiankong-dingtalk .
```
### 本地运行
```
export DING_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=8dcbbd554a6yyyyyyyyyyyy51d5967exxxxxxxxxxxx7d38e27bea14
export DING_SECRET=SEC9fc1a5ef162dd30bexxxxxxxx759eda5a93d7xxxxxxxxxb4eb81d0bb5
./jiankong-dingtalk
```

### 压测方式
```
sudo apt-get install stress
# 4 核全部跑满 100 秒，Ctrl+C 随时停止
stress --cpu 4 --timeout 100s
# 申请 8 GB 内存 100 秒后自动释放
stress --vm 1 --vm-bytes 8G --timeout 100s
./jiankong-dingtalk
```
### systemd管理
#### 二进制放置
```
sudo cp jiankong-dingtalk /usr/local/bin/
sudo chmod +x /usr/local/bin/jiankong-dingtalk
```

#### systemd 单元文件
```
sudo tee /etc/systemd/system/jiankong-dingtalk.service >/dev/null <<'EOF'
[Unit]
Description=Push server status to DingTalk once
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
# 关键环境变量
Environment=DING_WEBHOOK=https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN
Environment=DING_SECRET=YOUR_SECRET
Environment=CPU_THRESHOLD=85
Environment=MEM_THRESHOLD=85
Environment=DISK_THRESHOLD=85
Environment=REPORT_TIME=09:30
# 执行路径
ExecStart=/usr/local/bin/jiankong-dingtalk 
EOF
```
#### systemd 定时器
```
sudo tee /etc/systemd/system/jiankong-dingtalk.timer  >/dev/null <<'EOF'
[Unit]
Description=Daily jiankong to DingTalk

[Timer]
OnCalendar=*-*-* 09:30:00
Persistent=true

[Install]
WantedBy=timers.target
EOF
```

### 启用并立即测试
```
sudo systemctl daemon-reload
sudo systemctl enable --now jiankong-dingtalk.timer
# 手动验证
sudo systemctl start jiankong-dingtalk.service
journalctl -u jiankong-dingtalk --no-pager -e
```

## 效果图
![66a31723aa9be92737707015b54644e2.jpeg](https://i.mji.rip/2025/07/16/66a31723aa9be92737707015b54644e2.jpeg)

## 更新记录

### v1.1.0 (2026-11-13)
- 依赖升级：gopsutil v3 → v4.26、caarlos0/env v9 → v11，go.mod 提升到 Go 1.26
- 修复：钉钉返回 HTTP 200 但 errcode 非 0（被机器人拒绝）时旧版会误报推送成功，现在会正确报错
- 健壮性：推送请求增加 10 秒超时；内存/磁盘/网络指标采集失败时不再 panic，缺失项显示为 -
- 新增：单元测试（human / icon / signDing）、.gitignore
- 程序启动日志打印版本号（构建时用 `-ldflags "-X main.version=v1.1.0"` 注入）

### v1.0.0 (2025-07-16)
- 首个版本：服务器状态日报推送到钉钉（CPU / 内存 / 磁盘 / 网络 / 主机信息）
