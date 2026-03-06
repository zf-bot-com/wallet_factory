package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
)

// handleRedisWorker 处理单个 Redis 连接的任务
func handleRedisWorker(ctx context.Context, wrapper *RedisClientWrapper, workerState *WorkerState, wg *sync.WaitGroup, cache *AddressCache, waiterMgr *WaiterManager, gpuMgr *GPUManager) {
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

		processTask(ctx, client, workerState, item, queueOutName, cache, waiterMgr, gpuMgr)
	}

	log.Printf("[Redis-%d] Worker 已停止\n", cfg.ID)
}

// server 启动服务器模式
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

	// 初始化缓存系统
	var cache *AddressCache
	var waiterMgr *WaiterManager
	var gpuMgr *GPUManager

	cacheEnabled := config["CACHE_ENABLED"] == "true"
	log.Printf("📦 缓存系统状态: %v\n", cacheEnabled)
	if cacheEnabled {
		aesKey := config["CACHE_AES_KEY"]
		if aesKey == "" {
			// 硬编码密钥（编译时会被 garble 混淆）
			aesKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			log.Printf("⚠️  使用默认 AES 密钥（生产环境请配置 CACHE_AES_KEY）\n")
		}

		dbPath := config["CACHE_DB_PATH"]
		if dbPath == "" {
			dbPath = "./address_cache.db"
		}

		// 解析缓存上限
		maxCounts := make(map[string]int64)
		if max6a := config["CACHE_MAX_6A"]; max6a != "" {
			if val, err := strconv.ParseInt(max6a, 10, 64); err == nil {
				maxCounts["6a"] = val
			}
		} else {
			maxCounts["6a"] = 1000
		}
		if max7a := config["CACHE_MAX_7A"]; max7a != "" {
			if val, err := strconv.ParseInt(max7a, 10, 64); err == nil {
				maxCounts["7a"] = val
			}
		} else {
			maxCounts["7a"] = 1000
		}

		var err error
		cache, err = NewAddressCache(dbPath, aesKey, maxCounts)
		if err != nil {
			log.Fatalf("初始化缓存失败: %v\n", err)
		}
		defer cache.Close()

		waiterMgr = NewWaiterManager()
		gpuMgr = NewGPUManager(cache, waiterMgr)

		// 启动后台 profanity
		log.Printf("🚀 正在启动后台 profanity 进程...\n")
		if err := gpuMgr.StartBackground(ctx); err != nil {
			log.Printf("⚠️  启动后台 profanity 失败: %v\n", err)
		}
	} else {
		log.Printf("⚠️  缓存系统未启用，所有任务将直接使用 GPU 生成\n")
	}

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
		go handleRedisWorker(ctx, wrapper, workerState, &wg, cache, waiterMgr, gpuMgr)
	}

	wg.Wait()
	log.Printf("所有 Redis Worker 已停止\n")
}
