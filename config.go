package main

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

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
