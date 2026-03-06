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
	"strconv"
)

//go:embed config.env
var envContent string

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
