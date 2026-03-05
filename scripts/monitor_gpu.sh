#!/bin/bash

# GPU 状态监控脚本
# 定期检查 GPU 驱动状态，发现问题时告警

set -e

# 配置
LOG_FILE="/var/log/gpu_monitor.log"
ALERT_FILE="/tmp/gpu_alert.flag"
CHECK_INTERVAL=300  # 检查间隔（秒），默认 5 分钟

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 日志函数
log() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "[$timestamp] $1" | tee -a "$LOG_FILE"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    log "ERROR: $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
    log "WARNING: $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
    log "SUCCESS: $1"
}

# 检查 NVIDIA 驱动状态
check_nvidia_driver() {
    if ! command -v nvidia-smi &> /dev/null; then
        return 0  # 没有 NVIDIA GPU，跳过检查
    fi

    local output=$(nvidia-smi --query-gpu=name,driver_version --format=csv,noheader 2>&1)
    local exit_code=$?

    # 检查是否有版本不匹配错误
    if echo "$output" | grep -qi "version mismatch"; then
        log_error "检测到 NVIDIA 驱动版本不匹配！"
        log_error "输出: $output"
        return 1
    fi

    # 检查是否有其他初始化错误
    if echo "$output" | grep -qi "failed to initialize"; then
        log_error "NVIDIA 驱动初始化失败！"
        log_error "输出: $output"
        return 1
    fi

    # 检查退出代码
    if [ $exit_code -ne 0 ]; then
        log_error "nvidia-smi 执行失败 (退出代码: $exit_code)"
        return 1
    fi

    return 0
}

# 检查 OpenCL 设备
check_opencl_devices() {
    if ! command -v clinfo &> /dev/null; then
        log_warning "clinfo 未安装，跳过 OpenCL 检查"
        return 0
    fi

    local device_count=$(clinfo 2>/dev/null | grep -i "device name" | wc -l)

    if [ "$device_count" -eq 0 ]; then
        log_error "未检测到 OpenCL 设备！"
        return 1
    fi

    return 0
}

# 发送告警
send_alert() {
    local message="$1"

    # 创建告警标记文件
    echo "$message" > "$ALERT_FILE"

    # 这里可以添加其他告警方式
    # 例如：发送邮件、调用 webhook、发送消息到 Slack 等

    log_warning "已创建告警标记: $ALERT_FILE"
}

# 清除告警
clear_alert() {
    if [ -f "$ALERT_FILE" ]; then
        rm -f "$ALERT_FILE"
        log_success "已清除告警标记"
    fi
}

# 单次检查
check_once() {
    log "开始 GPU 状态检查..."

    local has_error=0

    # 检查 NVIDIA 驱动
    if ! check_nvidia_driver; then
        has_error=1
        send_alert "NVIDIA 驱动异常"
    fi

    # 检查 OpenCL 设备
    if ! check_opencl_devices; then
        has_error=1
        send_alert "OpenCL 设备不可用"
    fi

    if [ $has_error -eq 0 ]; then
        log_success "GPU 状态正常"
        clear_alert
        return 0
    else
        log_error "GPU 状态异常，需要处理！"
        return 1
    fi
}

# 持续监控模式
monitor_loop() {
    log "启动 GPU 监控服务（检查间隔: ${CHECK_INTERVAL}秒）"

    while true; do
        check_once

        sleep "$CHECK_INTERVAL"
    done
}

# 主函数
main() {
    case "${1:-once}" in
        once)
            check_once
            ;;
        monitor)
            monitor_loop
            ;;
        *)
            echo "用法: $0 [once|monitor]"
            echo "  once    - 执行一次检查（默认）"
            echo "  monitor - 持续监控模式"
            exit 1
            ;;
    esac
}

main "$@"
