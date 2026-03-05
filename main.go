package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
)

//go:embed config.env
var envContent string

// 任务处理超时时间（GPU 计算可能需要较长时间）
var taskTimeout = 168 * time.Hour

// Worker 状态
type WorkerStatus string

const (
	StatusIdle    WorkerStatus = "idle"
	StatusBusy    WorkerStatus = "busy"
	StatusOffline WorkerStatus = "offline"
)

// 心跳消息结构
type Heartbeat struct {
	WorkerID        string       `json:"workerId"`
	Hostname        string       `json:"hostname"`
	Status          WorkerStatus `json:"status"`
	CurrentTaskID   *string      `json:"currentTaskId"`
	CurrentTaskType *string      `json:"currentTaskType"`
	Timestamp       string       `json:"timestamp"`
}

// 取消通知消息结构
type CancelNotification struct {
	Action    string `json:"action"`
	TaskID    string `json:"taskId"`
	Timestamp string `json:"timestamp"`
}

// Worker 状态管理
type WorkerState struct {
	mu              sync.RWMutex
	workerId        string
	hostname        string
	status          WorkerStatus
	currentTaskID   *string
	currentTaskType *string
	isShuttingDown  bool
	cancelRequested bool               // 当前任务是否被请求取消
	taskCancelFunc  context.CancelFunc // 任务取消函数
	currentCmd      *exec.Cmd          // 当前正在执行的命令
}

func (ws *WorkerState) SetStatus(status WorkerStatus, taskID *string, taskType *string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.status = status
	ws.currentTaskID = taskID
	ws.currentTaskType = taskType
	// 切换任务时重置取消标志
	if taskID == nil {
		ws.cancelRequested = false
		ws.taskCancelFunc = nil
	}
}

func (ws *WorkerState) GetStatus() (WorkerStatus, *string, *string) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.status, ws.currentTaskID, ws.currentTaskType
}

func (ws *WorkerState) SetShuttingDown(value bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.isShuttingDown = value
}

func (ws *WorkerState) IsShuttingDown() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.isShuttingDown
}

func (ws *WorkerState) SetTaskCancelFunc(cancelFunc context.CancelFunc) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.taskCancelFunc = cancelFunc
}

func (ws *WorkerState) SetCurrentCmd(cmd *exec.Cmd) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.currentCmd = cmd
}

func (ws *WorkerState) CancelCurrentTask(taskID string) bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	// 检查是否是当前任务
	if ws.currentTaskID != nil && *ws.currentTaskID == taskID {
		ws.cancelRequested = true

		// 终止正在运行的命令
		if ws.currentCmd != nil && ws.currentCmd.Process != nil {
			log.Printf("终止正在运行的命令进程: PID=%d\n", ws.currentCmd.Process.Pid)
			if err := ws.currentCmd.Process.Kill(); err != nil {
				log.Printf("终止命令进程失败: %v\n", err)
			}
		}

		// 取消 context
		if ws.taskCancelFunc != nil {
			ws.taskCancelFunc()
			log.Printf("任务已取消: TaskID=%s\n", taskID)
			return true
		}
	}
	return false
}

func (ws *WorkerState) IsCancelRequested() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.cancelRequested
}

type Item struct {
	TaskID       string `json:"taskId"`
	TaskType     string `json:"taskType"`     // "5a", "6a", "7a", "8a", "custom_address"
	CustomFormat string `json:"customFormat"` // 例如 "TTTT-TTTT"
}

// sendHeartbeat 发送心跳到 Redis 队列
func sendHeartbeat(ctx context.Context, client *redis.Client, workerState *WorkerState) error {
	status, taskID, taskType := workerState.GetStatus()

	heartbeat := Heartbeat{
		WorkerID:        workerState.workerId,
		Hostname:        workerState.hostname,
		Status:          status,
		CurrentTaskID:   taskID,
		CurrentTaskType: taskType,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
	}

	jsonBytes, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("序列化心跳消息失败: %v", err)
	}

	if err := client.LPush(ctx, "worker_heartbeat", string(jsonBytes)).Err(); err != nil {
		return fmt.Errorf("推送心跳到队列失败: %v", err)
	}

	// 格式化 taskID 用于日志输出
	taskIDStr := "<nil>"
	if taskID != nil {
		taskIDStr = *taskID
	}
	log.Printf("心跳已发送: status=%s, taskId=%s\n", status, taskIDStr)
	return nil
}

type TaskResult struct {
	PrivateKey     string `json:"privateKey"`
	Address        string `json:"address"`
	TotalGenerated int64  `json:"totalGenerated"`
}

type ItemReply struct {
	TaskID string     `json:"taskId"`
	Status string     `json:"status"` // "completed", "failed"
	Result TaskResult `json:"result,omitempty"`
}

// sendFailureResult 发送失败结果到队列
func sendFailureResult(ctx context.Context, client *redis.Client, queueOutName string, taskID string, errorMsg string) {
	itemReply := ItemReply{
		TaskID: taskID,
		Status: "failed",
		Result: TaskResult{
			PrivateKey:     "",
			Address:        "",
			TotalGenerated: 0,
		},
	}

	jsonBytes, err := json.Marshal(itemReply)
	if err != nil {
		log.Printf("序列化失败结果失败: %v\n", err)
		return
	}

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		if err := client.LPush(ctx, queueOutName, string(jsonBytes)).Err(); err != nil {
			log.Printf("推送失败结果到队列失败 (尝试 %d/%d): %v\n", i+1, maxRetries, err)
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * time.Second)
			}
		} else {
			log.Printf("失败结果已推送到队列: %s, TaskID=%s, Error=%s\n", queueOutName, taskID, errorMsg)
			break
		}
	}
}

// RedisConfig Redis 连接配置
type RedisConfig struct {
	ID           int           // Redis 实例编号
	Addr         string        // Redis 地址
	Password     string        // Redis 密码
	DB           int           // Redis 数据库
	PoolSize     int           // 连接池大小
	MinIdleConns int           // 最小空闲连接数
	DialTimeout  time.Duration // 连接超时
	ReadTimeout  time.Duration // 读取超时
	WriteTimeout time.Duration // 写入超时
}

// RedisClientWrapper 封装 Redis 客户端及其配置
type RedisClientWrapper struct {
	Config *RedisConfig
	Client *redis.Client
}

// loadEnvConfig 从嵌入的 env 内容中加载配置
func loadEnvConfig() map[string]string {
	config := make(map[string]string)
	lines := strings.Split(envContent, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			config[key] = value
		}
	}
	return config
}

// parseRedisConfigs 从配置中解析多个 Redis 配置
func parseRedisConfigs(config map[string]string) []*RedisConfig {
	redisConfigs := make(map[int]*RedisConfig)

	// 遍历配置，找出所有 REDIS_<N>_* 的配置项
	for key, value := range config {
		if !strings.HasPrefix(key, "REDIS_") {
			continue
		}

		// 解析格式：REDIS_<N>_<FIELD>
		parts := strings.Split(key, "_")
		if len(parts) < 3 {
			continue
		}

		// 获取 Redis 实例编号
		id, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		// 确保该 ID 的配置对象存在
		if _, exists := redisConfigs[id]; !exists {
			redisConfigs[id] = &RedisConfig{
				ID:           id,
				PoolSize:     10,
				MinIdleConns: 5,
				DialTimeout:  5 * time.Second,
				ReadTimeout:  3 * time.Second,
				WriteTimeout: 3 * time.Second,
			}
		}

		// 解析配置字段
		field := strings.Join(parts[2:], "_")
		switch field {
		case "ADDR":
			redisConfigs[id].Addr = value
		case "PASSWORD":
			redisConfigs[id].Password = value
		case "DB":
			if db, err := strconv.Atoi(value); err == nil {
				redisConfigs[id].DB = db
			}
		case "POOL_SIZE":
			if ps, err := strconv.Atoi(value); err == nil {
				redisConfigs[id].PoolSize = ps
			}
		case "MIN_IDLE_CONNS":
			if mic, err := strconv.Atoi(value); err == nil {
				redisConfigs[id].MinIdleConns = mic
			}
		case "DIAL_TIMEOUT":
			redisConfigs[id].DialTimeout = parseDuration(value)
		case "READ_TIMEOUT":
			redisConfigs[id].ReadTimeout = parseDuration(value)
		case "WRITE_TIMEOUT":
			redisConfigs[id].WriteTimeout = parseDuration(value)
		}
	}

	// 转换为切片并按 ID 排序
	var result []*RedisConfig
	for i := 1; i <= len(redisConfigs)+10; i++ { // +10 防止编号不连续
		if cfg, exists := redisConfigs[i]; exists {
			if cfg.Addr != "" { // 只添加配置了地址的实例
				result = append(result, cfg)
			}
		}
	}

	return result
}

// parseDuration 解析时间字符串，支持 "5s", "3s" 等格式
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("解析时间失败 %s，使用默认值: %v\n", s, err)
		return 5 * time.Second
	}
	return d
}

// checkGPUEnvironment 检查 GPU 环境是否正确配置
// processTask 处理单个任务
func processTask(ctx context.Context, client *redis.Client, workerState *WorkerState, item Item, queueOutName string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("任务处理发生 panic: %v\n", r)
			sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("panic: %v", r))
		}
		workerState.SetStatus(StatusIdle, nil, nil)
		sendHeartbeat(ctx, client, workerState)
	}()

	workerState.SetStatus(StatusBusy, &item.TaskID, &item.TaskType)
	workerState.SetTaskCancelFunc(nil)
	if err := sendHeartbeat(ctx, client, workerState); err != nil {
		log.Printf("发送忙碌心跳失败: %v\n", err)
	}

	var prefixCount, suffixCount string
	var matchingAddress string

	if item.CustomFormat != "" {
		prefixStr, suffixStr, err := parseCustomFormat(item.CustomFormat)
		if err != nil {
			log.Printf("解析 customFormat 失败: %v\n", err)
			sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("解析格式失败: %v", err))
			return
		}
		prefixCount = strconv.Itoa(len(prefixStr))
		suffixCount = strconv.Itoa(len(suffixStr))
		matchingAddress = prefixStr
		for i := 0; i < 34-len(prefixStr)-len(suffixStr); i++ {
			matchingAddress += "X"
		}
		matchingAddress += suffixStr
	} else {
		switch item.TaskType {
		case "5a", "6a", "7a", "8a":
			suffixLen, err := strconv.Atoi(string(item.TaskType[0]))
			if err != nil {
				log.Printf("解析 taskType 失败: %v\n", err)
				sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("解析 taskType 失败: %v", err))
				return
			}
			suffixCount = strconv.Itoa(suffixLen)
			prefixCount = "0"
			matchingAddress = "./profanity.txt"
			if _, err := os.Stat(matchingAddress); os.IsNotExist(err) {
				log.Printf("靓号模板文件不存在\n")
				sendFailureResult(ctx, client, queueOutName, item.TaskID, "靓号模板文件不存在")
				return
			}

		case "custom_address":
			log.Printf("错误: taskType=custom_address 但未提供 customFormat\n")
			sendFailureResult(ctx, client, queueOutName, item.TaskID, "taskType=custom_address 必须提供 customFormat 字段")
			return

		default:
			log.Printf("未知的 taskType: %s\n", item.TaskType)
			sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("未知的 taskType: %s", item.TaskType))
			return
		}
	}

	log.Printf("生成地址参数: matching=%s, prefixCount=%s, suffixCount=%s\n", matchingAddress, prefixCount, suffixCount)

	if workerState.IsCancelRequested() {
		log.Printf("任务已被取消: TaskID=%s\n", item.TaskID)
		sendFailureResult(ctx, client, queueOutName, item.TaskID, "任务已被用户取消")
		return
	}

	taskCtx, taskCancel := context.WithTimeout(ctx, taskTimeout)
	defer taskCancel()
	workerState.SetTaskCancelFunc(taskCancel)

	privateKey, addr, totalGenerated, err := generateAddressByGPU(taskCtx, workerState, matchingAddress, prefixCount, suffixCount, "1")
	if err != nil {
		if workerState.IsCancelRequested() {
			log.Printf("任务已被取消: TaskID=%s\n", item.TaskID)
			sendFailureResult(ctx, client, queueOutName, item.TaskID, "任务已被用户取消")
			return
		}
		log.Printf("生成地址失败: TaskID=%s, 错误: %v\n", item.TaskID, err)
		sendFailureResult(ctx, client, queueOutName, item.TaskID, err.Error())
		return
	}

	log.Printf("地址生成成功: TaskID=%s, %s --> %s, 生成数量: %d\n", item.TaskID, matchingAddress, addr, totalGenerated)

	itemReply := ItemReply{
		TaskID: item.TaskID,
		Status: "completed",
		Result: TaskResult{
			PrivateKey:     privateKey,
			Address:        addr,
			TotalGenerated: totalGenerated,
		},
	}

	jsonBytes, err := json.Marshal(itemReply)
	if err != nil {
		log.Printf("序列化结果失败: %v\n", err)
		sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("序列化失败: %v", err))
		return
	}

	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		if err := client.LPush(ctx, queueOutName, string(jsonBytes)).Err(); err != nil {
			log.Printf("推送结果到队列失败 (尝试 %d/%d): %v\n", i+1, maxRetries, err)
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * time.Second)
			}
		} else {
			log.Printf("结果已推送到队列: %s, TaskID=%s\n", queueOutName, item.TaskID)
			break
		}
	}
}

func checkGPUEnvironment() error {
	log.Printf("检查 GPU 环境...\n")

	// 1. 检查 clinfo 是否安装
	cmd := exec.Command("which", "clinfo")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clinfo 未安装，请先安装 OpenCL 工具:\n  apt update && apt install -y ocl-icd-libopencl1 clinfo")
	}
	log.Printf("✓ clinfo 已安装\n")

	// 2. 检查是否能检测到 GPU 设备
	cmd = exec.Command("clinfo")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("运行 clinfo 失败: %v\n输出: %s", err, string(output))
	}

	// 检查输出中是否包含 GPU 设备信息
	outputStr := string(output)
	if !strings.Contains(outputStr, "Device Name") && !strings.Contains(outputStr, "DEVICE_NAME") {
		return fmt.Errorf("未检测到 GPU 设备，请确认:\n  1. GPU 驱动已正确安装\n  2. OpenCL 环境已配置\n  3. 运行 'clinfo' 查看详细信息")
	}

	// 提取并显示 GPU 设备名称
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Device Name") || strings.Contains(line, "DEVICE_NAME") {
			log.Printf("✓ 检测到 GPU: %s\n", strings.TrimSpace(line))
			break
		}
	}

	log.Printf("✓ GPU 环境检查通过\n")
	return nil
}

// handleRedisWorker 处理单个 Redis 连接的任务
func handleRedisWorker(ctx context.Context, wrapper *RedisClientWrapper, workerState *WorkerState, wg *sync.WaitGroup) {
	defer wg.Done()

	client := wrapper.Client
	cfg := wrapper.Config
	queueInName := "address_producer"
	queueOutName := "address_consumer"

	log.Printf("[Redis-%d] 开始监听队列: %s (地址: %s)\n", cfg.ID, queueInName, cfg.Addr)

	for {
		if workerState.IsShuttingDown() {
			log.Printf("[Redis-%d] Worker 正在关闭，停止接收新任务\n", cfg.ID)
			break
		}

		result, err := client.BRPop(ctx, 5*time.Second, queueInName).Result()
		if err == redis.Nil {
			continue
		} else if err != nil {
			log.Printf("[Redis-%d] 从队列获取任务失败: %v，5秒后重试\n", cfg.ID, err)
			time.Sleep(5 * time.Second)
			if err := client.Ping(ctx).Err(); err != nil {
				log.Printf("[Redis-%d] Redis 连接失败: %v，继续重试\n", cfg.ID, err)
			}
			continue
		}

		if len(result) < 2 {
			log.Printf("[Redis-%d] 从队列获取的数据格式错误: %v\n", cfg.ID, result)
			continue
		}
		jsonStr := result[1]

		var item Item
		if err := json.Unmarshal([]byte(jsonStr), &item); err != nil {
			log.Printf("[Redis-%d] 解析任务 JSON 失败: %v，任务数据: %s\n", cfg.ID, err, jsonStr)
			continue
		}

		log.Printf("[Redis-%d] 收到任务: TaskID=%s, TaskType=%s, CustomFormat=%s\n", cfg.ID, item.TaskID, item.TaskType, item.CustomFormat)

		isCancelled, err := client.SIsMember(ctx, "cancelled_tasks", item.TaskID).Result()
		if err != nil {
			log.Printf("[Redis-%d] 检查任务取消状态失败: %v\n", cfg.ID, err)
		} else if isCancelled {
			log.Printf("[Redis-%d] ⚠️  任务已被取消，跳过处理: TaskID=%s\n", cfg.ID, item.TaskID)
			continue
		}

		processTask(ctx, client, workerState, item, queueOutName)
	}

	log.Printf("[Redis-%d] Worker 已停止\n", cfg.ID)
}

func server() {
	if runtime.GOOS == "darwin" {
		log.Printf("⚠️  macOS 开发环境，跳过 GPU 环境检查\n")
	} else {
		if err := checkGPUEnvironment(); err != nil {
			log.Printf("❌ GPU 环境检查失败:\n%v\n", err)
			log.Printf("\n请按照以下步骤配置环境:\n")
			log.Printf("1. 安装 OpenCL 工具:\n")
			log.Printf("   apt update && apt install -y ocl-icd-libopencl1 clinfo\n")
			log.Printf("2. 验证 GPU:\n")
			log.Printf("   clinfo | grep -i \"device name\"\n")
			log.Printf("3. 如果看到 GPU 信息（如 NVIDIA GeForce RTX 4090），说明环境已配置成功\n")
			log.Fatal("程序退出")
		}
	}

	config := loadEnvConfig()
	redisConfigs := parseRedisConfigs(config)

	if len(redisConfigs) == 0 {
		log.Fatal("未找到任何 Redis 配置，请检查 config.env 文件")
	}

	log.Printf("✅ 找到 %d 个 Redis 配置\n", len(redisConfigs))

	ctx := context.Background()
	var redisClients []*RedisClientWrapper

	for _, cfg := range redisConfigs {
		client := redis.NewClient(&redis.Options{
			Addr:         cfg.Addr,
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
		})

		if err := client.Ping(ctx).Err(); err != nil {
			log.Printf("❌ Redis-%d 连接失败: %s, 错误: %v\n", cfg.ID, cfg.Addr, err)
			client.Close()
			continue
		}

		log.Printf("✅ Redis-%d 连接成功: %s (DB: %d)\n", cfg.ID, cfg.Addr, cfg.DB)
		redisClients = append(redisClients, &RedisClientWrapper{
			Config: cfg,
			Client: client,
		})
	}

	if len(redisClients) == 0 {
		log.Fatal("所有 Redis 连接均失败，程序退出")
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("获取主机名失败: %v, 使用默认值\n", err)
		hostname = "unknown"
	}
	workerId := fmt.Sprintf("worker-%s", hostname)

	workerState := &WorkerState{
		workerId: workerId,
		hostname: hostname,
		status:   StatusIdle,
	}

	log.Printf("Worker 启动: %s\n", workerId)
	log.Printf("✅ 成功连接 %d 个 Redis 实例，开始并发处理任务\n", len(redisClients))

	for _, wrapper := range redisClients {
		if err := sendHeartbeat(ctx, wrapper.Client, workerState); err != nil {
			log.Printf("[Redis-%d] 发送初始心跳失败: %v\n", wrapper.Config.ID, err)
		}
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			for _, wrapper := range redisClients {
				if err := sendHeartbeat(ctx, wrapper.Client, workerState); err != nil {
					log.Printf("[Redis-%d] 发送心跳失败: %v\n", wrapper.Config.ID, err)
				}
			}
		}
	}()

	for _, wrapper := range redisClients {
		go func(w *RedisClientWrapper) {
			pubsub := w.Client.Subscribe(ctx, "task_cancel_channel")
			defer pubsub.Close()

			_, err := pubsub.Receive(ctx)
			if err != nil {
				log.Printf("[Redis-%d] 订阅取消频道失败: %v\n", w.Config.ID, err)
				return
			}
			log.Printf("[Redis-%d] 已订阅任务取消频道: task_cancel_channel\n", w.Config.ID)

			ch := pubsub.Channel()

			for {
				if workerState.IsShuttingDown() {
					break
				}

				select {
				case msg := <-ch:
					if msg == nil {
						continue
					}

					var cancelNotif CancelNotification
					if err := json.Unmarshal([]byte(msg.Payload), &cancelNotif); err != nil {
						log.Printf("[Redis-%d] 解析取消通知失败: %v, 数据: %s\n", w.Config.ID, err, msg.Payload)
						continue
					}

					if cancelNotif.Action == "cancel" {
						log.Printf("[Redis-%d] 收到任务取消通知: TaskID=%s\n", w.Config.ID, cancelNotif.TaskID)
						if workerState.CancelCurrentTask(cancelNotif.TaskID) {
							log.Printf("[Redis-%d] 任务取消成功: TaskID=%s\n", w.Config.ID, cancelNotif.TaskID)
						} else {
							log.Printf("[Redis-%d] 任务取消失败（可能不是当前任务）: TaskID=%s\n", w.Config.ID, cancelNotif.TaskID)
						}
					}

				case <-time.After(5 * time.Second):
					continue
				}
			}
		}(wrapper)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigChan
		log.Printf("收到退出信号: %v, 准备优雅退出...\n", sig)

		workerState.SetShuttingDown(true)

		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				log.Printf("等待任务完成超时，强制退出\n")
				goto exit
			case <-ticker.C:
				status, _, _ := workerState.GetStatus()
				if status != StatusBusy {
					log.Printf("当前无任务处理，可以安全退出\n")
					goto exit
				}
				log.Printf("等待当前任务完成...\n")
			}
		}

	exit:
		workerState.SetStatus(StatusOffline, nil, nil)
		for _, wrapper := range redisClients {
			if err := sendHeartbeat(ctx, wrapper.Client, workerState); err != nil {
				log.Printf("[Redis-%d] 发送离线心跳失败: %v\n", wrapper.Config.ID, err)
			}
			if err := wrapper.Client.Close(); err != nil {
				log.Printf("[Redis-%d] 关闭 Redis 连接失败: %v\n", wrapper.Config.ID, err)
			}
		}

		log.Printf("Worker 已安全退出\n")
		os.Exit(0)
	}()

	var wg sync.WaitGroup
	for _, wrapper := range redisClients {
		wg.Add(1)
		go handleRedisWorker(ctx, wrapper, workerState, &wg)
	}

	wg.Wait()
	log.Printf("所有 Redis Worker 已停止\n")
}

func extractValues(input string) (privateKey, address string, totalGenerated int64) {
	re := regexp.MustCompile(`Private: ([a-fA-F0-9]+) Address:([a-zA-Z0-9]+)`)
	matches := re.FindStringSubmatch(input)

	if len(matches) == 3 {
		privateKey = matches[1]
		address = matches[2]
	}

	// 从速度和运行时间计算生成数量
	// 格式: "Time: 8s" 和 "Total: 15.619 MH/s"
	// 提取时间（秒）
	timeRe := regexp.MustCompile(`Time:\s*(\d+)s`)
	timeMatches := timeRe.FindStringSubmatch(input)

	// 提取速度（MH/s）
	speedRe := regexp.MustCompile(`Total:\s*([\d.]+)\s*MH/s`)
	speedMatches := speedRe.FindStringSubmatch(input)

	if len(timeMatches) == 2 && len(speedMatches) == 2 {
		if timeSeconds, err := strconv.ParseInt(timeMatches[1], 10, 64); err == nil {
			if speedMH, err := strconv.ParseFloat(speedMatches[1], 64); err == nil {
				// 计算生成数量 = 速度(MH/s) × 时间(秒) × 10^6
				totalGenerated = int64(speedMH * float64(timeSeconds) * 1000000)
			}
		}
	}

	return privateKey, address, totalGenerated
}

// parseCustomFormat 解析自定义格式，例如 "TABC-8888" 返回前缀字符串、后缀字符串
// 前缀的第一个字符必须是 T（Tron 地址要求），其他字符可以是任意值
// 后缀可以是任意字符
func parseCustomFormat(format string) (prefix string, suffix string, err error) {
	parts := regexp.MustCompile(`-`).Split(format, -1)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("格式错误，应为 '前缀-后缀' 格式，例如 'TABC-8888'")
	}

	prefix = parts[0]
	suffix = parts[1]

	if len(prefix) == 0 || len(suffix) == 0 {
		return "", "", fmt.Errorf("前缀或后缀不能为空")
	}

	// 检查前缀的第一个字符必须是 T（Tron 地址必须以 T 开头）
	if prefix[0] != 'T' {
		return "", "", fmt.Errorf("前缀的第一个字符必须是 T（Tron 地址要求）")
	}

	return prefix, suffix, nil
}

func generateAddressByGPU(ctx context.Context, workerState *WorkerState, address string, prefixCount string, suffixCount string, quictCount string) (string, string, int64, error) {
	// log.Printf("Generating address for %s, prefixCount=%s, suffixCount=%s\n", address, prefixCount, suffixCount)
	var exec_file string
	switch runtime.GOOS {
	case "darwin":
		exec_file = "./profanity.arm64"
	case "windows":
		exec_file = "./profanity.exe"
	default:
		exec_file = "./profanity.x64"
	}

	// 检查可执行文件是否存在
	if _, err := os.Stat(exec_file); os.IsNotExist(err) {
		return "", "", 0, fmt.Errorf("可执行文件不存在: %s", exec_file)
	}

	log.Printf("执行命令: %s --matching %s --prefix-count %s --suffix-count %s --quit-count %s", exec_file, address, prefixCount, suffixCount, quictCount)

	// 使用 CommandContext 创建可取消的命令
	cmd := exec.CommandContext(ctx, exec_file, "--matching", address, "--prefix-count", prefixCount, "--suffix-count", suffixCount, "--quit-count", quictCount)

	// 保存命令引用到 workerState
	if workerState != nil {
		workerState.SetCurrentCmd(cmd)
		defer workerState.SetCurrentCmd(nil)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// 检查是否是因为取消导致的错误
		if ctx.Err() == context.Canceled {
			return "", "", 0, fmt.Errorf("命令已被取消")
		}
		return "", "", 0, fmt.Errorf("执行命令失败: %v, 输出: %s", err, string(output))
	}

	// 调试：打印实际输出（可以注释掉用于生产环境）
	// log.Printf("Profanity 原始输出: %q\n", string(output))

	privateKey, addr, totalGenerated := extractValues(string(output))
	if privateKey == "" || addr == "" {
		return "", "", 0, fmt.Errorf("无法从输出中提取私钥和地址，输出: %s", string(output))
	}

	return privateKey, addr, totalGenerated, nil
}

func showHelp() {
	programName := os.Args[0]
	helpText := fmt.Sprintf(`
Tron 靓号生成工具

使用方法:
  %s <command> [arguments]

可用命令:
  server                          启动服务器模式，从 Redis 队列获取任务并处理
  build <fromAddress> <prefixCount> <suffixCount> <quitCount>
                                  直接生成靓号地址
                                  参数说明:
                                    fromAddress:  要匹配的目标地址模板
                                    prefixCount:  地址前缀需要匹配的字符数
                                    suffixCount:  地址后缀需要匹配的字符数
                                    quitCount:    生成多少个匹配的地址后退出
  help, -h, --help                显示此帮助信息

示例:
  # 启动服务器模式
  %s server

  # 生成第一位是 T, 后三位是 888 的靓号, 生成 1 个匹配的地址后退出
  %s build TTTCqtavqZiKEMVYgEQSN2b91h88888888 1 3 1

  # 生成第一位是 T, 后四位是 6666 的靓号, 生成 5 个匹配的地址后退出
  %s build TTTCqtavqZiKEMVYgEQSN2b91h66666666 1 4 5

更多信息请查看 README.md
`, programName, programName, programName, programName)
	fmt.Print(helpText)
}

func main() {
	if len(os.Args) < 2 {
		showHelp()
		return
	}

	switch os.Args[1] {
	case "server":
		server()
	case "build":
		if len(os.Args) != 6 {
			log.Printf("Usage: %s build <fromAddress> <prefixCount> <suffixCount> <quitCount>\n", os.Args[0])
			log.Printf("使用 '%s help' 查看详细帮助\n", os.Args[0])
			return
		} else {
			fromAddress, prefixCount, suffixCount, quitCount := os.Args[2], os.Args[3], os.Args[4], os.Args[5]
			// 将 quictCount 转换为数字
			intQuitCount, err := strconv.Atoi(quitCount)
			if err != nil {
				log.Printf("Error converting quictCount to integer: %v\n", err)
				return
			}

			// 从配置文件加载 POST_URL
			config := loadEnvConfig()
			postURL := config["POST_URL"]

			for i := 0; i < intQuitCount; i++ {
				privateKey, address, totalGenerated, err := generateAddressByGPU(context.Background(), nil, fromAddress, prefixCount, suffixCount, "1")
				if err != nil {
					log.Printf("Error generating address: %v\n", err)
					return
				}
				log.Printf("%s %s (生成数量: %d)\n", privateKey, address, totalGenerated)

				// 如果配置了 POST_URL，则上传结果
				if postURL != "" {
					data := map[string]string{"address": address, "private_key": privateKey}
					jsonData, err := json.Marshal(data)
					if err != nil {
						log.Printf("序列化数据失败: %v\n", err)
						continue
					}

					resp, err := http.Post(postURL, "application/json", bytes.NewBuffer(jsonData))
					if err != nil {
						log.Printf("上传数据失败: %v\n", err)
						continue
					}
					defer resp.Body.Close()

					if resp.StatusCode == http.StatusOK {
						log.Printf("数据已成功上传到: %s\n", postURL)
					} else {
						log.Printf("上传数据返回状态码: %d\n", resp.StatusCode)
					}
				}
			}
		}
	case "help", "-h", "--help":
		showHelp()
	default:
		log.Printf("Unknown command: %s\n", os.Args[1])
		log.Printf("使用 '%s help' 查看可用命令\n", os.Args[0])
	}
}
