# Worker 任务取消功能实现说明

## 概述

本项目实现了完整的任务取消机制，允许主系统通过 Redis 通知 Worker 取消正在处理的任务。

## 功能特性

### 1. 取消通知监听

Worker 启动后会自动订阅 `task_cancel_channel` 频道（Redis Pub/Sub），接收来自主系统的取消通知。

**优势**：
- 使用 Pub/Sub 机制，取消通知可以广播到所有 Worker
- 实时性更好，无需轮询
- 不会遗漏消息

### 2. 取消通知格式

```json
{
  "action": "cancel",
  "taskId": "task-uuid-12345",
  "timestamp": "2026-01-24T13:00:00Z"
}
```

**字段说明：**
- `action`: 操作类型，固定为 "cancel"
- `taskId`: 要取消的任务 ID
- `timestamp`: 取消请求的时间戳（ISO 8601 格式）

### 3. 取消处理流程

1. Worker 订阅 `task_cancel_channel` 频道
2. 主系统通过 `PUBLISH` 广播取消通知
3. 所有订阅的 Worker 同时收到通知
4. Worker 检查通知中的 taskId 是否与当前正在处理的任务匹配
5. 如果匹配，立即取消任务执行
6. 发送失败结果到 `address_consumer` 队列，状态为 "failed"，错误信息为 "任务已被用户取消"
7. Worker 恢复空闲状态，可以接收新任务

### 4. 取消时机

任务可以在以下时机被取消：

- **任务开始前**: 检查取消标志
- **任务执行中**:
  - 通过 `context.Cancel()` 中断执行
  - 通过 `Process.Kill()` 终止正在运行的 GPU 命令进程
- **任务执行后**: 检查取消标志

**命令进程终止**：当任务被取消时，Worker 会立即调用 `cmd.Process.Kill()` 终止正在运行的 GPU 命令进程，确保任务能够被真正终止，而不是继续在后台运行。

### 5. 线程安全

使用 `sync.RWMutex` 保证取消操作的线程安全，支持并发访问。

## 实现细节

### 核心数据结构

```go
// 取消通知消息结构
type CancelNotification struct {
    Action    string `json:"action"`
    TaskID    string `json:"taskId"`
    Timestamp string `json:"timestamp"`
}

// Worker 状态管理（新增取消相关字段）
type WorkerState struct {
    mu              sync.RWMutex
    workerId        string
    hostname        string
    status          WorkerStatus
    currentTaskID   *string
    currentTaskType *string
    isShuttingDown  bool
    cancelRequested bool              // 当前任务是否被请求取消
    taskCancelFunc  context.CancelFunc // 任务取消函数
    currentCmd      *exec.Cmd          // 当前正在执行的命令
}
```

### 核心方法

#### SetCurrentCmd

设置当前正在执行的命令：

```go
func (ws *WorkerState) SetCurrentCmd(cmd *exec.Cmd)
```

#### CancelCurrentTask

取消当前正在处理的任务：

```go
func (ws *WorkerState) CancelCurrentTask(taskID string) bool
```

- 检查 taskID 是否与当前任务匹配
- 如果匹配：
  1. 设置取消标志
  2. 终止正在运行的命令进程（`cmd.Process.Kill()`）
  3. 调用 context.CancelFunc
- 返回是否成功取消

**进程终止日志**：
```
终止正在运行的命令进程: PID=12345
任务已取消: TaskID=task-123
```

#### IsCancelRequested

检查当前任务是否被请求取消：

```go
func (ws *WorkerState) IsCancelRequested() bool
```

### 监听取消通知

Worker 启动时会创建一个 goroutine 订阅取消频道：

```go
go func() {
    // 创建 Pub/Sub 订阅
    pubsub := client.Subscribe(ctx, "task_cancel_channel")
    defer pubsub.Close()

    // 等待订阅确认
    _, err := pubsub.Receive(ctx)
    if err != nil {
        log.Printf("订阅取消频道失败: %v\n", err)
        return
    }
    log.Printf("已订阅任务取消频道: task_cancel_channel\n")

    // 获取消息频道
    ch := pubsub.Channel()

    for {
        select {
        case msg := <-ch:
            // 解析取消通知
            var cancelNotif CancelNotification
            json.Unmarshal([]byte(msg.Payload), &cancelNotif)

            // 处理取消请求
            if cancelNotif.Action == "cancel" {
                workerState.CancelCurrentTask(cancelNotif.TaskID)
            }

        case <-time.After(5 * time.Second):
            // 定期检查是否需要退出
            continue
        }
    }
}()
```

### 任务处理中的取消检查

```go
// 1. 任务开始前检查
if workerState.IsCancelRequested() {
    sendFailureResult(ctx, client, queueOutName, item.TaskID, "任务已被用户取消")
    return
}

// 2. 使用可取消的 context
taskCtx, taskCancel := context.WithTimeout(ctx, taskTimeout)
workerState.SetTaskCancelFunc(taskCancel)

// 3. 任务执行后检查
if workerState.IsCancelRequested() {
    sendFailureResult(ctx, client, queueOutName, item.TaskID, "任务已被用户取消")
    return
}

// 4. context 取消处理
case <-taskCtx.Done():
    if taskCtx.Err() == context.Canceled {
        if workerState.IsCancelRequested() {
            sendFailureResult(ctx, client, queueOutName, taskID, "任务已被用户取消")
        }
    }
```

## 使用示例

### 主系统发送取消通知

```javascript
// Node.js 示例
const cancelMessage = {
  action: 'cancel',
  taskId: 'task-uuid-12345',
  timestamp: new Date().toISOString(),
};

// 使用 PUBLISH 广播到所有 Worker
await redis.publish('task_cancel_channel', JSON.stringify(cancelMessage));
logger.info(`任务取消通知已广播到所有 Worker: ${taskId}`);
```

**Redis 命令行测试**：
```bash
redis-cli PUBLISH task_cancel_channel '{"action":"cancel","taskId":"test-123","timestamp":"2026-01-24T13:00:00Z"}'
```

### 取消结果

Worker 收到取消通知后，会发送失败结果到 `address_consumer` 队列：

```json
{
  "taskId": "task-uuid-12345",
  "status": "failed",
  "result": {
    "privateKey": "",
    "address": "",
    "totalGenerated": 0
  }
}
```

错误信息会包含 "任务已被用户取消"。

## 测试

### 运行取消功能测试

```bash
./scripts/test_cancel.sh
```

测试脚本会：
1. 启动 Worker
2. 发送一个长时间运行的任务（8a 类型）
3. 等待任务开始处理
4. 发送取消通知
5. 验证任务是否被成功取消
6. 检查 Worker 是否恢复空闲状态

### 手动测试

```bash
# 1. 启动 Worker
./factory-mac server

# 2. 发送测试任务
redis-cli LPUSH address_producer '{"taskId":"test-123","taskType":"8a","customFormat":""}'

# 3. 等待任务开始处理（查看日志）

# 4. 发送取消通知（使用 PUBLISH）
redis-cli PUBLISH task_cancel_channel '{"action":"cancel","taskId":"test-123","timestamp":"2026-01-24T13:00:00Z"}'

# 5. 查看结果
redis-cli LRANGE address_consumer 0 0
```

## 日志输出

### 正常取消流程

```
Worker 启动: worker-MacBook-Pro.local
已订阅任务取消频道: task_cancel_channel
收到任务: TaskID=test-123, TaskType=8a, CustomFormat=
心跳已发送: status=busy, taskId=test-123
执行命令: ./profanity.arm64 --matching ...
收到任务取消通知: TaskID=test-123
终止正在运行的命令进程: PID=12345
任务已取消: TaskID=test-123
任务取消成功: TaskID=test-123
任务已被取消: TaskID=test-123
失败结果已推送到队列: address_consumer, TaskID=test-123, Error=任务已被用户取消
心跳已发送: status=idle, taskId=<nil>
```

### 取消非当前任务

```
收到任务取消通知: TaskID=other-task
任务取消失败（可能不是当前任务）: TaskID=other-task
```

## 注意事项

1. **取消时机**: 只能取消当前正在处理的任务，已完成或未开始的任务无法取消
2. **取消延迟**: 取消操作可能需要几秒钟才能生效，取决于任务的执行状态
3. **GPU 任务**: 对于 GPU 计算任务，取消操作会通过 context 传递，并终止进程
4. **并发安全**: 所有取消操作都是线程安全的，可以在任何时候调用
5. **广播机制**: 使用 Pub/Sub 广播，所有 Worker 都会收到通知，但只有处理该任务的 Worker 会执行取消
6. **订阅确认**: Worker 启动时会等待订阅确认，确保不会遗漏消息

## 与心跳机制的集成

取消功能与心跳机制完美集成：

- 任务取消后，Worker 会立即发送 idle 心跳
- 主系统可以通过心跳监控任务取消的效果
- 取消操作不会影响心跳的正常发送

## 相关文档

- [HEARTBEAT_IMPLEMENTATION.md](./HEARTBEAT_IMPLEMENTATION.md) - Worker 心跳机制实现
- [README.md](./README.md) - 项目使用说明
