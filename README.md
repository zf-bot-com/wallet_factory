# wallet_factory

用 golang 写的一个 Tron 靓号生成工具，支持多种条件配置，生成靓号，保存到 txt 文档中.

支持多协程，生成速度快，欢迎试用。

## 安装步骤

如果不想自己搭建 golang 环境，直接用编译好的二进制文件即可。

* windows 下载地址：[wallet.exe](https://github.com/zf-bot-com/wallet_factory/releases/download/v1.0/wallet.exe)
* mac 下载地址：[wallet-mac](https://github.com/zf-bot-com/wallet_factory/releases/download/v1.0/wallet-mac)

## 使用方法

以下为 windows 版本的使用方法，mac 版本类似。

```
# 生成后缀为 7777 的靓号
.\wallet.exe -endStr=7777 

# 生成前缀是 Tttt 后缀是 7777 的靓号
.\wallet.exe -beginStr=Tttt -endStr=7777

# 生成前4位重复，后4位重复的靓号
.\wallet.exe -beginTimes=4 -endTimes=4

# 生成10个后缀位 7777 的靓号
.\wallet.exe -endStr=7777 -numAddr=10
```