package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"sync"

	_ "modernc.org/sqlite"
)

// AddressResult 地址生成结果
type AddressResult struct {
	PrivateKey string
	Address    string
}

// CachedAddress 缓存的地址记录
type CachedAddress struct {
	ID           int64
	TaskType     string
	Address      string
	EncryptedKey string
	Nonce        string
	Prefix       string
	Suffix       string
}

// AddressCache 地址缓存管理器
type AddressCache struct {
	db       *sql.DB
	aesKey   []byte
	counters map[string]int64 // 内存计数器: {"6a": 523, "7a": 89}
	mu       sync.RWMutex
	maxCounts map[string]int64 // 缓存上限配置
}

// NewAddressCache 创建缓存管理器
func NewAddressCache(dbPath, aesKeyHex string, maxCounts map[string]int64) (*AddressCache, error) {
	// 解析 AES 密钥
	aesKey, err := hex.DecodeString(aesKeyHex)
	if err != nil {
		return nil, fmt.Errorf("解析 AES 密钥失败: %v", err)
	}
	if len(aesKey) != 32 {
		return nil, fmt.Errorf("AES 密钥必须是 32 字节（64 位十六进制）")
	}

	// 打开 SQLite 数据库
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %v", err)
	}

	// 创建表
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS address_cache (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		task_type   TEXT NOT NULL,
		address     TEXT NOT NULL UNIQUE,
		private_key TEXT NOT NULL,
		nonce       TEXT NOT NULL,
		prefix      TEXT DEFAULT '',
		suffix      TEXT DEFAULT '',
		created_at  DATETIME DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_task_type ON address_cache(task_type);
	CREATE INDEX IF NOT EXISTS idx_prefix_suffix ON address_cache(prefix, suffix);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建表失败: %v", err)
	}

	cache := &AddressCache{
		db:        db,
		aesKey:    aesKey,
		counters:  make(map[string]int64),
		maxCounts: maxCounts,
	}

	// 初始化计数器
	if err := cache.InitCounters(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化计数器失败: %v", err)
	}

	log.Printf("✅ 缓存系统初始化成功: %s\n", dbPath)
	return cache, nil
}

// InitCounters 从数据库加载计数到内存
func (c *AddressCache) InitCounters() error {
	rows, err := c.db.Query("SELECT task_type, COUNT(*) FROM address_cache WHERE task_type IN ('6a', '7a') GROUP BY task_type")
	if err != nil {
		return err
	}
	defer rows.Close()

	c.mu.Lock()
	defer c.mu.Unlock()

	for rows.Next() {
		var taskType string
		var count int64
		if err := rows.Scan(&taskType, &count); err != nil {
			return err
		}
		c.counters[taskType] = count
		log.Printf("缓存计数器: %s = %d\n", taskType, count)
	}

	return rows.Err()
}

// Close 关闭数据库连接
func (c *AddressCache) Close() error {
	return c.db.Close()
}

// encryptPrivateKey 使用 AES-256-GCM 加密私钥
func (c *AddressCache) encryptPrivateKey(plaintext string) (ciphertext string, nonceStr string, err error) {
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return "", "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}

	ciphertextBytes := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertextBytes),
		base64.StdEncoding.EncodeToString(nonce),
		nil
}

// decryptPrivateKey 使用 AES-256-GCM 解密私钥
func (c *AddressCache) decryptPrivateKey(ciphertext string, nonceStr string) (string, error) {
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	nonce, err := base64.StdEncoding.DecodeString(nonceStr)
	if err != nil {
		return "", err
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// FetchAndDelete 原子查询并删除缓存（标准任务）
func (c *AddressCache) FetchAndDelete(taskType string) (*CachedAddress, error) {
	tx, err := c.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRow(
		"SELECT id, address, private_key, nonce FROM address_cache WHERE task_type = ? LIMIT 1",
		taskType,
	)

	var cached CachedAddress
	cached.TaskType = taskType
	if err := row.Scan(&cached.ID, &cached.Address, &cached.EncryptedKey, &cached.Nonce); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 未命中
		}
		return nil, err
	}

	_, err = tx.Exec("DELETE FROM address_cache WHERE id = ?", cached.ID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 更新内存计数器
	c.mu.Lock()
	if c.counters[taskType] > 0 {
		c.counters[taskType]--
	}
	c.mu.Unlock()

	return &cached, nil
}

// FetchByPattern 按前缀后缀模糊匹配（custom_address）
// 支持后缀前缀匹配：例如查询 suffix="66666" 可以匹配到 suffix="666666" 或 "6666666"
func (c *AddressCache) FetchByPattern(prefix, suffix string) (*CachedAddress, error) {
	tx, err := c.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 先尝试精确匹配
	row := tx.QueryRow(
		"SELECT id, task_type, address, private_key, nonce FROM address_cache WHERE prefix = ? AND suffix = ? LIMIT 1",
		prefix, suffix,
	)

	var cached CachedAddress
	cached.Prefix = prefix
	cached.Suffix = suffix
	err = row.Scan(&cached.ID, &cached.TaskType, &cached.Address, &cached.EncryptedKey, &cached.Nonce)

	// 如果精确匹配未找到，尝试后缀前缀匹配（suffix LIKE 'xxx%'）
	if err == sql.ErrNoRows {
		row = tx.QueryRow(
			"SELECT id, task_type, address, private_key, nonce FROM address_cache WHERE prefix = ? AND suffix LIKE ? LIMIT 1",
			prefix, suffix+"%",
		)
		err = row.Scan(&cached.ID, &cached.TaskType, &cached.Address, &cached.EncryptedKey, &cached.Nonce)
	}

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	_, err = tx.Exec("DELETE FROM address_cache WHERE id = ?", cached.ID)
	if err != nil {
		return nil, err
	}

	// 更新内存计数器
	c.mu.Lock()
	if c.counters[cached.TaskType] > 0 {
		c.counters[cached.TaskType]--
	}
	c.mu.Unlock()

	return &cached, tx.Commit()
}

// Insert 插入缓存记录
func (c *AddressCache) Insert(taskType, address, privateKey, prefix, suffix string) error {
	encrypted, nonce, err := c.encryptPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("加密私钥失败: %v", err)
	}

	_, err = c.db.Exec(
		"INSERT OR IGNORE INTO address_cache (task_type, address, private_key, nonce, prefix, suffix) VALUES (?, ?, ?, ?, ?, ?)",
		taskType, address, encrypted, nonce, prefix, suffix,
	)
	if err != nil {
		return err
	}

	// 更新内存计数器
	if taskType == "6a" || taskType == "7a" {
		c.mu.Lock()
		c.counters[taskType]++
		c.mu.Unlock()
	}

	return nil
}

// ShouldCache 检查是否应该缓存（基于内存计数器）
func (c *AddressCache) ShouldCache(taskType string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	maxCount, exists := c.maxCounts[taskType]
	if !exists {
		return true // 无上限
	}

	currentCount := c.counters[taskType]
	return currentCount < maxCount
}

// GetTotalCount 获取数据库中所有地址的总数
func (c *AddressCache) GetTotalCount() int64 {
	var total int64
	err := c.db.QueryRow("SELECT COUNT(*) FROM address_cache").Scan(&total)
	if err != nil {
		log.Printf("⚠️  查询总数失败: %v\n", err)
		return 0
	}
	return total
}

// WaiterManager 等待者管理器
type WaiterManager struct {
	mu      sync.Mutex
	waiters map[string][]chan *AddressResult // key=taskType
}

// NewWaiterManager 创建等待者管理器
func NewWaiterManager() *WaiterManager {
	return &WaiterManager{
		waiters: make(map[string][]chan *AddressResult),
	}
}

// Wait 注册等待并阻塞直到收到结果或超时
func (wm *WaiterManager) Wait(taskType string, ctx context.Context) (*AddressResult, error) {
	resultChan := make(chan *AddressResult, 1)

	wm.mu.Lock()
	wm.waiters[taskType] = append(wm.waiters[taskType], resultChan)
	wm.mu.Unlock()

	// 清理函数
	defer func() {
		wm.mu.Lock()
		// 从等待列表中移除
		waiters := wm.waiters[taskType]
		for i, ch := range waiters {
			if ch == resultChan {
				wm.waiters[taskType] = append(waiters[:i], waiters[i+1:]...)
				break
			}
		}
		wm.mu.Unlock()
		close(resultChan)
	}()

	select {
	case result := <-resultChan:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TryDeliver 尝试交付结果给等待者
func (wm *WaiterManager) TryDeliver(taskType string, result *AddressResult) bool {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	waiters := wm.waiters[taskType]
	if len(waiters) == 0 {
		return false // 无等待者
	}

	// 交付给第一个等待者
	waiter := waiters[0]
	wm.waiters[taskType] = waiters[1:]

	select {
	case waiter <- result:
		log.Printf("✅ 结果已交付给等待者: taskType=%s, address=%s\n", taskType, result.Address)
		return true
	default:
		return false
	}
}

// classifyAddress 分析地址后缀，返回任务类型（如 "6a", "8a"）
func classifyAddress(address string) string {
	if len(address) == 0 {
		return ""
	}

	n := len(address)
	lastChar := address[n-1]
	count := 1

	// 从后往前统计连续相同字符
	for i := n - 2; i >= 0; i-- {
		if address[i] == lastChar {
			count++
		} else {
			break
		}
	}

	// 只缓存 6-10 位
	if count >= 6 && count <= 10 {
		return fmt.Sprintf("%da", count)
	}

	return ""
}

// extractPrefixSuffix 从地址中提取前缀和后缀
// 例如: "TGSSTmkHawoAPjbYK55LF5NLEYYE888888" -> prefix="T", suffix="888888"
// 注意：后缀是地址中最长的连续相同字符序列（至少 5 个）
func extractPrefixSuffix(address string) (prefix string, suffix string) {
	if len(address) == 0 {
		return "", ""
	}

	n := len(address)

	// 前缀：第一个字符（Tron 地址必须以 T 开头）
	prefix = string(address[0])

	// 后缀：找到地址中最长的连续相同字符序列
	maxCount := 0
	maxStart := 0

	i := 0
	for i < n {
		char := address[i]
		count := 1

		// 统计从当前位置开始的连续相同字符数量
		for i+count < n && address[i+count] == char {
			count++
		}

		// 如果找到更长的序列，更新
		if count > maxCount {
			maxCount = count
			maxStart = i
		}

		i += count
	}

	// 只有当连续字符数 >= 5 时才作为后缀
	if maxCount >= 5 {
		suffix = address[maxStart : maxStart+maxCount]
	}

	return prefix, suffix
}
