#!/bin/bash

# 任务取消功能测试脚本

echo "=== Worker 任务取消功能测试 ==="
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

# 清空相关队列
echo ""
echo "2. 清空相关队列..."
redis-cli DEL address_producer > /dev/null
redis-cli DEL address_consumer > /dev/null
redis-cli DEL task_cancel_notifications > /dev/null
redis-cli DEL worker_heartbeat > /dev/null
echo "   ✓ 队列已清空"

# 启动 Worker（后台运行）
echo ""
echo "3. 启动 Worker（后台运行）..."
./factory-test server > /tmp/worker_cancel_test.log 2>&1 &
WORKER_PID=$!
echo "   ✓ Worker 已启动 (PID: $WORKER_PID)"

# 等待 3 秒让 Worker 初始化
echo ""
echo "4. 等待 3 秒让 Worker 初始化..."
sleep 3

# 发送一个测试任务（这个任务会运行很长时间）
echo ""
echo "5. 发送测试任务（8a 类型，会运行较长时间）..."
TASK_ID="test-task-$(date +%s)"
TASK_JSON=$(cat <<EOF
{
  "taskId": "$TASK_ID",
  "taskType": "8a",
  "customFormat": ""
}
EOF
)
redis-cli LPUSH address_producer "$TASK_JSON" > /dev/null
echo "   ✓ 任务已发送: TaskID=$TASK_ID"

# 等待 5 秒让任务开始处理
echo ""
echo "6. 等待 5 秒让任务开始处理..."
sleep 5

# 检查 Worker 状态
echo ""
echo "7. 检查 Worker 状态..."
LATEST_HEARTBEAT=$(redis-cli LRANGE worker_heartbeat 0 0)
echo "   最新心跳: $LATEST_HEARTBEAT"

if echo "$LATEST_HEARTBEAT" | grep -q "busy"; then
    echo "   ✓ Worker 正在处理任务"
else
    echo "   ⚠ Worker 可能未开始处理任务"
fi

# 发送取消通知（使用 PUBLISH）
echo ""
echo "8. 发送任务取消通知（使用 Pub/Sub）..."
CANCEL_JSON=$(cat <<EOF
{
  "action": "cancel",
  "taskId": "$TASK_ID",
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
}
EOF
)
redis-cli PUBLISH task_cancel_channel "$CANCEL_JSON" > /dev/null
echo "   ✓ 取消通知已广播"

# 等待 3 秒让取消生效
echo ""
echo "9. 等待 3 秒让取消生效..."
sleep 3

# 检查任务结果
echo ""
echo "10. 检查任务结果..."
RESULT=$(redis-cli LRANGE address_consumer 0 0)
if [ -n "$RESULT" ]; then
    echo "   任务结果: $RESULT"
    if echo "$RESULT" | grep -q "failed"; then
        echo "   ✓ 任务已标记为失败"
        if echo "$RESULT" | grep -q "取消"; then
            echo "   ✓ 失败原因包含'取消'关键字"
        fi
    else
        echo "   ⚠ 任务状态异常"
    fi
else
    echo "   ⚠ 未找到任务结果"
fi

# 检查 Worker 状态
echo ""
echo "11. 检查 Worker 当前状态..."
sleep 2
LATEST_HEARTBEAT=$(redis-cli LRANGE worker_heartbeat 0 0)
echo "   最新心跳: $LATEST_HEARTBEAT"

if echo "$LATEST_HEARTBEAT" | grep -q "idle"; then
    echo "   ✓ Worker 已恢复空闲状态"
else
    echo "   ⚠ Worker 状态异常"
fi

# 停止 Worker
echo ""
echo "12. 停止 Worker..."
kill -TERM $WORKER_PID
sleep 2

# 查看 Worker 日志
echo ""
echo "13. Worker 日志（最后 30 行）:"
echo "---"
tail -30 /tmp/worker_cancel_test.log
echo "---"

echo ""
echo "=== 测试完成 ==="
