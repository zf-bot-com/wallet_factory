package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

// 任务处理超时时间（GPU 计算可能需要较长时间）
var taskTimeout = 168 * time.Hour

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

// processTask 处理单个任务
func processTask(ctx context.Context, client *redis.Client, workerState *WorkerState, item Item, queueOutName string, cache *AddressCache, waiterMgr *WaiterManager, gpuMgr *GPUManager) {
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

	// 第一步：尝试从缓存获取
	if cache != nil {
		var cached *CachedAddress
		var err error

		if item.CustomFormat != "" {
			// custom_address：按前缀后缀精确匹配
			prefixStr, suffixStr, parseErr := parseCustomFormat(item.CustomFormat)
			if parseErr == nil {
				cached, err = cache.FetchByPattern(prefixStr, suffixStr)
			}
		} else if item.TaskType == "6a" || item.TaskType == "7a" || item.TaskType == "8a" || item.TaskType == "9a" {
			// 标准任务：按 task_type 匹配
			cached, err = cache.FetchAndDelete(item.TaskType)
		}

		if err != nil {
			log.Printf("查询缓存失败: %v，继续 GPU 生成\n", err)
		} else if cached != nil {
			// 缓存命中！解密并返回
			privateKey, err := cache.decryptPrivateKey(cached.EncryptedKey, cached.Nonce)
			if err != nil {
				log.Printf("解密缓存私钥失败: %v，继续 GPU 生成\n", err)
			} else {
				log.Printf("✅ 缓存命中: TaskID=%s, TaskType=%s, Address=%s\n", item.TaskID, item.TaskType, cached.Address)
				sendSuccessResult(ctx, client, queueOutName, item.TaskID, privateKey, cached.Address, 0)
				return
			}
		}
	}

	// 第二步：标准任务（6a-9a）缓存未命中 → 等待后台产出
	if waiterMgr != nil && (item.TaskType == "6a" || item.TaskType == "7a" || item.TaskType == "8a" || item.TaskType == "9a") && item.CustomFormat == "" {
		log.Printf("⏳ 缓存未命中，等待后台产出: TaskID=%s, TaskType=%s\n", item.TaskID, item.TaskType)

		taskCtx, taskCancel := context.WithTimeout(ctx, taskTimeout)
		defer taskCancel()
		workerState.SetTaskCancelFunc(taskCancel)

		result, err := waiterMgr.Wait(item.TaskType, taskCtx)
		if err != nil {
			if workerState.IsCancelRequested() {
				log.Printf("任务已被取消: TaskID=%s\n", item.TaskID)
				sendFailureResult(ctx, client, queueOutName, item.TaskID, "任务已被用户取消")
				return
			}
			log.Printf("等待后台产出超时: TaskID=%s, 错误: %v\n", item.TaskID, err)
			sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("等待超时: %v", err))
			return
		}

		log.Printf("✅ 后台产出交付: TaskID=%s, Address=%s\n", item.TaskID, result.Address)
		sendSuccessResult(ctx, client, queueOutName, item.TaskID, result.PrivateKey, result.Address, 0)
		return
	}

	// 第三步：5a 或 custom_address → 暂停后台，跑专用进程
	log.Printf("🔧 准备执行专用 GPU 任务: TaskID=%s, TaskType=%s, CustomFormat=%s\n", item.TaskID, item.TaskType, item.CustomFormat)

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

	var privateKey, addr string
	var totalGenerated int64
	var err error

	if gpuMgr != nil && (item.TaskType == "5a" || item.CustomFormat != "") {
		// 使用 GPUManager 运行专用任务（会暂停后台）
		privateKey, addr, totalGenerated, err = gpuMgr.RunDedicatedTask(taskCtx, workerState, matchingAddress, prefixCount, suffixCount, "1")
	} else {
		// 直接调用 GPU 生成
		privateKey, addr, totalGenerated, err = generateAddressByGPU(taskCtx, workerState, matchingAddress, prefixCount, suffixCount, "1")
	}

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
	sendSuccessResult(ctx, client, queueOutName, item.TaskID, privateKey, addr, totalGenerated)
}

// sendSuccessResult 发送成功结果到队列
func sendSuccessResult(ctx context.Context, client *redis.Client, queueOutName string, taskID string, privateKey string, address string, totalGenerated int64) {
	itemReply := ItemReply{
		TaskID: taskID,
		Status: "completed",
		Result: TaskResult{
			PrivateKey:     privateKey,
			Address:        address,
			TotalGenerated: totalGenerated,
		},
	}

	jsonBytes, err := json.Marshal(itemReply)
	if err != nil {
		log.Printf("序列化结果失败: %v\n", err)
		sendFailureResult(ctx, client, queueOutName, taskID, fmt.Sprintf("序列化失败: %v", err))
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
			log.Printf("结果已推送到队列: %s, TaskID=%s\n", queueOutName, taskID)
			break
		}
	}
}
