#!/bin/bash

# 部署脚本 - 自动编译、打包并上传到服务器
# 使用方法: ./deploy.sh [服务器地址] [服务器路径]

# 配置
SERVER="${1:-gpu}"
SERVER_PATH="${2:-/srv}"
APP_NAME="trap-factory"
TAR_FILE="trap-factory-linux.tar.gz"
REMOTE_DIR="${SERVER_PATH}/${APP_NAME}"

set -e  # 遇到错误立即退出

echo "=========================================="
echo "开始部署 ${APP_NAME} 到服务器 ${SERVER}"
echo "=========================================="

# 1. 编译 Linux 版本
echo ""
echo "[1/5] 编译 Linux 版本..."
make linux
if [ $? -ne 0 ]; then
    echo "❌ 编译失败"
    exit 1
fi
echo "✅ 编译成功"

# 2. 打包
echo ""
echo "[2/5] 打包程序..."
make tar-linux
if [ $? -ne 0 ]; then
    echo "❌ 打包失败"
    exit 1
fi
echo "✅ 打包成功: ${TAR_FILE}"

# 3. 上传到服务器
echo ""
echo "[3/5] 上传到服务器 ${SERVER}:${SERVER_PATH}..."
scp ${TAR_FILE} ${SERVER}:${SERVER_PATH}/
if [ $? -ne 0 ]; then
    echo "❌ 上传失败"
    exit 1
fi
echo "✅ 上传成功"

# 4. 在服务器上解压和安装
echo ""
echo "[4/5] 在服务器上解压和安装..."
ssh ${SERVER} << EOF
set -e
cd ${SERVER_PATH}

# 备份旧版本（如果存在）
if [ -d "${APP_NAME}" ]; then
    echo "备份旧版本..."
    mv ${APP_NAME} ${APP_NAME}.backup.\$(date +%Y%m%d_%H%M%S) || true
fi

# 解压新版本（忽略 macOS 扩展属性警告）
echo "解压新版本..."
tar -xzf ${TAR_FILE} 2>/dev/null || tar -xzf ${TAR_FILE}

# 检查解压后的目录（可能是 trap-factory-linux 或 trap-factory）
EXTRACTED_DIR=""
if [ -d "trap-factory-linux" ]; then
    EXTRACTED_DIR="trap-factory-linux"
elif [ -d "${APP_NAME}" ]; then
    EXTRACTED_DIR="${APP_NAME}"
else
    echo "❌ 错误: 无法找到解压后的目录"
    ls -la
    exit 1
fi

# 重命名为标准目录名
if [ "\${EXTRACTED_DIR}" != "${APP_NAME}" ]; then
    echo "重命名目录: \${EXTRACTED_DIR} -> ${APP_NAME}"
    mv \${EXTRACTED_DIR} ${APP_NAME}
fi

# 设置执行权限
chmod +x ${APP_NAME}/factory-linux
chmod +x ${APP_NAME}/profanity.x64

# 清理压缩包
rm -f ${TAR_FILE}

echo "✅ 解压完成"
EOF

if [ $? -ne 0 ]; then
    echo "❌ 服务器端安装失败"
    exit 1
fi

# 5. 更新 supervisor 配置并重启服务
echo ""
echo "[5/5] 更新 supervisor 配置并重启服务..."
scp supervisor/trap-factory.conf ${SERVER}:/tmp/trap-factory.conf
ssh ${SERVER} << EOF
set -e

# 检查 supervisor 是否安装
if ! command -v supervisorctl &> /dev/null; then
    echo "⚠️  supervisor 未安装，正在安装..."
    sudo apt-get update
    sudo apt-get install -y supervisor
fi

# 确保 supervisor 服务开机自启
if command -v systemctl &> /dev/null; then
    echo "设置 supervisor 开机自启..."
    sudo systemctl enable supervisor
    sudo systemctl start supervisor
elif command -v service &> /dev/null; then
    echo "设置 supervisor 开机自启..."
    sudo update-rc.d supervisor defaults
    sudo service supervisor start
fi

# 创建日志目录
sudo mkdir -p /var/log/trap-factory
sudo chown root:root /var/log/trap-factory

# 复制配置文件
sudo cp /tmp/trap-factory.conf /etc/supervisor/conf.d/trap-factory.conf

# 重新加载配置
sudo supervisorctl reread
sudo supervisorctl update

# 重启服务
sudo supervisorctl restart trap-factory || sudo supervisorctl start trap-factory

# 查看状态
echo ""
echo "服务状态:"
sudo supervisorctl status trap-factory

echo "✅ supervisor 配置已更新"
EOF

if [ $? -ne 0 ]; then
    echo "❌ supervisor 配置失败"
    exit 1
fi

echo ""
echo "=========================================="
echo "✅ 部署完成！"
echo "=========================================="
echo ""
echo "查看服务状态: ssh ${SERVER} 'sudo supervisorctl status trap-factory'"
echo "查看服务日志: ssh ${SERVER} 'sudo supervisorctl tail -f trap-factory'"
echo "停止服务: ssh ${SERVER} 'sudo supervisorctl stop trap-factory'"
echo "启动服务: ssh ${SERVER} 'sudo supervisorctl start trap-factory'"
echo ""
