# Worker 心跳功能实现说明

## 概述

本项目已完整实现了 Worker 心跳机制，符合 [WORKER_HEARTBEAT_INTEGRATION.md](https://github.com/your-repo/docs/WORKER_HEARTBEAT_INTEGRATION.md) 文档的要求。

## 实现的功能

### 1. Worker 标识

每个 Worker 实例都有唯一的标识：

- **workerId**: 格式为 `worker-{hostname}`（例如：`worker-MacBook-Pro.local`）
- **hostname**: 从系统获取的主机名

**优点**：
- 简单直接
- 重启后 ID 保持不变
- 适合每台机器只运行一个 Worker
- 便于识别和管理

### 2. 心跳机制

- **心跳频率**: 30 秒一次
- **心跳队列**: `worker_heartbeat`（Redis List）
- **心跳消息格式**: JSON 格式，包含以下字段：
  - `workerId`: Worker 唯一标识
  - `hostname`: 主机名
  - `status`: 状态（idle/busy/offline）
  - `currentTaskId`: 当前处理的任务 ID（busy 时有值）
  - `currentTaskType`: 当前任务类型（busy 时有值）
  - `timestamp`: ISO 8601 格式时间戳

### 3. Worker 状态

- `idle`: 空闲（在线但未处理任务）
- `busy`: 忙碌（正在处理任务）
- `offline`: 离线（准备退出）

### 4. 状态同步

- Worker 启动时发送初始心跳（状态为 idle）
- 每 30 秒自动发送心跳
- 开始处理任务时立即发送心跳（状态为 busy）
- 任务完成后发送心跳（状态为 idle）
- 退出前发送离线心跳（状态为 offline）

### 5. 优雅退出

- 捕获 SIGTERM 和 SIGINT 信号
- 停止接收新任务
- 等待当前任务完成（最多 30 秒）
- 发送离线心跳
- 关闭 Redis 连接
- 安全退出

## 代码结构

### 核心数据结构

```go
// Worker 状态类型
type WorkerStatus string

const (
    StatusIdle    WorkerStatus = "idle"
    StatusBusy    WorkerStatus = "busy"
    StatusOffline WorkerStatus = "offline"
)

// 心跳消息
type Heartbeat struct {
    WorkerID        string       `json:"workerId"`
    Hostname        string       `json:"hostname"`
    Status          WorkerStatus `json:"status"`
    CurrentTaskID   *string      `json:"currentTaskId"`
    CurrentTaskType *string      `json:"currentTaskType"`
    Timestamp       string       `json:"timestamp"`
}

// Worker 状态管理（线程安全）
type WorkerState struct {
    mu              sync.RWMutex
    workerId        string
    hostname        string
    status          WorkerStatus
    currentTaskID   *string
    currentTaskType *string
    isShuttingDown  bool
}
```

### 核心函数

#### sendHeartbeat

发送心跳消息到 Redis 队列：

```go
func sendHeartbeat(ctx context.Context, client *redis.Client, workerState *WorkerState) error
```

#### WorkerState 方法

- `SetStatus(status, taskID, taskType)`: 更新 Worker 状态
- `GetStatus()`: 获取当前状态
- `SetShuttingDown(value)`: 设置关闭标志
- `IsShuttingDown()`: 检查是否正在关闭

## 使用示例

### 启动 Worker

```bash
./factory-mac server
```

### 心跳消息示例

**空闲状态**:
```json
{
  "workerId": "worker-MacBook-Pro.local",
  "hostname": "MacBook-Pro.local",
  "status": "idle",
  "currentTaskId": null,
  "currentTaskType": null,
  "timestamp": "2026-01-24T13:00:00Z"
}
```

**忙碌状态**:
```json
{
  "workerId": "worker-MacBook-Pro.local",
  "hostname": "MacBook-Pro.local",
  "status": "busy",
  "currentTaskId": "task-uuid-12345",
  "currentTaskType": "5a",
  "timestamp": "2026-01-24T13:01:00Z"
}
```

**离线状态**:
```json
{
  "workerId": "worker-MacBook-Pro.local",
  "hostname": "MacBook-Pro.local",
  "status": "offline",
  "currentTaskId": null,
  "currentTaskType": null,
  "timestamp": "2026-01-24T13:05:00Z"
}
```

## 监控和调试

### 查看心跳队列

```bash
# 查看心跳队列长度
redis-cli LLEN worker_heartbeat

# 查看最新心跳
redis-cli LRANGE worker_heartbeat 0 0

# 查看最近 10 条心跳
redis-cli LRANGE worker_heartbeat 0 9
```

### 日志输出

Worker 运行时会输出以下日志：

```
Worker 启动: worker-MacBook-Pro.local
Redis 连接成功
开始监听队列: address_producer
心跳已发送: status=idle, taskId=<nil>
收到任务: TaskID=task-123, TaskType=5a, CustomFormat=
心跳已发送: status=busy, taskId=task-123
地址生成成功: TaskID=task-123, ...
心跳已发送: status=idle, taskId=<nil>
```

### 优雅退出日志

```
收到退出信号: interrupt, 准备优雅退出...
当前无任务处理，可以安全退出
心跳已发送: status=offline, taskId=<nil>
Worker 已安全退出
```

## 与主系统对接

本 Worker 实现完全符合主系统的心跳对接要求：

1. ✅ 使用 `worker_heartbeat` 队列
2. ✅ 心跳频率 30 秒
3. ✅ 包含完整的状态信息
4. ✅ 任务开始/结束时立即发送心跳
5. ✅ 优雅退出机制
6. ✅ 线程安全的状态管理

## 配置说明

心跳功能使用 `config.env` 中的 Redis 配置：

```env
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_POOL_SIZE=10
REDIS_MIN_IDLE_CONNS=5
REDIS_DIAL_TIMEOUT=5s
REDIS_READ_TIMEOUT=3s
REDIS_WRITE_TIMEOUT=3s
```

## 注意事项

1. **线程安全**: 使用 `sync.RWMutex` 保证状态更新的线程安全
2. **错误处理**: 心跳发送失败不会影响任务处理，只会记录日志
3. **优雅退出**: 最多等待 30 秒让当前任务完成
4. **自动重连**: Redis 连接失败时会自动重试

## 相关文档

- [WORKER_HEARTBEAT_INTEGRATION.md](https://github.com/your-repo/docs/WORKER_HEARTBEAT_INTEGRATION.md) - 主系统心跳对接文档
