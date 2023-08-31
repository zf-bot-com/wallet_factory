package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	btcec "github.com/btcsuite/btcd/btcec/v2"
	addr "github.com/fbsobreira/gotron-sdk/pkg/address"
	// cl "github.com/MasterOfBinary/go-opencl"
)

func GenerateKey() (address string, wif string) {
	pri, err := btcec.NewPrivateKey()
	if err != nil {
		return "", ""
	}
	if len(pri.Key.Bytes()) != 32 {
		for {
			pri, err = btcec.NewPrivateKey()
			if err != nil {
				continue
			}
			if len(pri.Key.Bytes()) == 32 {
				break
			}
		}
	}

	address = addr.PubkeyToAddress(pri.ToECDSA().PublicKey).String()
	wif = pri.Key.String()
	return
}

func randomChar() string {
	rand.Seed(time.Now().UnixNano())
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	return string(charset[rand.Intn(len(charset))])
}

func generateBeginAndEndRepeatAccount(beginRepeatTimes int, endRepeatTimes int, numWorker int) (string, string, error) {
	if beginRepeatTimes == 0 && endRepeatTimes == 0 {
		return "", "", fmt.Errorf("开始和结束连号位数不能同时为0")
	}

	beginStr := strings.Repeat("t", beginRepeatTimes) // trc 的地址都是以 t 开头
	rs := randomChar()
	endStr := strings.Repeat(rs, endRepeatTimes)
	return generateSpecialBeginAndEndAccount(beginStr, endStr, numWorker)
}

func generateSpecialBeginAndEndAccount(beginStr string, endStr string, numWorker int) (string, string, error) {
	if beginStr == "" && endStr == "" {
		return "", "", fmt.Errorf("开始和结束位数不能同时为空")
	}

	fmt.Printf("开始生成钱包 --> beginStr: %v, endStr: %v\n", beginStr, endStr)
	// 创建一个无缓冲通道，用于在协程之间传递结果
	resultChan := make(chan []string)
	var lowerAddress string
	if beginStr != "" && endStr != "" {
		for i := 0; i < numWorker; i++ {
			go func() {
				for {
					address, privateKey := GenerateKey()
					lowerAddress = strings.ToLower(address)
					fmt.Println(beginStr, endStr, lowerAddress)
					if strings.HasPrefix(lowerAddress, beginStr) && strings.HasSuffix(lowerAddress, endStr) {
						resultChan <- []string{address, privateKey}
					}
				}
			}()
		}

	} else if beginStr != "" {
		for i := 0; i < numWorker; i++ {
			go func() {
				for {
					address, privateKey := GenerateKey()
					lowerAddress = strings.ToLower(address)
					fmt.Println(beginStr, endStr, lowerAddress)
					if strings.HasPrefix(lowerAddress, beginStr) {
						resultChan <- []string{address, privateKey}
					}
				}
			}()
		}

	} else {
		for i := 0; i < numWorker; i++ {
			go func() {
				for {
					address, privateKey := GenerateKey()
					lowerAddress := strings.ToLower(address)
					fmt.Println(beginStr, endStr, lowerAddress)
					if strings.HasSuffix(lowerAddress, endStr) {
						resultChan <- []string{address, privateKey}
						return
					}
				}
			}()
		}
	}

	// 等待任意一个协程返回结果
	result := <-resultChan
	return result[0], result[1], nil
}

func Product(beginTimes int, endTimes int, beginStr string, endStr string, numAddr int, numWorker int) [][]string {
	// 定义一个列表，列表中的每项都是一个字符串列表
	var addrList [][]string
	if beginStr != "" || endStr != "" {
		for i := 0; i < numAddr; i++ {
			tronAddress, privateKey, err := generateSpecialBeginAndEndAccount(beginStr, endStr, numWorker)
			if err != nil {
				fmt.Printf("生成账号出现异常: %v\n", err)
				os.Exit(1)
			} else {
				// fmt.Printf("\n >>> 钱包地址: %v, >>> 私钥: %v\n", tronAddress, privateKey)
				addrList = append(addrList, []string{tronAddress, privateKey})
			}
		}

	} else if beginTimes != 0 || endTimes != 0 {
		for i := 0; i < numAddr; i++ {
			tronAddress, privateKey, err := generateBeginAndEndRepeatAccount(beginTimes, endTimes, numWorker)
			if err != nil {
				fmt.Printf("生成账号出现异常: %v\n", err)
				os.Exit(1)
			} else {
				// fmt.Printf("钱包地址: %v, 私钥: %v\n\n", tronAddress, privateKey)
				addrList = append(addrList, []string{tronAddress, privateKey})
			}
		}
	} else {
		fmt.Println("参数错误")
		os.Exit(1)
	}
	return addrList
}

func main() {
	beginTimes := flag.Int("beginTimes", 0, "开始重复次数")
	endTimes := flag.Int("endTimes", 0, "结束重复次数")
	beginStr := flag.String("beginStr", "", "开始字符串")
	endStr := flag.String("endStr", "", "结束字符串")
	numAddr := flag.Int("numAddr", 1, "生成账号数量")
	numWorker := flag.Int("numWorker", 8, "并发数")
	flag.Parse()

	fmt.Printf("开始生成靓号 --> beginTimes: %v, endTimes: %v, beginStr: %v, endStr: %v\n", *beginTimes, *endTimes, *beginStr, *endStr)
	addrList := Product(*beginTimes, *endTimes, *beginStr, *endStr, *numAddr, *numWorker)

	fmt.Println("生成的靓号列表: ", addrList)
	file_name := fmt.Sprintf("addr_%v_%v_%v_%v.txt", *beginTimes, *endTimes, *beginStr, *endStr)
	file, err := os.OpenFile(file_name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Printf("打开文件出错: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	for _, addr := range addrList {
		_, err := file.WriteString(fmt.Sprintf("%v, %v\n", addr[0], addr[1]))
		if err != nil {
			fmt.Printf("写入文件出错: %v\n", err)
			os.Exit(1)
		}
	}
}
