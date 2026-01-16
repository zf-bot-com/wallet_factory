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
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

//go:embed config.env
var envContent string

type Item struct {
	TaskID       string `json:"taskId"`
	TaskType     string `json:"taskType"`     // "5a", "6a", "7a", "8a", "custom_address"
	CustomFormat string `json:"customFormat"` // 例如 "TTTT-TTTT"
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

// parseDuration 解析时间字符串，支持 "5s", "3s" 等格式
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("解析时间失败 %s，使用默认值: %v\n", s, err)
		return 5 * time.Second
	}
	return d
}

func server() {
	// 从嵌入的 config.env 加载配置
	config := loadEnvConfig()

	// 获取配置值，如果不存在则使用默认值
	redisAddr := config["REDIS_ADDR"]
	if redisAddr == "" {
		log.Fatal("REDIS_ADDR 未配置")
	}

	// Redis 密码可选，如果为空则不使用密码
	redisPassword := config["REDIS_PASSWORD"]

	redisDB := 0
	if dbStr := config["REDIS_DB"]; dbStr != "" {
		if db, err := strconv.Atoi(dbStr); err == nil {
			redisDB = db
		}
	}

	poolSize := 10
	if poolSizeStr := config["REDIS_POOL_SIZE"]; poolSizeStr != "" {
		if ps, err := strconv.Atoi(poolSizeStr); err == nil {
			poolSize = ps
		}
	}

	minIdleConns := 5
	if minIdleStr := config["REDIS_MIN_IDLE_CONNS"]; minIdleStr != "" {
		if mic, err := strconv.Atoi(minIdleStr); err == nil {
			minIdleConns = mic
		}
	}

	dialTimeout := parseDuration(config["REDIS_DIAL_TIMEOUT"])
	readTimeout := parseDuration(config["REDIS_READ_TIMEOUT"])
	writeTimeout := parseDuration(config["REDIS_WRITE_TIMEOUT"])

	// 创建Redis客户端连接，添加连接池配置
	client := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     redisPassword,
		DB:           redisDB,
		PoolSize:     poolSize,
		MinIdleConns: minIdleConns,
		DialTimeout:  dialTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	})

	// 测试 Redis 连接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("无法连接到 Redis, 请确认配置是否正确, 或是否加白名单, 错误信息: %v\n", err)
		return
	}
	log.Printf("Redis 连接成功\n")

	// 定义要监听的队列名称
	queueInName := "address_producer"
	queueOutName := "address_consumer"
	// 定义一个键，用于检查工作程序是否仍在运行
	isWorkerAlive := "is_worker_alive"

	// 任务处理超时时间（GPU 计算可能需要较长时间）
	taskTimeout := 30 * time.Minute

	log.Printf("开始监听队列: %s\n", queueInName)

	// 定期更新 worker 存活状态
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := client.SetEX(ctx, isWorkerAlive, "1", 60*time.Second).Err(); err != nil {
				log.Printf("更新 worker 存活状态失败: %v\n", err)
			}
		}
	}()

	for {
		// 使用 BRPop 阻塞式获取任务，超时时间 5 秒
		// 这样比轮询 RPop + Sleep 更高效
		result, err := client.BRPop(ctx, 5*time.Second, queueInName).Result()
		if err == redis.Nil {
			// 超时，继续循环
			continue
		} else if err != nil {
			log.Printf("从队列获取任务失败: %v，5秒后重试\n", err)
			time.Sleep(5 * time.Second)
			// 尝试重连 Redis
			if err := client.Ping(ctx).Err(); err != nil {
				log.Printf("Redis 连接失败: %v，继续重试\n", err)
			}
			continue
		}

		// result[0] 是队列名，result[1] 是任务数据
		if len(result) < 2 {
			log.Printf("从队列获取的数据格式错误: %v\n", result)
			continue
		}
		jsonStr := result[1]

		// 解析任务
		var item Item
		if err := json.Unmarshal([]byte(jsonStr), &item); err != nil {
			log.Printf("解析任务 JSON 失败: %v，任务数据: %s\n", err, jsonStr)
			// 继续处理下一个任务，不退出
			continue
		}

		log.Printf("收到任务: TaskID=%s, TaskType=%s, CustomFormat=%s\n", item.TaskID, item.TaskType, item.CustomFormat)

		// 创建带超时的上下文处理任务
		taskCtx, cancel := context.WithTimeout(ctx, taskTimeout)

		// 使用 channel 来等待任务完成
		done := make(chan bool, 1)

		go func(item Item) {
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("任务处理发生 panic: %v\n", r)
					// 发送失败结果
					sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("panic: %v", r))
				}
				done <- true
			}()

			var prefixCount, suffixCount string
			var matchingAddress string

			// 根据 taskType 决定生成策略
			switch item.TaskType {
			case "5a", "6a", "7a", "8a":
				// 生成后 N 位相同的地址，例如 5a 表示后5位相同
				suffixLen, err := strconv.Atoi(string(item.TaskType[0]))
				if err != nil {
					log.Printf("解析 taskType 失败: %v\n", err)
					sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("解析 taskType 失败: %v", err))
					return
				}
				suffixCount = strconv.Itoa(suffixLen)
				prefixCount = "0"
				// 直接使用 profanity.txt 文件作为匹配模式
				matchingAddress = "./profanity.txt"
				// 检查文件是否存在
				if _, err := os.Stat(matchingAddress); os.IsNotExist(err) {
					log.Printf("靓号模板文件不存在\n")
					sendFailureResult(ctx, client, queueOutName, item.TaskID, "靓号模板文件不存在")
					return
				}

			case "custom_address":
				// ./profanity --matching <address> --prefix-count 4 --suffix-count 4 --quit-count 1
				// 根据 customFormat 解析
				prefixStr, suffixStr, err := parseCustomFormat(item.CustomFormat)
				if err != nil {
					log.Printf("解析 customFormat 失败: %v\n", err)
					sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("解析格式失败: %v", err))
					return
				}
				prefixCount = strconv.Itoa(len(prefixStr))
				suffixCount = strconv.Itoa(len(suffixStr))
				// 构造匹配地址：Tron 地址是 34 位
				// 格式：前缀 + 中间占位符 + 后缀
				matchingAddress = prefixStr
				// 中间部分用 X 占位
				for i := 0; i < 34-len(prefixStr)-len(suffixStr); i++ {
					matchingAddress += "X"
				}
				matchingAddress += suffixStr

			default:
				log.Printf("未知的 taskType: %s\n", item.TaskType)
				sendFailureResult(ctx, client, queueOutName, item.TaskID, fmt.Sprintf("未知的 taskType: %s", item.TaskType))
				return
			}

			log.Printf("生成地址参数: matching=%s, prefixCount=%s, suffixCount=%s\n", matchingAddress, prefixCount, suffixCount)

			// 处理任务
			privateKey, addr, totalGenerated, err := generateAddressByGPU(matchingAddress, prefixCount, suffixCount, "1")
			if err != nil {
				log.Printf("生成地址失败: TaskID=%s, 错误: %v\n", item.TaskID, err)
				sendFailureResult(ctx, client, queueOutName, item.TaskID, err.Error())
				return
			}

			log.Printf("地址生成成功: TaskID=%s, %s --> %s, 生成数量: %d\n", item.TaskID, matchingAddress, addr, totalGenerated)

			// 构建返回结果
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

			// 将结果推送到输出队列，带重试机制
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
		}(item)

		// 等待任务完成或超时
		taskID := item.TaskID // 保存 taskID 用于超时日志
		select {
		case <-done:
			// 任务完成
		case <-taskCtx.Done():
			if taskCtx.Err() == context.DeadlineExceeded {
				log.Printf("任务处理超时: TaskID=%s\n", taskID)
				// 发送超时失败结果
				sendFailureResult(ctx, client, queueOutName, taskID, "任务处理超时")
			}
		}
	}
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

func generateAddressByGPU(address string, prefixCount string, suffixCount string, quictCount string) (string, string, int64, error) {
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
	cmd := exec.Command(exec_file, "--matching", address, "--prefix-count", prefixCount, "--suffix-count", suffixCount, "--quit-count", quictCount)
	output, err := cmd.CombinedOutput()
	if err != nil {
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
				privateKey, address, totalGenerated, err := generateAddressByGPU(fromAddress, prefixCount, suffixCount, "1")
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
