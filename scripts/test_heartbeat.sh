#!/bin/bash

# 心跳功能测试脚本

echo "=== Worker 心跳功能测试 ==="
echo ""

# 检查 Redis 是否运行
echo "1. 检查 Redis 连接..."
redis-cli ping > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "   ✓ Redis 连接正常"
else
    echo "   ✗ Redis 未运行，请先启动 Redis"
    exit 1
fi

# 清空心跳队列
echo ""
echo "2. 清空心跳队列..."
redis-cli DEL worker_heartbeat > /dev/null
echo "   ✓ 心跳队列已清空"

# 启动 Worker（后台运行）
echo ""
echo "3. 启动 Worker（后台运行 60 秒）..."
timeout 60s ./factory-mac server > /tmp/worker.log 2>&1 &
WORKER_PID=$!
echo "   ✓ Worker 已启动 (PID: $WORKER_PID)"

# 等待 5 秒让 Worker 初始化
echo ""
echo "4. 等待 5 秒让 Worker 初始化..."
sleep 5

# 检查心跳队列
echo ""
echo "5. 检查心跳队列..."
HEARTBEAT_COUNT=$(redis-cli LLEN worker_heartbeat)
echo "   心跳数量: $HEARTBEAT_COUNT"

if [ "$HEARTBEAT_COUNT" -gt 0 ]; then
    echo "   ✓ 心跳发送成功"
    echo ""
    echo "6. 查看最新心跳消息:"
    redis-cli LRANGE worker_heartbeat 0 0 | jq '.' 2>/dev/null || redis-cli LRANGE worker_heartbeat 0 0
else
    echo "   ✗ 未检测到心跳"
fi

# 等待 35 秒，应该会有第二次心跳
echo ""
echo "7. 等待 35 秒，检查定期心跳..."
sleep 35

HEARTBEAT_COUNT_2=$(redis-cli LLEN worker_heartbeat)
echo "   心跳数量: $HEARTBEAT_COUNT_2"

if [ "$HEARTBEAT_COUNT_2" -gt "$HEARTBEAT_COUNT" ]; then
    echo "   ✓ 定期心跳正常"
else
    echo "   ✗ 定期心跳异常"
fi

# 发送退出信号
echo ""
echo "8. 发送退出信号测试优雅退出..."
kill -TERM $WORKER_PID
sleep 3

# 检查是否有离线心跳
echo ""
echo "9. 检查离线心跳..."
LATEST_HEARTBEAT=$(redis-cli LRANGE worker_heartbeat 0 0)
echo "   最新心跳: $LATEST_HEARTBEAT"

if echo "$LATEST_HEARTBEAT" | grep -q "offline"; then
    echo "   ✓ 离线心跳发送成功"
else
    echo "   ⚠ 未检测到离线心跳（可能 Worker 已退出）"
fi

# 查看 Worker 日志
echo ""
echo "10. Worker 日志（最后 20 行）:"
echo "---"
tail -20 /tmp/worker.log
echo "---"

echo ""
echo "=== 测试完成 ==="
