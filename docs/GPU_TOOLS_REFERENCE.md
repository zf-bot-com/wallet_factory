# GPU 工具快速参考

## 可用脚本

### 1. check_opencl.sh - OpenCL 设备检测和修复

**位置**：`scripts/check_opencl.sh` 或 `/usr/local/bin/check_opencl.sh`（部署后）

**功能**：
- 检测 OpenCL 设备是否可用
- 诊断 GPU 驱动问题
- 自动修复常见问题（驱动版本不匹配、权限问题等）

**使用方法**：

```bash
# 本地运行
./scripts/check_opencl.sh

# 远程服务器运行
ssh gpu '/usr/local/bin/check_opencl.sh'
```

**适用场景**：
- GPU 设备检测不到
- nvidia-smi 报错
- OpenCL 设备不可用
- 驱动版本不匹配

### 2. monitor_gpu.sh - GPU 状态监控

**位置**：`scripts/monitor_gpu.sh` 或 `/usr/local/bin/monitor_gpu.sh`（部署后）

**功能**：
- 定期检查 GPU 驱动状态
- 检测驱动版本不匹配
- 检测 OpenCL 设备可用性
- 发现问题时创建告警标记

**使用方法**：

```bash
# 执行一次检查
./scripts/monitor_gpu.sh once

# 持续监控模式（每 5 分钟检查一次）
./scripts/monitor_gpu.sh monitor

# 远程服务器检查
ssh gpu '/usr/local/bin/monitor_gpu.sh once'
```

**适用场景**：
- 定期健康检查
- 持续监控 GPU 状态
- 集成到 CI/CD 流程

### 3. gpu-monitor.service - GPU 监控系统服务

**位置**：`scripts/gpu-monitor.service` 或 `/etc/systemd/system/gpu-monitor.service`（部署后）

**功能**：
- 作为系统服务持续运行
- 自动监控 GPU 状态
- 开机自启动

**使用方法**：

```bash
# 查看服务状态
sudo systemctl status gpu-monitor.service

# 启动服务
sudo systemctl start gpu-monitor.service

# 停止服务
sudo systemctl stop gpu-monitor.service

# 重启服务
sudo systemctl restart gpu-monitor.service

# 查看日志
sudo journalctl -u gpu-monitor.service -f

# 远程服务器
ssh gpu 'sudo systemctl status gpu-monitor.service'
```

## 部署集成

### 自动部署配置

使用 `deploy.sh` 脚本部署时，会自动询问是否配置 GPU 驱动预防措施：

```bash
./deploy.sh prod
```

部署过程会：
1. 检查所有服务器的 GPU 环境
2. 上传程序文件
3. 询问是否配置 GPU 驱动预防措施（推荐选择 y）
4. 询问是否配置 supervisor 服务

### 手动配置

如果需要在已部署的服务器上手动配置：

```bash
# 1. 上传脚本
scp scripts/check_opencl.sh scripts/monitor_gpu.sh scripts/gpu-monitor.service gpu:/tmp/

# 2. 在服务器上安装
ssh gpu << 'EOF'
sudo cp /tmp/check_opencl.sh /usr/local/bin/
sudo cp /tmp/monitor_gpu.sh /usr/local/bin/
sudo chmod +x /usr/local/bin/check_opencl.sh
sudo chmod +x /usr/local/bin/monitor_gpu.sh

# 锁定 NVIDIA 驱动版本
sudo apt-mark hold nvidia-driver-* nvidia-dkms-*

# 配置监控服务
sudo cp /tmp/gpu-monitor.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable gpu-monitor.service
sudo systemctl start gpu-monitor.service

# 配置定期检查
(crontab -l 2>/dev/null; echo "0 * * * * /usr/local/bin/monitor_gpu.sh once >> /var/log/gpu_check.log 2>&1") | crontab -
EOF
```

## 常用命令

### 检查 GPU 状态

```bash
# 检查 NVIDIA GPU
nvidia-smi

# 检查 OpenCL 设备
clinfo | grep -i "device name"

# 完整检查（推荐）
/usr/local/bin/check_opencl.sh
```

### 查看驱动锁定状态

```bash
# Ubuntu/Debian
apt-mark showhold | grep nvidia

# 解锁驱动（需要更新时）
sudo apt-mark unhold nvidia-driver-* nvidia-dkms-*

# 更新后重新锁定
sudo apt-mark hold nvidia-driver-* nvidia-dkms-*
```

### 查看监控日志

```bash
# 查看监控服务日志
sudo journalctl -u gpu-monitor.service -f

# 查看定期检查日志
tail -f /var/log/gpu_check.log

# 查看 GPU 监控日志
tail -f /var/log/gpu_monitor.log
```

### 检查告警标记

```bash
# 如果存在此文件，说明 GPU 有问题
if [ -f /tmp/gpu_alert.flag ]; then
    cat /tmp/gpu_alert.flag
fi
```

## 故障排查

### 问题：nvidia-smi 报错 "Driver/library version mismatch"

**原因**：驱动更新后未重启，内核模块和用户空间库版本不一致

**解决方案**：

```bash
# 方案 1：重启系统（推荐）
sudo reboot

# 方案 2：使用检查脚本自动修复
/usr/local/bin/check_opencl.sh
# 选择修复方案 2（重新加载驱动模块）
```

### 问题：OpenCL 设备检测不到

**检查步骤**：

```bash
# 1. 运行完整检查
/usr/local/bin/check_opencl.sh

# 2. 检查 NVIDIA 驱动
nvidia-smi

# 3. 检查 OpenCL 平台
clinfo | grep -i "platform"

# 4. 检查用户权限
groups | grep video
```

### 问题：驱动被自动更新导致问题

**预防措施**：

```bash
# 锁定驱动版本
sudo apt-mark hold nvidia-driver-* nvidia-dkms-*

# 验证锁定状态
apt-mark showhold | grep nvidia
```

### 问题：监控服务未运行

**检查和修复**：

```bash
# 检查服务状态
sudo systemctl status gpu-monitor.service

# 查看错误日志
sudo journalctl -u gpu-monitor.service -n 50

# 重启服务
sudo systemctl restart gpu-monitor.service

# 如果服务未安装
sudo cp scripts/gpu-monitor.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable gpu-monitor.service
sudo systemctl start gpu-monitor.service
```

## 批量操作

### 批量检查所有服务器

```bash
# 创建批量检查脚本
cat > check_all_servers.sh << 'EOF'
#!/bin/bash
SERVERS=(gpu worker-01 worker-02)

for server in "${SERVERS[@]}"; do
    echo "========== $server =========="
    ssh $server '/usr/local/bin/monitor_gpu.sh once'
    echo ""
done
EOF

chmod +x check_all_servers.sh
./check_all_servers.sh
```

### 批量查看监控状态

```bash
# 查看所有服务器的监控服务状态
for server in gpu worker-01 worker-02; do
    echo "========== $server =========="
    ssh $server 'sudo systemctl status gpu-monitor.service --no-pager'
    echo ""
done
```

## 最佳实践

### 1. 部署新服务器时

```bash
# 使用 deploy.sh 自动配置
./deploy.sh prod
# 选择 y 配置 GPU 驱动预防措施
```

### 2. 定期维护

```bash
# 每周检查一次所有服务器
./check_all_servers.sh

# 查看监控日志
ssh gpu 'tail -100 /var/log/gpu_check.log'
```

### 3. 更新驱动前

```bash
# 1. 解锁驱动
sudo apt-mark unhold nvidia-driver-*

# 2. 停止服务
sudo supervisorctl stop trap-factory

# 3. 更新驱动
sudo apt update && sudo apt upgrade

# 4. 立即重启
sudo reboot

# 5. 重启后验证
nvidia-smi
/usr/local/bin/check_opencl.sh

# 6. 重新锁定驱动
sudo apt-mark hold nvidia-driver-*
```

### 4. 监控集成

```bash
# 在应用启动脚本中添加 GPU 检查
if [ -f /tmp/gpu_alert.flag ]; then
    echo "警告: GPU 存在问题，请检查"
    cat /tmp/gpu_alert.flag
    exit 1
fi

# 启动应用
./factory-linux
```

## 相关文档

- [GPU_DRIVER_MAINTENANCE.md](GPU_DRIVER_MAINTENANCE.md) - GPU 驱动维护详细指南
- [批量部署说明.md](批量部署说明.md) - 部署流程详细说明
- [GPU环境配置指南.md](GPU环境配置指南.md) - GPU 环境配置
