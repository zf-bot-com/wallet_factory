package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// extractValues 从 profanity 输出中提取私钥、地址和生成数量
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

// generateAddressByGPU 使用 GPU 生成靓号地址
func generateAddressByGPU(ctx context.Context, workerState *WorkerState, address string, prefixCount string, suffixCount string, quictCount string) (string, string, int64, error) {
	log.Printf("🎮 GPU 开始生成地址: matching=%s, prefix=%s, suffix=%s\n", address, prefixCount, suffixCount)

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

	// 构建命令参数
	args := []string{"--matching", address, "--prefix-count", prefixCount, "--suffix-count", suffixCount, "--quit-count", quictCount}

	// macOS 环境需要添加 -w 1 参数以生成正确的私钥
	if runtime.GOOS == "darwin" {
		args = append(args, "-w", "1")
	}

	// 打印完整的执行命令（包含所有参数）
	log.Printf("执行命令: %s %s\n", exec_file, strings.Join(args, " "))

	// 使用 CommandContext 创建可取消的命令
	cmd := exec.CommandContext(ctx, exec_file, args...)

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

	log.Printf("✅ GPU 生成完成: address=%s, totalGenerated=%d\n", addr, totalGenerated)
	return privateKey, addr, totalGenerated, nil
}

// checkGPUEnvironment 检查 GPU 环境是否正确配置
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

// GPUManager GPU 独占管理器
type GPUManager struct {
	mu            sync.Mutex
	bgCmd         *exec.Cmd
	bgCancel      context.CancelFunc
	bgRunning     bool
	bgCtx         context.Context // 保存 context 用于自动重启
	onResult      func(AddressResult) // 后台产出回调
	cache         *AddressCache
	waiterMgr     *WaiterManager
	cacheFullWarn map[string]bool // 记录每个 taskType 是否已警告过缓存已满
}

// NewGPUManager 创建 GPU 管理器
func NewGPUManager(cache *AddressCache, waiterMgr *WaiterManager) *GPUManager {
	return &GPUManager{
		cache:         cache,
		waiterMgr:     waiterMgr,
		cacheFullWarn: make(map[string]bool),
	}
}

// StartBackground 启动后台 profanity 进程
func (gm *GPUManager) StartBackground(ctx context.Context) error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if gm.bgRunning {
		return nil // 已在运行
	}

	// 保存 context 用于自动重启
	gm.bgCtx = ctx

	return gm.startBackgroundLocked()
}

// startBackgroundLocked 内部启动方法（需要持有锁）
func (gm *GPUManager) startBackgroundLocked() error {
	var exec_file string
	switch runtime.GOOS {
	case "darwin":
		exec_file = "./profanity.arm64"
	case "windows":
		exec_file = "./profanity.exe"
	default:
		exec_file = "./profanity.x64"
	}

	if _, err := os.Stat(exec_file); os.IsNotExist(err) {
		return fmt.Errorf("可执行文件不存在: %s", exec_file)
	}

	bgCtx, bgCancel := context.WithCancel(gm.bgCtx)
	gm.bgCancel = bgCancel

	// 构建后台 profanity 参数
	args := []string{
		"--matching", "./profanity.txt",
		"--prefix-count", "0",
		"--suffix-count", "6",
		"--quit-count", "999999999", // 设置一个极大的值，实现持续运行
	}

	// macOS 环境需要添加 -w 1 参数
	if runtime.GOOS == "darwin" {
		args = append(args, "-w", "1")
	}

	cmd := exec.CommandContext(bgCtx, exec_file, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		bgCancel()
		return fmt.Errorf("创建 stdout pipe 失败: %v", err)
	}

	if err := cmd.Start(); err != nil {
		bgCancel()
		return fmt.Errorf("启动后台 profanity 失败: %v", err)
	}

	gm.bgCmd = cmd
	gm.bgRunning = true

	log.Printf("✅ 后台 profanity 已启动 (PID: %d, 参数: suffix-count=6, quit-count=999999999)\n", cmd.Process.Pid)

	// 启动 goroutine 读取输出
	go gm.readBackgroundOutput(stdout)

	// 启动 goroutine 监控进程退出并自动重启
	go func() {
		cmd.Wait()
		gm.mu.Lock()
		gm.bgRunning = false
		gm.mu.Unlock()

		log.Printf("⚠️  后台 profanity 已退出\n")

		// 等待 3 秒后自动重启
		time.Sleep(3 * time.Second)

		// 检查 context 是否已取消（如果是主动停止则不重启）
		select {
		case <-gm.bgCtx.Done():
			log.Printf("🛑 后台 profanity 不再重启（context 已取消）\n")
			return
		default:
			log.Printf("🔄 正在自动重启后台 profanity...\n")
			gm.mu.Lock()
			if err := gm.startBackgroundLocked(); err != nil {
				log.Printf("❌ 自动重启失败: %v\n", err)
			}
			gm.mu.Unlock()
		}
	}()

	return nil
}

// StopBackground 停止后台 profanity 进程
func (gm *GPUManager) StopBackground() {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	if !gm.bgRunning {
		return
	}

	if gm.bgCancel != nil {
		gm.bgCancel()
	}

	if gm.bgCmd != nil && gm.bgCmd.Process != nil {
		log.Printf("停止后台 profanity (PID: %d)\n", gm.bgCmd.Process.Pid)
		gm.bgCmd.Process.Kill()
	}

	gm.bgRunning = false
}

// readBackgroundOutput 读取后台 profanity 的输出
func (gm *GPUManager) readBackgroundOutput(stdout io.ReadCloser) {
	defer stdout.Close()

	scanner := bufio.NewScanner(stdout)
	re := regexp.MustCompile(`Private: ([a-fA-F0-9]+) Address:([a-zA-Z0-9]+)`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)

		if len(matches) == 3 {
			result := AddressResult{
				PrivateKey: matches[1],
				Address:    matches[2],
			}

			// 分类地址
			taskType := classifyAddress(result.Address)
			if taskType == "" {
				continue // 不符合缓存条件
			}

			// 优先检查是否有等待者
			hasWaiter := gm.waiterMgr.HasWaiter(taskType)

			// 检查缓存是否已满
			cacheFull := !gm.cache.ShouldCache(taskType)

			// 如果缓存已满且没有等待者，直接跳过（不输出任何日志）
			if cacheFull && !hasWaiter {
				// 只在第一次检测到缓存已满时打印警告
				if !gm.cacheFullWarn[taskType] {
					log.Printf("⚠️  缓存已满，跳过: taskType=%s\n", taskType)
					gm.cacheFullWarn[taskType] = true
				}
				continue
			}

			// 如果缓存有空间了，重置警告标志
			if !cacheFull && gm.cacheFullWarn[taskType] {
				gm.cacheFullWarn[taskType] = false
			}

			// 输出产出日志（只有在有等待者或缓存未满时才输出）
			totalCount := gm.cache.GetTotalCount()
			log.Printf("🎯 后台产出地址 #%d: taskType=%s, address=%s\n", totalCount+1, taskType, result.Address)

			// 优先交付给等待者
			if gm.waiterMgr.TryDeliver(taskType, &result) {
				log.Printf("✅ 地址已交付给等待任务: taskType=%s\n", taskType)
				continue
			}

			// 如果缓存已满，这里不应该到达（因为前面已经检查过了）
			if cacheFull {
				continue
			}

			// 提取前缀和后缀用于缓存
			prefix, suffix := extractPrefixSuffix(result.Address)

			// 写入缓存
			if err := gm.cache.Insert(taskType, result.Address, result.PrivateKey, prefix, suffix); err != nil {
				log.Printf("❌ 缓存写入失败: %v\n", err)
			} else {
				log.Printf("💾 缓存写入成功: taskType=%s, address=%s, prefix=%s, suffix=%s\n", taskType, result.Address, prefix, suffix)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("读取后台输出失败: %v\n", err)
	}
}

// RunDedicatedTask 运行专用任务（暂停后台 → 执行 → 恢复后台）
func (gm *GPUManager) RunDedicatedTask(ctx context.Context, workerState *WorkerState, matchingAddress, prefixCount, suffixCount, quitCount string) (string, string, int64, error) {
	// 暂停后台
	log.Printf("⏸️  暂停后台 profanity，准备执行专用任务\n")
	gm.StopBackground()
	defer func() {
		// 恢复后台（异步，避免阻塞）
		go func() {
			time.Sleep(1 * time.Second)
			log.Printf("▶️  恢复后台 profanity\n")
			if err := gm.StartBackground(context.Background()); err != nil {
				log.Printf("恢复后台 profanity 失败: %v\n", err)
			}
		}()
	}()

	// 执行专用任务
	log.Printf("🔧 开始执行专用 GPU 任务: matching=%s, prefix=%s, suffix=%s\n", matchingAddress, prefixCount, suffixCount)
	return generateAddressByGPU(ctx, workerState, matchingAddress, prefixCount, suffixCount, quitCount)
}
