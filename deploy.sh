#!/bin/bash

# 部署脚本 - 自动编译、打包并批量上传到多个服务器
# 使用方法: ./deploy.sh [环境]
# 示例: ./deploy.sh prod

# 配置
ENV="${1:-prod}"
APP_NAME="trap-factory"
TAR_FILE="trap-factory-linux.tar.gz"
SERVERS_FILE="servers.env.${ENV}"

set -e  # 遇到错误立即退出

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印带颜色的消息
print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# 确认函数：Enter 或 y/Y 继续，其他取消
# 用法: confirm "提示信息" || exit 1
confirm() {
    local prompt="${1:-确认继续?}"
    read -r -p "${prompt} [Y/n] " response
    # 空输入或 y/Y 都视为确认
    [[ -z "$response" || "$response" =~ ^[Yy]$ ]]
}

# 读取服务器列表
read_servers() {
    if [ ! -f "${SERVERS_FILE}" ]; then
        print_error "服务器配置文件不存在: ${SERVERS_FILE}"
        print_info "可用的配置文件:"
        ls -1 servers.env.* 2>/dev/null || echo "  无"
        exit 1
    fi

    # 读取服务器列表（忽略注释和空行）
    SERVERS=()
    while IFS= read -r line || [ -n "$line" ]; do
        # 去除前后空格
        line=$(echo "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        # 跳过空行和注释
        if [ -z "$line" ] || [[ "$line" =~ ^# ]]; then
            continue
        fi
        SERVERS+=("$line")
    done < "${SERVERS_FILE}"

    if [ ${#SERVERS[@]} -eq 0 ]; then
        print_error "服务器配置文件为空: ${SERVERS_FILE}"
        exit 1
    fi
}

# 解析服务器地址和路径
parse_server() {
    local server_config="$1"
    if [[ "$server_config" == *":"* ]]; then
        SERVER_ADDR="${server_config%%:*}"
        SERVER_PATH="${server_config#*:}"
    else
        SERVER_ADDR="$server_config"
        SERVER_PATH="/srv"
    fi
}

echo "=========================================="
echo "开始批量部署 ${APP_NAME}"
echo "环境: ${ENV}"
echo "=========================================="

# 读取服务器列表
read_servers
print_info "目标服务器数量: ${#SERVERS[@]}"
for i in "${!SERVERS[@]}"; do
    parse_server "${SERVERS[$i]}"
    echo "  [$((i+1))] ${SERVER_ADDR}:${SERVER_PATH}"
done
echo ""

# 1. 编译并打包
echo "=========================================="
echo "[步骤 1/3] 编译并打包"
echo "=========================================="
if confirm "开始编译并打包 (环境: ${ENV})?"; then
    make linux ENV=${ENV}
    if [ $? -ne 0 ]; then
        print_error "编译失败"
        exit 1
    fi
    print_success "编译完成"

    make tar-linux
    if [ $? -ne 0 ]; then
        print_error "打包失败"
        exit 1
    fi
    print_success "打包完成: ${TAR_FILE}"
else
    print_info "跳过编译打包"
fi
echo ""

# 2. 检查所有服务器的 GPU 环境
echo "=========================================="
echo "[步骤 2/3] 检查服务器 GPU 环境"
echo "=========================================="
ENV_CHECK_SUCCESS=0
ENV_CHECK_FAILED=0
ENV_FAILED_SERVERS=()

if confirm "检查服务器 GPU 环境?"; then
    for i in "${!SERVERS[@]}"; do
        parse_server "${SERVERS[$i]}"
        print_info "[$((i+1))/${#SERVERS[@]}] 检查 ${SERVER_ADDR} 的 GPU 环境..."

        # 检查 clinfo 是否安装
        if ssh ${SERVER_ADDR} 'which clinfo' >/dev/null 2>&1; then
            # 检查是否能检测到 GPU
            if ssh ${SERVER_ADDR} 'clinfo 2>/dev/null | grep -i "device name"' >/dev/null 2>&1; then
                GPU_INFO=$(ssh ${SERVER_ADDR} 'clinfo 2>/dev/null | grep -i "device name" | head -1')
                print_success "环境正常: ${SERVER_ADDR} - ${GPU_INFO}"
                ((ENV_CHECK_SUCCESS++))
            else
                print_error "未检测到 GPU: ${SERVER_ADDR}"
                ((ENV_CHECK_FAILED++))
                ENV_FAILED_SERVERS+=("${SERVER_ADDR}")
            fi
        else
            print_warning "clinfo 未安装: ${SERVER_ADDR}"
            ((ENV_CHECK_FAILED++))
            ENV_FAILED_SERVERS+=("${SERVER_ADDR}")
        fi
    done

    echo ""
    print_info "环境检查统计: 正常 ${ENV_CHECK_SUCCESS}, 异常 ${ENV_CHECK_FAILED}"

    if [ ${ENV_CHECK_FAILED} -gt 0 ]; then
        print_warning "以下服务器环境异常:"
        for server in "${ENV_FAILED_SERVERS[@]}"; do
            echo "  - ${server}"
        done
        echo ""
        print_info "修复方法:"
        echo "  ssh <服务器> 'apt update && apt install -y ocl-icd-libopencl1 clinfo'"
        echo ""
        if confirm "自动修复环境异常的服务器?"; then
            print_info "开始自动修复环境..."
            for server in "${ENV_FAILED_SERVERS[@]}"; do
                print_info "修复 ${server}..."
                if ssh ${server} 'apt update && apt install -y ocl-icd-libopencl1 clinfo' 2>/dev/null; then
                    print_success "修复成功: ${server}"
                else
                    print_error "修复失败: ${server}"
                fi
            done
            echo ""
            print_info "重新检查环境..."
            ENV_CHECK_FAILED=0
            ENV_FAILED_SERVERS=()
            for i in "${!SERVERS[@]}"; do
                parse_server "${SERVERS[$i]}"
                if ! ssh ${SERVER_ADDR} 'which clinfo && clinfo 2>/dev/null | grep -i "device name"' >/dev/null 2>&1; then
                    ((ENV_CHECK_FAILED++))
                    ENV_FAILED_SERVERS+=("${SERVER_ADDR}")
                fi
            done
            if [ ${ENV_CHECK_FAILED} -gt 0 ]; then
                print_error "仍有 ${ENV_CHECK_FAILED} 个服务器环境异常"
                print_warning "请手动修复后再部署"
                exit 1
            else
                print_success "所有服务器环境已修复"
            fi
        else
            print_info "跳过自动修复"
        fi
    fi
else
    print_info "跳过 GPU 环境检查"
fi
echo ""

# 3. 上传并部署到所有服务器
echo "=========================================="
echo "[步骤 3/3] 上传并部署到服务器"
echo "=========================================="
UPLOAD_SUCCESS=0
UPLOAD_FAILED=0
DEPLOY_SUCCESS=0
DEPLOY_FAILED=0
FAILED_SERVERS=()

if confirm "开始上传并部署?"; then
    for i in "${!SERVERS[@]}"; do
        parse_server "${SERVERS[$i]}"

        # 跳过环境异常的服务器
        if [[ " ${ENV_FAILED_SERVERS[@]} " =~ " ${SERVER_ADDR} " ]]; then
            print_warning "[$((i+1))/${#SERVERS[@]}] 跳过 ${SERVER_ADDR} (环境异常)"
            continue
        fi

        # 上传
        print_info "[$((i+1))/${#SERVERS[@]}] 上传到 ${SERVER_ADDR}:${SERVER_PATH}..."
        if ! scp ${TAR_FILE} ${SERVER_ADDR}:${SERVER_PATH}/ 2>/dev/null; then
            print_error "上传失败: ${SERVER_ADDR}"
            ((UPLOAD_FAILED++))
            FAILED_SERVERS+=("${SERVER_ADDR}")
            continue
        fi
        print_success "上传成功: ${SERVER_ADDR}"
        ((UPLOAD_SUCCESS++))

        # 部署
        print_info "[$((i+1))/${#SERVERS[@]}] 部署到 ${SERVER_ADDR}..."
        if ssh ${SERVER_ADDR} bash << EOF
set -e
cd ${SERVER_PATH}

# 备份旧版本（如果存在）
if [ -d "${APP_NAME}" ]; then
    echo "  备份旧版本..."
    mv ${APP_NAME} ${APP_NAME}.backup.\$(date +%Y%m%d_%H%M%S) 2>/dev/null || true
fi

# 解压新版本
echo "  解压新版本..."
tar -xzf ${TAR_FILE} 2>/dev/null || tar -xzf ${TAR_FILE}

# 检查解压后的目录
EXTRACTED_DIR=""
if [ -d "trap-factory-linux" ]; then
    EXTRACTED_DIR="trap-factory-linux"
elif [ -d "${APP_NAME}" ]; then
    EXTRACTED_DIR="${APP_NAME}"
else
    echo "  错误: 无法找到解压后的目录"
    exit 1
fi

# 重命名为标准目录名
if [ "\${EXTRACTED_DIR}" != "${APP_NAME}" ]; then
    mv \${EXTRACTED_DIR} ${APP_NAME}
fi

# 设置执行权限
chmod +x ${APP_NAME}/factory-linux
chmod +x ${APP_NAME}/profanity.x64

# 清理压缩包
rm -f ${TAR_FILE}

echo "  部署完成"
EOF
        then
            print_success "部署成功: ${SERVER_ADDR}"
            ((DEPLOY_SUCCESS++))
        else
            print_error "部署失败: ${SERVER_ADDR}"
            ((DEPLOY_FAILED++))
        fi
    done

    echo ""
    echo "=========================================="
    echo "部署完成"
    echo "=========================================="
    print_info "统计:"
    echo "  - 上传成功: ${UPLOAD_SUCCESS}, 失败: ${UPLOAD_FAILED}"
    echo "  - 部署成功: ${DEPLOY_SUCCESS}, 失败: ${DEPLOY_FAILED}"
    echo "  - 总计: ${#SERVERS[@]}"
    echo ""

    if [ ${DEPLOY_FAILED} -eq 0 ] && [ ${UPLOAD_FAILED} -eq 0 ]; then
        print_success "所有服务器部署成功！"
    else
        print_warning "部分服务器部署失败，请检查日志"
    fi
else
    print_info "跳过上传部署"
fi

# 询问是否配置 GPU 驱动预防措施
echo ""
if confirm "配置 GPU 驱动预防措施? (推荐)"; then
    echo ""
    echo "=========================================="
    echo "配置 GPU 驱动预防措施"
    echo "=========================================="

    GPU_SETUP_SUCCESS=0
    GPU_SETUP_FAILED=0

    for i in "${!SERVERS[@]}"; do
        parse_server "${SERVERS[$i]}"

        # 跳过环境异常和部署失败的服务器
        if [[ " ${ENV_FAILED_SERVERS[@]} " =~ " ${SERVER_ADDR} " ]] || [[ " ${FAILED_SERVERS[@]} " =~ " ${SERVER_ADDR} " ]]; then
            print_warning "[$((i+1))/${#SERVERS[@]}] 跳过 ${SERVER_ADDR}"
            continue
        fi

        print_info "[$((i+1))/${#SERVERS[@]}] 配置 ${SERVER_ADDR} 的 GPU 驱动预防措施..."

        # 上传脚本文件
        if ! scp scripts/check_opencl.sh scripts/monitor_gpu.sh scripts/gpu-monitor.service ${SERVER_ADDR}:/tmp/ 2>/dev/null; then
            print_error "上传脚本失败: ${SERVER_ADDR}"
            ((GPU_SETUP_FAILED++))
            continue
        fi

        # 在服务器上配置
        if ssh ${SERVER_ADDR} bash << 'EOFGPU'
set -e

echo "  [1/5] 安装脚本到系统目录..."
sudo cp /tmp/check_opencl.sh /usr/local/bin/
sudo cp /tmp/monitor_gpu.sh /usr/local/bin/
sudo chmod +x /usr/local/bin/check_opencl.sh
sudo chmod +x /usr/local/bin/monitor_gpu.sh
echo "  [SUCCESS] 脚本已安装"

echo "  [2/5] 锁定 NVIDIA 驱动版本..."
if command -v apt-mark &> /dev/null; then
    # Ubuntu/Debian
    if dpkg -l | grep -q nvidia-driver; then
        sudo apt-mark hold nvidia-driver-* nvidia-dkms-* 2>/dev/null || true
        echo "  [SUCCESS] NVIDIA 驱动版本已锁定"
    else
        echo "  [INFO] 未检测到 NVIDIA 驱动包，跳过"
    fi
elif command -v yum &> /dev/null; then
    # RHEL/CentOS
    if rpm -qa | grep -q nvidia; then
        if ! yum list installed yum-plugin-versionlock &> /dev/null; then
            echo "  [INFO] 安装 yum-plugin-versionlock..."
            sudo yum install -y yum-plugin-versionlock 2>/dev/null || true
        fi
        sudo yum versionlock nvidia-* kmod-nvidia-* 2>/dev/null || true
        echo "  [SUCCESS] NVIDIA 驱动版本已锁定"
    else
        echo "  [INFO] 未检测到 NVIDIA 驱动包，跳过"
    fi
else
    echo "  [WARNING] 无法识别的包管理器，跳过驱动锁定"
fi

echo "  [3/5] 配置 GPU 监控服务..."
sudo cp /tmp/gpu-monitor.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable gpu-monitor.service 2>/dev/null || true
sudo systemctl restart gpu-monitor.service 2>/dev/null || true
echo "  [SUCCESS] GPU 监控服务已配置"

echo "  [4/5] 配置定期检查任务..."
# 添加 crontab 任务（如果不存在）
if ! crontab -l 2>/dev/null | grep -q "monitor_gpu.sh"; then
    (crontab -l 2>/dev/null; echo "# GPU 状态定期检查") | crontab -
    (crontab -l 2>/dev/null; echo "0 * * * * /usr/local/bin/monitor_gpu.sh once >> /var/log/gpu_check.log 2>&1") | crontab -
    echo "  [SUCCESS] 定期检查任务已添加"
else
    echo "  [INFO] 定期检查任务已存在"
fi

echo "  [5/5] 验证配置..."
# 检查服务状态
if systemctl is-active --quiet gpu-monitor.service; then
    echo "  [SUCCESS] GPU 监控服务运行正常"
else
    echo "  [WARNING] GPU 监控服务未运行"
fi

# 检查驱动锁定状态
if command -v apt-mark &> /dev/null; then
    HELD_PACKAGES=$(apt-mark showhold | grep nvidia | wc -l)
    if [ $HELD_PACKAGES -gt 0 ]; then
        echo "  [SUCCESS] 已锁定 $HELD_PACKAGES 个 NVIDIA 驱动包"
    fi
fi

echo ""
echo "  配置完成！"
echo "  - 检查脚本: /usr/local/bin/check_opencl.sh"
echo "  - 监控脚本: /usr/local/bin/monitor_gpu.sh"
echo "  - 监控服务: systemctl status gpu-monitor.service"
echo "  - 检查日志: journalctl -u gpu-monitor.service -f"

EOFGPU
        then
            print_success "GPU 驱动预防措施配置成功: ${SERVER_ADDR}"
            ((GPU_SETUP_SUCCESS++))
        else
            print_error "GPU 驱动预防措施配置失败: ${SERVER_ADDR}"
            ((GPU_SETUP_FAILED++))
        fi
    done

    echo ""
    print_info "GPU 驱动预防措施配置统计:"
    echo "  - 成功: ${GPU_SETUP_SUCCESS}"
    echo "  - 失败: ${GPU_SETUP_FAILED}"
    echo ""

    if [ ${GPU_SETUP_FAILED} -eq 0 ]; then
        print_success "所有服务器 GPU 驱动预防措施配置成功！"
    else
        print_warning "部分服务器 GPU 驱动预防措施配置失败"
    fi
fi

# 询问是否配置 supervisor
echo ""
if confirm "配置 supervisor 服务?"; then
    echo ""
    echo "=========================================="
    echo "配置 supervisor 服务"
    echo "=========================================="

    # 检查 supervisor 配置文件是否存在
    if [ ! -f "supervisor/trap-factory.conf" ]; then
        print_error "supervisor 配置文件不存在: supervisor/trap-factory.conf"
        exit 1
    fi

    SUPERVISOR_SUCCESS=0
    SUPERVISOR_FAILED=0

    for i in "${!SERVERS[@]}"; do
        parse_server "${SERVERS[$i]}"

        # 跳过环境异常和部署失败的服务器
        if [[ " ${ENV_FAILED_SERVERS[@]} " =~ " ${SERVER_ADDR} " ]] || [[ " ${FAILED_SERVERS[@]} " =~ " ${SERVER_ADDR} " ]]; then
            print_warning "[$((i+1))/${#SERVERS[@]}] 跳过 ${SERVER_ADDR}"
            continue
        fi

        print_info "[$((i+1))/${#SERVERS[@]}] 配置 ${SERVER_ADDR} 的 supervisor..."

        # 上传 supervisor 配置文件
        if ! scp supervisor/trap-factory.conf ${SERVER_ADDR}:/tmp/trap-factory.conf 2>/dev/null; then
            print_error "上传配置文件失败: ${SERVER_ADDR}"
            ((SUPERVISOR_FAILED++))
            continue
        fi

        # 在服务器上配置 supervisor
        ssh ${SERVER_ADDR} bash << 'EOF'
set -e

echo "  [1/6] 检查 supervisor 是否安装..."
if ! command -v supervisorctl &> /dev/null; then
    echo "  [INFO] supervisor 未安装，正在安装..."

    # 等待 dpkg 锁释放（最多等待 60 秒）
    echo "  [INFO] 检查 dpkg 锁状态..."
    for i in {1..12}; do
        if ! sudo fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; then
            break
        fi
        echo "  [INFO] 等待其他 apt 进程完成... ($i/12)"
        sleep 5
    done

    # 再次检查锁状态
    if sudo fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; then
        echo "  [WARNING] dpkg 锁仍被占用，尝试强制解锁..."
        sudo killall apt apt-get unattended-upgr 2>/dev/null || true
        sleep 2
    fi

    if sudo apt-get update && sudo apt-get install -y supervisor; then
        echo "  [SUCCESS] supervisor 安装成功"
    else
        echo "  [ERROR] supervisor 安装失败"
        exit 1
    fi
else
    echo "  [SUCCESS] supervisor 已安装"
fi

echo "  [2/6] 确保 supervisor 服务开机自启..."
if command -v systemctl &> /dev/null; then
    sudo systemctl enable supervisor 2>/dev/null || true
    sudo systemctl start supervisor 2>/dev/null || true
    echo "  [SUCCESS] supervisor 服务已启用"
elif command -v service &> /dev/null; then
    sudo update-rc.d supervisor defaults 2>/dev/null || true
    sudo service supervisor start 2>/dev/null || true
    echo "  [SUCCESS] supervisor 服务已启用"
fi

echo "  [3/6] 创建日志目录..."
sudo mkdir -p /var/log/trap-factory
sudo chown root:root /var/log/trap-factory
echo "  [SUCCESS] 日志目录已创建"

echo "  [4/6] 复制配置文件..."
sudo cp /tmp/trap-factory.conf /etc/supervisor/conf.d/trap-factory.conf
echo "  [SUCCESS] 配置文件已复制"

echo "  [5/6] 重新加载配置..."
if sudo supervisorctl reread; then
    echo "  [SUCCESS] 配置已重新加载"
else
    echo "  [ERROR] 配置重新加载失败"
    exit 1
fi

if sudo supervisorctl update; then
    echo "  [SUCCESS] 配置已更新"
else
    echo "  [ERROR] 配置更新失败"
    exit 1
fi

echo "  [6/6] 重启服务..."
if sudo supervisorctl restart trap-factory 2>/dev/null || sudo supervisorctl start trap-factory 2>/dev/null; then
    echo "  [SUCCESS] 服务已启动"
else
    echo "  [ERROR] 服务启动失败"
    exit 1
fi

echo ""
echo "  服务状态:"
sudo supervisorctl status trap-factory

EOF

        if [ $? -eq 0 ]; then
            print_success "supervisor 配置成功: ${SERVER_ADDR}"
            ((SUPERVISOR_SUCCESS++))
        else
            print_error "supervisor 配置失败: ${SERVER_ADDR}"
            ((SUPERVISOR_FAILED++))
        fi
    done

    echo ""
    print_info "supervisor 配置统计:"
    echo "  - 成功: ${SUPERVISOR_SUCCESS}"
    echo "  - 失败: ${SUPERVISOR_FAILED}"
    echo ""

    if [ ${SUPERVISOR_FAILED} -eq 0 ]; then
        print_success "所有服务器 supervisor 配置成功！"
    else
        print_warning "部分服务器 supervisor 配置失败"
    fi
fi

echo ""
print_info "常用命令:"
echo "  查看服务状态: ssh <服务器> 'sudo supervisorctl status trap-factory'"
echo "  查看服务日志: ssh <服务器> 'sudo supervisorctl tail -f trap-factory'"
echo "  重启服务: ssh <服务器> 'sudo supervisorctl restart trap-factory'"
echo ""
