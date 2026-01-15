# trap_factory

用 golang 写的一个 Tron 靓号生成工具，支持多种条件配置，支持多协程，生成速度快，欢迎使用。

## 用户文档

打包后的程序包含详细的用户使用指南：

- **[使用说明.md](使用说明.md)** - 中文用户指南（详细的使用说明、示例和故障排查）
- **[USER_GUIDE.md](USER_GUIDE.md)** - English User Guide (Detailed usage instructions, examples and troubleshooting)

这两个文档内容相同，仅语言不同。建议用户根据语言偏好选择阅读。

## 编译

项目提供了 Makefile 来简化编译过程：

### 编译不同平台的可执行文件

```bash
# 编译 Linux 版本
make linux

# 编译 Windows 版本
make win
```

编译后的文件：
- Linux: `factory-linux`
- Windows: `factory-win.exe`

## 部署到服务器

项目提供了自动化部署脚本，可以一键完成编译、打包、上传和 supervisor 配置。

### 快速部署

```bash
# 使用默认配置（服务器: gpu, 路径: /srv）
./deploy.sh

# 或指定服务器和路径
./deploy.sh gpu /srv

# 或使用 Makefile
make deploy SERVER=gpu SERVER_PATH=/srv
```

### 部署流程

脚本会自动完成以下步骤：

1. **编译** - 执行 `make linux` 编译 Linux 版本
2. **打包** - 执行 `make tar-linux` 打包程序
3. **上传** - 使用 `scp` 上传到服务器指定目录
4. **解压** - 在服务器上自动解压并设置权限
5. **配置** - 自动配置 supervisor 并启动服务

### Supervisor 管理

部署完成后，服务由 supervisor 管理，常用命令：

```bash
# 查看服务状态
ssh gpu 'sudo supervisorctl status trap-factory'

# 查看服务日志
ssh gpu 'sudo supervisorctl tail -f trap-factory'

# 停止服务
ssh gpu 'sudo supervisorctl stop trap-factory'

# 启动服务
ssh gpu 'sudo supervisorctl start trap-factory'

# 重启服务
ssh gpu 'sudo supervisorctl restart trap-factory'
```

### 手动安装 Supervisor（可选）

如果需要在服务器上手动安装 supervisor 配置：

```bash
# 1. 上传配置文件
scp supervisor/trap-factory.conf gpu:/tmp/

# 2. 在服务器上执行安装脚本
ssh gpu 'cd /tmp && sudo bash -c "$(curl -fsSL <install.sh_url>)"'
# 或手动执行
ssh gpu 'sudo cp /tmp/trap-factory.conf /etc/supervisor/conf.d/ && sudo supervisorctl reread && sudo supervisorctl update'
```

## 使用方法

程序支持两种运行模式：`build` 和 `server`

**查看帮助信息：**

```bash
# 显示帮助信息
./factory-linux help
# 或
./factory-linux -h
# 或
./factory-linux --help
```

### build 模式

直接生成靓号地址的命令行模式。

**基本格式：**
```
<可执行文件> build <模仿的地址> <前几位一致> <后几位一致> <生成几个地址>
```

**参数说明：**
- `模仿的地址`: 要匹配的目标地址模板（例如：`TTTCqtavqZiKEMVYgEQSN2b91h88888888`）
- `前几位一致`: 地址前缀需要匹配的字符数（例如：`1` 表示第一位必须是 T）
- `后几位一致`: 地址后缀需要匹配的字符数（例如：`3` 表示后三位必须是 888）
- `生成几个地址`: 生成多少个匹配的地址后退出（例如：`1` 表示生成 1 个后退出）

**Windows 示例：**

```bash
# 生成第一位是 T, 后三位是 888 的靓号, 生成 1 个匹配的地址后退出
.\factory-win.exe build TTTCqtavqZiKEMVYgEQSN2b91h88888888 1 3 1

# 生成第一位是 T, 后四位是 6666 的靓号, 生成 5 个匹配的地址后退出
.\factory-win.exe build TTTCqtavqZiKEMVYgEQSN2b91h66666666 1 4 5
```

**Linux 示例：**

```bash
# 生成第一位是 T, 后三位是 888 的靓号, 生成 1 个匹配的地址后退出
./factory-linux build TTTCqtavqZiKEMVYgEQSN2b91h88888888 1 3 1

# 生成第一位是 T, 后四位是 6666 的靓号, 生成 5 个匹配的地址后退出
./factory-linux build TTTCqtavqZiKEMVYgEQSN2b91h66666666 1 4 5
```

**输出格式：**
```
<私钥> <地址> (生成数量: <已生成的地址总数>)
```

### server 模式

作为服务器模式运行，从 Redis 队列中获取任务并处理。

**配置说明：**

Redis 连接配置存储在 `config.env` 文件中，使用 `go:embed` 在编译时嵌入到二进制文件中。这样打包后的可执行文件包含了配置信息，无需额外的配置文件。

**修改配置：**

在编译前，编辑 `config.env` 文件：

```env
REDIS_ADDR=144.172.76.40:16379
REDIS_PASSWORD=fCn3XMS5bGj2Uds6
REDIS_DB=0
REDIS_POOL_SIZE=10
REDIS_MIN_IDLE_CONNS=5
REDIS_DIAL_TIMEOUT=5s
REDIS_READ_TIMEOUT=3s
REDIS_WRITE_TIMEOUT=3s
```

修改后需要重新编译程序，配置才会生效。

**启动方式：**

```bash
# 使用编译后的可执行文件
./factory-linux server
# 或
.\factory-win.exe server

# 或使用 Makefile 直接运行
make server
```

**工作原理：**

1. 程序从嵌入的 `config.env` 读取 Redis 连接配置
2. 连接到 Redis 服务器
3. 监听输入队列 `address_producer`，等待任务
4. 处理任务并生成靓号地址
5. 将结果推送到输出队列 `address_consumer`

**任务格式（JSON）：**

```json
{
  "taskId": "任务ID",
  "taskType": "任务类型",
  "customFormat": "自定义格式（可选）"
}
```

**支持的任务类型：**

- `5a`, `6a`, `7a`, `8a`: 生成后 5/6/7/8 位相同的地址（使用 `profanity.txt` 模板）
- `custom_address`: 自定义格式，需要提供 `customFormat` 字段（格式：`前缀-后缀`，例如：`TABC-8888`）

**返回结果格式（JSON）：**

```json
{
  "taskId": "任务ID",
  "status": "completed" | "failed",
  "result": {
    "privateKey": "私钥",
    "address": "地址",
    "totalGenerated": 生成的总数量
  }
}
```

**注意事项：**
- 确保 Redis 服务器可访问
- 确保 `profanity.x64`（Linux）或 `profanity.exe`（Windows）可执行文件存在
- 确保 `profanity.txt` 文件存在（用于 `5a`/`6a`/`7a`/`8a` 任务类型）
- 任务处理超时时间为 30 分钟

## 注意事项

### 如果是在 windows 电脑上运行, 做如下处理

#### 安装显卡驱动

1. 打开 `nvidia` 驱动下载网站：[https://www.nvidia.cn/Download/index.aspx?lang=cn](https://www.nvidia.cn/Download/index.aspx?lang=cn)
2. 根据服务器配置搜索驱动，然后下载
3. 显卡驱动安装完毕后，打开设备管理器，可以查看到显卡信息

#### 安装 `visual studio`

打开 `visual studio` 官网：[https://visualstudio.microsoft.com/zh-hans/vs/](https://visualstudio.microsoft.com/zh-hans/vs/)


### 如果是在 Vast.ai 租用的实例, 做如下处理:

Vast.ai 运行 Profanity 的最佳实践流程

1. 启动实例：选一个 RTX 4090 机器，模板用 vastai/pytorch。
2. 进入终端（SSH 或 Jupyter Terminal）。
3. 修复环境并安装工具（即使镜像里有，执行一下也没坏处）：
```
apt update && apt install -y ocl-icd-libopencl1 clinfo
```
4. 验证 GPU： 运行 `clinfo | grep -i "device name"`。如果输出结果里能看到显卡信息, 例如 `NVIDIA GeForce RTX 4090`，说明环境已经彻底打通。
5. 运行你的程序：
```
chmod +x profanity.x64
./profanity.x64 --matching profanity.txt
```