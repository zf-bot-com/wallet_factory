#!/bin/bash

# OpenCL 设备检测和修复脚本
# 用于诊断和修复 OpenCL 设备不可见的问题

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查是否为 root 用户
check_root() {
    if [ "$EUID" -eq 0 ]; then
        log_warning "正在以 root 用户运行"
        return 0
    else
        log_info "当前用户: $(whoami)"
        return 1
    fi
}

# 检查 clinfo 是否安装
check_clinfo() {
    log_info "检查 clinfo 是否安装..."
    if command -v clinfo &> /dev/null; then
        log_success "clinfo 已安装"
        return 0
    else
        log_error "clinfo 未安装"
        return 1
    fi
}

# 安装 clinfo
install_clinfo() {
    log_info "尝试安装 clinfo..."

    if [ -f /etc/debian_version ]; then
        # Debian/Ubuntu
        sudo apt-get update
        sudo apt-get install -y clinfo
    elif [ -f /etc/redhat-release ]; then
        # RHEL/CentOS/Fedora
        sudo yum install -y clinfo || sudo dnf install -y clinfo
    elif [ -f /etc/arch-release ]; then
        # Arch Linux
        sudo pacman -S --noconfirm clinfo
    else
        log_error "无法识别的 Linux 发行版，请手动安装 clinfo"
        return 1
    fi

    log_success "clinfo 安装完成"
    return 0
}

# 检查 OpenCL 设备
check_opencl_devices() {
    log_info "检查 OpenCL 设备..."

    local device_count=$(clinfo 2>/dev/null | grep -i "device name" | wc -l)

    if [ "$device_count" -gt 0 ]; then
        log_success "找到 $device_count 个 OpenCL 设备:"
        clinfo | grep -i "device name"
        return 0
    else
        log_error "未找到任何 OpenCL 设备"
        return 1
    fi
}

# 检查 OpenCL 平台
check_opencl_platforms() {
    log_info "检查 OpenCL 平台..."

    local platform_count=$(clinfo 2>/dev/null | grep -i "platform name" | wc -l)

    if [ "$platform_count" -gt 0 ]; then
        log_success "找到 $platform_count 个 OpenCL 平台:"
        clinfo | grep -i "platform name"
        return 0
    else
        log_error "未找到任何 OpenCL 平台"
        return 1
    fi
}

# 检查显卡硬件
check_gpu_hardware() {
    log_info "检查显卡硬件..."

    # 检查 NVIDIA GPU
    if command -v nvidia-smi &> /dev/null; then
        log_info "检测到 NVIDIA GPU:"
        local nvidia_output=$(nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader 2>&1)
        local nvidia_exit_code=$?

        # 先检查输出内容是否包含错误信息
        if echo "$nvidia_output" | grep -qi "failed to initialize\|version mismatch"; then
            log_error "nvidia-smi 执行失败:"
            echo "$nvidia_output"

            # 检查是否是驱动版本不匹配
            if echo "$nvidia_output" | grep -qi "version mismatch"; then
                log_error "检测到 NVIDIA 驱动版本不匹配问题！"
                return 2  # 返回特殊代码表示版本不匹配
            fi
            return 1
        elif [ $nvidia_exit_code -eq 0 ]; then
            echo "$nvidia_output"
            return 0
        else
            log_error "nvidia-smi 执行失败 (退出代码: $nvidia_exit_code):"
            echo "$nvidia_output"
            return 1
        fi
    fi

    # 检查 AMD GPU
    if command -v rocm-smi &> /dev/null; then
        log_info "检测到 AMD GPU:"
        rocm-smi
        return 0
    fi

    # 使用 lspci 检查
    if command -v lspci &> /dev/null; then
        log_info "使用 lspci 检查 GPU:"
        lspci | grep -i "vga\|3d\|display"
        return 0
    fi

    log_warning "无法检测到 GPU 硬件信息"
    return 1
}

# 检查 NVIDIA 驱动状态
check_nvidia_driver_status() {
    log_info "检查 NVIDIA 驱动状态..."

    # 检查内核模块
    if lsmod | grep -q nvidia; then
        log_info "NVIDIA 内核模块已加载:"
        lsmod | grep nvidia
    else
        log_error "NVIDIA 内核模块未加载"
        return 1
    fi

    # 检查驱动版本
    if [ -f /proc/driver/nvidia/version ]; then
        log_info "内核驱动版本:"
        cat /proc/driver/nvidia/version
    fi

    # 检查库版本
    if command -v nvidia-smi &> /dev/null; then
        log_info "nvidia-smi 版本:"
        nvidia-smi --version 2>&1 | head -3
    fi

    return 0
}

# 修复 NVIDIA 驱动版本不匹配
fix_nvidia_driver_mismatch() {
    log_info "========================================="
    log_info "  修复 NVIDIA 驱动版本不匹配"
    log_info "========================================="
    echo ""

    log_warning "检测到 NVIDIA 驱动版本不匹配问题"
    log_info "这通常是因为驱动更新后内核模块未重新加载"
    echo ""

    log_info "可用的修复方案:"
    echo "  1. 重启系统（推荐，最可靠）"
    echo "  2. 重新加载 NVIDIA 内核模块（快速，但可能失败）"
    echo "  3. 重新安装 NVIDIA 驱动"
    echo ""

    read -p "选择修复方案 (1/2/3) 或按 q 退出: " -n 1 -r
    echo ""

    case $REPLY in
        1)
            log_warning "请手动重启系统以完成修复"
            log_info "重启后再次运行此脚本验证"
            exit 0
            ;;
        2)
            reload_nvidia_driver
            ;;
        3)
            reinstall_nvidia_driver
            ;;
        q|Q)
            log_info "退出修复"
            exit 1
            ;;
        *)
            log_error "无效选择"
            exit 1
            ;;
    esac
}

# 重新加载 NVIDIA 驱动
reload_nvidia_driver() {
    log_info "尝试重新加载 NVIDIA 驱动模块..."
    echo ""

    log_warning "警告: 这将终止所有使用 GPU 的进程！"
    read -p "确认继续? (y/n) " -n 1 -r
    echo ""

    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "取消操作"
        return 1
    fi

    # 检查是否有进程在使用 GPU
    if command -v fuser &> /dev/null; then
        log_info "检查使用 GPU 的进程..."
        fuser -v /dev/nvidia* 2>&1 || true
    fi

    # 卸载 NVIDIA 模块
    log_info "卸载 NVIDIA 内核模块..."

    sudo rmmod nvidia_uvm 2>/dev/null || true
    sudo rmmod nvidia_drm 2>/dev/null || true
    sudo rmmod nvidia_modeset 2>/dev/null || true
    sudo rmmod nvidia 2>/dev/null || true

    if lsmod | grep -q nvidia; then
        log_error "无法卸载 NVIDIA 模块，可能有进程正在使用"
        log_info "请尝试："
        log_info "  1. 停止所有使用 GPU 的进程"
        log_info "  2. 重启系统"
        return 1
    fi

    log_success "NVIDIA 模块已卸载"

    # 重新加载模块
    log_info "重新加载 NVIDIA 内核模块..."
    sudo modprobe nvidia
    sudo modprobe nvidia_modeset
    sudo modprobe nvidia_drm
    sudo modprobe nvidia_uvm

    if ! lsmod | grep -q nvidia; then
        log_error "无法加载 NVIDIA 模块"
        return 1
    fi

    log_success "NVIDIA 模块已重新加载"

    # 验证
    echo ""
    log_info "验证修复结果..."
    if nvidia-smi &> /dev/null; then
        log_success "nvidia-smi 现在可以正常工作了！"
        nvidia-smi
        return 0
    else
        log_error "nvidia-smi 仍然无法工作"
        log_warning "建议重启系统"
        return 1
    fi
}

# 重新安装 NVIDIA 驱动
reinstall_nvidia_driver() {
    log_info "重新安装 NVIDIA 驱动..."
    echo ""

    log_warning "这将重新安装 NVIDIA 驱动程序"
    log_info "请确保您知道正确的驱动版本"
    echo ""

    read -p "确认继续? (y/n) " -n 1 -r
    echo ""

    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "取消操作"
        return 1
    fi

    if [ -f /etc/debian_version ]; then
        # Ubuntu/Debian
        log_info "检测到 Debian/Ubuntu 系统"
        sudo apt-get update
        sudo apt-get install --reinstall -y nvidia-driver-* || \
        sudo apt-get install --reinstall -y nvidia-driver-535
    elif [ -f /etc/redhat-release ]; then
        # RHEL/CentOS
        log_info "检测到 RHEL/CentOS 系统"
        sudo yum reinstall -y kmod-nvidia* || \
        sudo dnf reinstall -y kmod-nvidia*
    else
        log_error "无法识别的系统，请手动重新安装驱动"
        return 1
    fi

    log_success "驱动重新安装完成"
    log_warning "请重启系统以使更改生效"
    return 0
}

# 检查 OpenCL 驱动
check_opencl_drivers() {
    log_info "检查 OpenCL 驱动..."

    # 检查 /etc/OpenCL/vendors
    if [ -d /etc/OpenCL/vendors ]; then
        log_info "OpenCL vendors 目录内容:"
        ls -la /etc/OpenCL/vendors/

        if [ -z "$(ls -A /etc/OpenCL/vendors/)" ]; then
            log_error "/etc/OpenCL/vendors/ 目录为空，缺少 OpenCL 驱动"
            return 1
        fi
    else
        log_error "/etc/OpenCL/vendors/ 目录不存在"
        return 1
    fi

    # 检查 libOpenCL.so
    if ldconfig -p | grep -q libOpenCL.so; then
        log_success "找到 libOpenCL.so"
        ldconfig -p | grep libOpenCL.so
    else
        log_error "未找到 libOpenCL.so"
        return 1
    fi

    return 0
}

# 检查用户权限
check_user_permissions() {
    log_info "检查用户权限..."

    local current_user=$(whoami)
    log_info "当前用户: $current_user"

    # 检查用户组
    log_info "用户所属组:"
    groups

    # 检查是否在 video 组
    if groups | grep -q video; then
        log_success "用户在 video 组中"
    else
        log_warning "用户不在 video 组中，这可能导致 GPU 访问问题"
        return 1
    fi

    # 检查 /dev/dri 权限
    if [ -d /dev/dri ]; then
        log_info "/dev/dri 目录权限:"
        ls -la /dev/dri/
    else
        log_warning "/dev/dri 目录不存在"
    fi

    return 0
}

# 修复用户权限
fix_user_permissions() {
    log_info "尝试修复用户权限..."

    local current_user=$(whoami)

    if ! groups | grep -q video; then
        log_info "将用户 $current_user 添加到 video 组..."
        sudo usermod -a -G video $current_user
        log_success "已将用户添加到 video 组，需要重新登录才能生效"
    fi

    if ! groups | grep -q render; then
        log_info "将用户 $current_user 添加到 render 组..."
        sudo usermod -a -G render $current_user 2>/dev/null || log_warning "render 组不存在，跳过"
    fi

    return 0
}

# 安装 NVIDIA OpenCL 驱动
install_nvidia_opencl() {
    log_info "尝试安装 NVIDIA OpenCL 驱动..."

    if ! command -v nvidia-smi &> /dev/null; then
        log_error "未检测到 NVIDIA GPU 或驱动，跳过"
        return 1
    fi

    if [ -f /etc/debian_version ]; then
        sudo apt-get update
        sudo apt-get install -y nvidia-opencl-icd ocl-icd-opencl-dev
    elif [ -f /etc/redhat-release ]; then
        sudo yum install -y ocl-icd ocl-icd-devel || sudo dnf install -y ocl-icd ocl-icd-devel
    fi

    log_success "NVIDIA OpenCL 驱动安装完成"
    return 0
}

# 安装 AMD OpenCL 驱动
install_amd_opencl() {
    log_info "尝试安装 AMD OpenCL 驱动..."

    if [ -f /etc/debian_version ]; then
        sudo apt-get update
        sudo apt-get install -y mesa-opencl-icd ocl-icd-opencl-dev
    elif [ -f /etc/redhat-release ]; then
        sudo yum install -y mesa-libOpenCL ocl-icd ocl-icd-devel || \
        sudo dnf install -y mesa-libOpenCL ocl-icd ocl-icd-devel
    fi

    log_success "AMD OpenCL 驱动安装完成"
    return 0
}

# 重启相关服务
restart_services() {
    log_info "尝试重启相关服务..."

    # 重新加载 udev 规则
    if command -v udevadm &> /dev/null; then
        sudo udevadm control --reload-rules
        sudo udevadm trigger
        log_success "已重新加载 udev 规则"
    fi

    return 0
}

# 显示详细诊断信息
show_diagnostic_info() {
    log_info "========== 详细诊断信息 =========="

    echo ""
    log_info "系统信息:"
    uname -a

    echo ""
    log_info "发行版信息:"
    cat /etc/*-release 2>/dev/null | head -5

    echo ""
    log_info "内核模块:"
    lsmod | grep -E "nvidia|amdgpu|i915" || log_warning "未找到 GPU 相关内核模块"

    echo ""
    log_info "OpenCL ICD Loader:"
    ldconfig -p | grep -i opencl || log_warning "未找到 OpenCL 库"

    echo ""
    if command -v clinfo &> /dev/null; then
        log_info "完整 clinfo 输出:"
        clinfo 2>&1 | head -100
    fi

    echo ""
    log_info "========== 诊断信息结束 =========="
}

# 主函数
main() {
    echo ""
    log_info "========================================="
    log_info "  OpenCL 设备检测和修复脚本"
    log_info "========================================="
    echo ""

    # 检查 clinfo
    if ! check_clinfo; then
        read -p "是否安装 clinfo? (y/n) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            install_clinfo || exit 1
        else
            log_error "需要 clinfo 才能继续检测"
            exit 1
        fi
    fi

    echo ""

    # 检查 GPU 硬件
    check_gpu_hardware
    local gpu_check_result=$?

    # 如果检测到 NVIDIA 驱动版本不匹配
    if [ $gpu_check_result -eq 2 ]; then
        echo ""
        check_nvidia_driver_status
        echo ""
        fix_nvidia_driver_mismatch
        echo ""
        log_info "重新检查 GPU 硬件..."
        check_gpu_hardware
    fi

    echo ""

    # 检查 OpenCL 平台
    check_opencl_platforms

    echo ""

    # 检查 OpenCL 设备
    if check_opencl_devices; then
        log_success "OpenCL 设备检测正常！"
        exit 0
    fi

    echo ""
    log_warning "未检测到 OpenCL 设备，开始诊断..."

    echo ""

    # 检查驱动
    check_opencl_drivers

    echo ""

    # 检查权限
    check_user_permissions

    echo ""

    # 显示诊断信息
    show_diagnostic_info

    echo ""
    log_info "========================================="
    log_info "  修复建议"
    log_info "========================================="
    echo ""

    read -p "是否尝试自动修复? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "跳过自动修复"
        exit 1
    fi

    echo ""

    # 修复用户权限
    fix_user_permissions

    echo ""

    # 尝试安装驱动
    if command -v nvidia-smi &> /dev/null; then
        install_nvidia_opencl
    else
        install_amd_opencl
    fi

    echo ""

    # 重启服务
    restart_services

    echo ""
    log_info "========================================="
    log_info "  修复完成，重新检测..."
    log_info "========================================="
    echo ""

    # 重新检测
    if check_opencl_devices; then
        log_success "修复成功！OpenCL 设备现在可用"
        exit 0
    else
        log_error "修复后仍未检测到 OpenCL 设备"
        log_info "可能需要："
        log_info "1. 重新登录以应用组权限更改"
        log_info "2. 重启系统以加载新驱动"
        log_info "3. 手动安装对应的 GPU 驱动程序"
        exit 1
    fi
}

# 运行主函数
main "$@"
