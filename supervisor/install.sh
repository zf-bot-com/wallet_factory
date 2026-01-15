#!/bin/bash

# Supervisor 安装脚本（在服务器上执行）
# 使用方法: 在服务器上执行 ./install.sh

set -e

APP_NAME="trap-factory"
APP_DIR="/srv/${APP_NAME}"
CONF_FILE="trap-factory.conf"

echo "=========================================="
echo "安装 ${APP_NAME} Supervisor 配置"
echo "=========================================="

# 检查 supervisor 是否安装
if ! command -v supervisorctl &> /dev/null; then
    echo "安装 supervisor..."
    sudo apt-get update
    sudo apt-get install -y supervisor
fi

# 创建日志目录
echo "创建日志目录..."
sudo mkdir -p /var/log/${APP_NAME}
sudo chown root:root /var/log/${APP_NAME}

# 检查应用目录是否存在
if [ ! -d "${APP_DIR}" ]; then
    echo "❌ 错误: 应用目录 ${APP_DIR} 不存在"
    echo "请先部署应用程序"
    exit 1
fi

# 复制配置文件
echo "复制 supervisor 配置文件..."
if [ -f "${CONF_FILE}" ]; then
    sudo cp ${CONF_FILE} /etc/supervisor/conf.d/${CONF_FILE}
else
    echo "❌ 错误: 找不到配置文件 ${CONF_FILE}"
    exit 1
fi

# 重新加载配置
echo "重新加载 supervisor 配置..."
sudo supervisorctl reread
sudo supervisorctl update

# 启动服务
echo "启动服务..."
sudo supervisorctl start ${APP_NAME} || true

# 查看状态
echo ""
echo "服务状态:"
sudo supervisorctl status ${APP_NAME}

echo ""
echo "=========================================="
echo "✅ 安装完成！"
echo "=========================================="
echo ""
echo "常用命令:"
echo "  查看状态: sudo supervisorctl status ${APP_NAME}"
echo "  查看日志: sudo supervisorctl tail -f ${APP_NAME}"
echo "  停止服务: sudo supervisorctl stop ${APP_NAME}"
echo "  启动服务: sudo supervisorctl start ${APP_NAME}"
echo "  重启服务: sudo supervisorctl restart ${APP_NAME}"
echo ""
