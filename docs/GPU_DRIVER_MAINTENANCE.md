# GPU 驱动维护指南

## 问题原因

### NVIDIA 驱动版本不匹配的根本原因

NVIDIA 驱动由两部分组成：

1. **内核模块**（Kernel Module）
   - 位置：`/lib/modules/$(uname -r)/kernel/drivers/video/`
   - 文件：`nvidia.ko`, `nvidia-uvm.ko`, `nvidia-modeset.ko` 等
   - 特点：加载到内核内存后持续运行，直到系统重启

2. **用户空间库**（User-space Libraries）
   - 位置：`/usr/lib/x86_64-linux-gnu/` 或 `/usr/lib64/`
   - 文件：`libcuda.so`, `libnvidia-ml.so`, `libOpenCL.so` 等
   - 特点：每次应用程序启动时动态加载

### 问题发生场景

```
场景 1: 系统自动更新
├─ apt/yum 自动更新了 NVIDIA 驱动包
├─ 用户空间库被替换为新版本（如 580.126）
├─ 内核模块仍是旧版本在运行（如 580.100）
└─ 版本不匹配！

场景 2: 手动安装驱动但未重启
├─ 手动安装了新版本驱动
├─ 安装程序更新了库文件
├─ 但没有重启系统
└─ 版本不匹配！

场景 3: 内核更新
├─ 系统内核更新
├─ 旧内核的驱动模块不兼容新内核
└─ 驱动加载失败！
```

### 为什么重启能解决？

重启系统时：
1. 内核重新初始化
2. 加载最新版本的驱动模块（从 `/lib/modules/` 读取）
3. 用户空间库和内核模块版本一致
4. 问题解决

## 预防措施

### 1. 禁用驱动自动更新

#### Ubuntu/Debian

```bash
# 方法 1: 锁定 NVIDIA 驱动包版本
sudo apt-mark hold nvidia-driver-* nvidia-dkms-*

# 查看已锁定的包
apt-mark showhold

# 需要更新时解锁
sudo apt-mark unhold nvidia-driver-* nvidia-dkms-*
```

#### RHEL/CentOS

```bash
# 方法 1: 在 yum.conf 中排除 NVIDIA 包
sudo vi /etc/yum.conf
# 添加: exclude=nvidia-* kmod-nvidia-*

# 方法 2: 使用 versionlock 插件
sudo yum install yum-plugin-versionlock
sudo yum versionlock nvidia-*
```

### 2. 设置更新策略

创建 `/etc/apt/apt.conf.d/99-nvidia-hold`：

```bash
# 阻止 NVIDIA 驱动自动更新
APT::Get::AllowUnauthenticated "false";
Unattended-Upgrade::Package-Blacklist {
    "nvidia-driver-*";
    "nvidia-dkms-*";
    "nvidia-kernel-*";
};
```

### 3. 使用 GPU 监控服务

#### 安装监控脚本

```bash
# 复制监控脚本
sudo cp scripts/monitor_gpu.sh /usr/local/bin/

# 创建 systemd 服务
sudo tee /etc/systemd/system/gpu-monitor.service > /dev/null <<'EOF'
[Unit]
Description=GPU Driver Status Monitor
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/monitor_gpu.sh monitor
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

# 启用并启动服务
sudo systemctl daemon-reload
sudo systemctl enable gpu-monitor.service
sudo systemctl start gpu-monitor.service

# 查看状态
sudo systemctl status gpu-monitor.service

# 查看日志
sudo journalctl -u gpu-monitor.service -f
```

### 4. 定期健康检查

添加到 crontab：

```bash
# 每小时检查一次 GPU 状态
0 * * * * /usr/local/bin/monitor_gpu.sh once >> /var/log/gpu_check.log 2>&1

# 每天早上 8 点发送状态报告
0 8 * * * /usr/local/bin/monitor_gpu.sh once && echo "GPU 状态正常" | mail -s "GPU Daily Check" admin@example.com
```

### 5. 驱动更新最佳实践

#### 更新前检查清单

```bash
# 1. 检查当前驱动版本
nvidia-smi

# 2. 检查正在运行的 GPU 进程
nvidia-smi pgrep

# 3. 备份当前配置
sudo cp -r /etc/X11/xorg.conf.d /etc/X11/xorg.conf.d.backup

# 4. 记录当前版本
nvidia-smi --query-gpu=driver_version --format=csv,noheader > /tmp/nvidia_version_before.txt
```

#### 更新流程

```bash
# 1. 停止使用 GPU 的服务
sudo systemctl stop trap-factory-worker

# 2. 更新驱动
sudo apt update
sudo apt install nvidia-driver-XXX

# 3. 立即重启（重要！）
sudo reboot

# 4. 重启后验证
nvidia-smi
clinfo | grep -i "device name"

# 5. 恢复服务
sudo systemctl start trap-factory-worker
```

## 应急处理

### 快速检查脚本

```bash
# 使用我们提供的检查脚本
./scripts/check_opencl.sh
```

### 手动修复步骤

#### 方案 1: 重启系统（推荐）

```bash
sudo reboot
```

#### 方案 2: 重新加载驱动模块

```bash
# 1. 停止所有 GPU 进程
sudo systemctl stop trap-factory-worker
# 或手动 kill 进程

# 2. 卸载驱动模块
sudo rmmod nvidia_uvm
sudo rmmod nvidia_drm
sudo rmmod nvidia_modeset
sudo rmmod nvidia

# 3. 重新加载
sudo modprobe nvidia
sudo modprobe nvidia_modeset
sudo modprobe nvidia_drm
sudo modprobe nvidia_uvm

# 4. 验证
nvidia-smi
```

#### 方案 3: 重新安装驱动

```bash
# Ubuntu/Debian
sudo apt-get install --reinstall nvidia-driver-*

# RHEL/CentOS
sudo yum reinstall kmod-nvidia-*

# 重启
sudo reboot
```

## 监控和告警

### 检查告警标记

```bash
# 如果存在此文件，说明 GPU 有问题
if [ -f /tmp/gpu_alert.flag ]; then
    cat /tmp/gpu_alert.flag
fi
```

### 集成到 trap_factory

在 `main.go` 中添加启动前检查：

```go
// 启动前检查 GPU 状态
func checkGPUStatus() error {
    cmd := exec.Command("/usr/local/bin/monitor_gpu.sh", "once")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("GPU 状态检查失败: %s", output)
    }

    // 检查告警标记
    if _, err := os.Stat("/tmp/gpu_alert.flag"); err == nil {
        return fmt.Errorf("GPU 存在告警，请检查")
    }

    return nil
}
```

## 长期稳定性建议

### 1. 使用 LTS 驱动版本

选择长期支持版本，避免频繁更新：
- NVIDIA 官方推荐的生产环境版本
- 经过充分测试的稳定版本

### 2. 计划性维护窗口

- 每月固定时间进行系统维护
- 在低峰期更新驱动
- 更新后立即重启验证

### 3. 文档化运维流程

- 记录每次驱动更新的时间和版本
- 记录遇到的问题和解决方案
- 建立标准操作程序（SOP）

### 4. 监控指标

需要监控的关键指标：
- GPU 驱动版本
- GPU 温度和使用率
- OpenCL 设备可用性
- 驱动错误日志

## 常见问题

### Q: 为什么不能自动重新加载驱动？

A: 因为：
1. 卸载驱动模块需要停止所有 GPU 进程
2. 可能影响正在运行的任务
3. 重新加载可能失败，导致 GPU 完全不可用
4. 重启是最安全可靠的方式

### Q: 可以在不重启的情况下更新驱动吗？

A: 理论上可以，但不推荐：
1. 需要停止所有 GPU 进程
2. 需要卸载并重新加载内核模块
3. 可能导致系统不稳定
4. 生产环境建议计划性重启

### Q: 如何选择合适的驱动版本？

A: 建议：
1. 查看 NVIDIA 官方推荐的生产环境版本
2. 选择 LTS（长期支持）版本
3. 避免使用 beta 或最新版本
4. 参考社区反馈和稳定性报告

## 相关脚本

- `scripts/check_opencl.sh` - OpenCL 设备检测和修复
- `scripts/monitor_gpu.sh` - GPU 状态监控
