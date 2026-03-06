# trap_factory

用 golang 写的一个 Tron 靓号生成工具，支持多种条件配置，支持多协程，生成速度快，欢迎使用。

## 环境要求

本程序使用 GPU 进行地址生成，需要：

- NVIDIA GPU（推荐 RTX 4090 或更高）
- NVIDIA 驱动
- OpenCL 运行时库

**首次使用前，请先配置 GPU 环境 (针对 Ubuntu 系统)：**

```bash
# 安装 OpenCL 工具
apt update && apt install -y ocl-icd-libopencl1 clinfo

# 验证 GPU
clinfo | grep -i "device name"
```

详细配置说明请查看 [docs/GPU环境配置指南.md](docs/GPU环境配置指南.md)

## 用户文档

打包后的程序包含详细的用户使用指南：

- **[使用说明.md](使用说明.md)** - 中文用户指南（详细的使用说明、示例和故障排查）
- **[USER_GUIDE.md](USER_GUIDE.md)** - English User Guide (Detailed usage instructions, examples and troubleshooting)

这两个文档内容相同，仅语言不同。建议用户根据语言偏好选择阅读。

## 编译

项目提供了 Makefile 来简化编译过程，支持多环境配置。

### 多环境配置

项目支持在编译时指定不同的环境配置：

```bash
# 编译开发环境版本（默认，使用本地 Redis）
make linux

# 编译生产环境版本（使用远程 Redis）
make linux ENV=prod

# 其他平台
make win ENV=prod
make mac ENV=dev
```

详细说明请查看：
- **[BUILD.md](BUILD.md)** - 快速参考

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

项目提供了自动化部署脚本，支持一次编译，批量部署到多个服务器。

### 批量部署

```bash
# 部署生产环境到所有配置的服务器
./deploy.sh prod

# 部署开发环境到所有配置的服务器
./deploy.sh dev
```

### 配置服务器列表

编辑对应环境的服务器配置文件：

```bash
# 编辑生产环境服务器列表
vim servers.env.prod
```

配置格式：

```bash
# 只指定服务器地址（使用默认路径 /srv）
gpu
worker-01
worker-02

# 或指定服务器地址和路径
gpu:/srv
worker-01:/home/user/trap-factory
```

### 部署流程

脚本会自动完成以下步骤：

1. **编译**：编译一次 Linux 版本（使用指定环境的配置）
2. **打包**：打包一次程序文件
3. **上传**：批量上传到所有配置的服务器
4. **部署**：在每个服务器上解压和安装

详细说明请查看 [docs/批量部署说明.md](docs/批量部署说明.md)

### GPU 驱动预防措施

部署脚本会询问是否配置 GPU 驱动预防措施（推荐配置），包括：

1. **安装监控脚本** - 安装 GPU 状态检测和修复脚本
2. **锁定驱动版本** - 防止系统自动更新 NVIDIA 驱动
3. **启用监控服务** - 自动监控 GPU 驱动状态
4. **定期检查** - 每小时自动检查 GPU 状态

配置后可以使用以下命令：

```bash
# 手动检查 GPU 状态
ssh gpu '/usr/local/bin/check_opencl.sh'

# 查看监控服务状态
ssh gpu 'sudo systemctl status gpu-monitor.service'

# 查看监控日志
ssh gpu 'sudo journalctl -u gpu-monitor.service -f'

# 查看定期检查日志
ssh gpu 'tail -f /var/log/gpu_check.log'
```

详细说明请查看 [docs/GPU_DRIVER_MAINTENANCE.md](docs/GPU_DRIVER_MAINTENANCE.md)

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

#### 靓号缓存系统

程序内置了智能缓存系统，可以显著提升响应速度：

- **后台持续生成**: profanity 在后台持续运行（quit-count=0），不断生成 6-10 位靓号存入本地 SQLite 缓存
- **秒级响应**: 6a-9a 标准任务优先从缓存获取，命中后秒返回结果
- **智能等待**: 缓存未命中时，任务会等待后台进程产出匹配结果
- **GPU 独占管理**: custom_address 等特殊任务需要独占 GPU 时，自动暂停后台进程，完成后恢复
- **私钥加密**: 所有缓存的私钥使用 AES-256-GCM 加密存储，编译时使用 garble 混淆密钥
- **缓存上限**: 6a/7a 各缓存 1000 个，8a+ 无上限

**缓存配置**（config.env）：

```env
CACHE_ENABLED=true
CACHE_AES_KEY=8471cbaa7ba6f001cb8c30fb0f13244b14485ecde73c232568cc563e2dda8a93
CACHE_DB_PATH=./address_cache.db
CACHE_MAX_6A=1000
CACHE_MAX_7A=1000
```

**日志示例**：

```
📦 缓存系统状态: true
🚀 正在启动后台 profanity 进程...
✅ 后台 profanity 已启动 (PID: 22521, 参数: suffix-count=6, quit-count=0)
🎯 后台产出地址 #1: taskType=6a, address=T...888888
💾 缓存写入成功: taskType=6a, address=T...888888
✅ 缓存命中: TaskID=xxx, TaskType=6a, Address=T...888888
```

#### Worker 心跳机制

本程序实现了完整的 Worker 心跳机制，用于与主系统进行状态同步：

- **心跳频率**: 每 30 秒自动发送一次心跳
- **心跳队列**: `worker_heartbeat`（Redis List）
- **Worker 状态**:
  - `idle`: 空闲（在线但未处理任务）
  - `busy`: 忙碌（正在处理任务）
  - `offline`: 离线（准备退出）
- **优雅退出**: 支持 SIGTERM/SIGINT 信号，会等待当前任务完成后安全退出

#### 任务取消机制

本程序支持通过 Redis Pub/Sub 实时取消正在处理的任务：

- **取消频道**: `task_cancel_channel`（Redis Pub/Sub）
- **取消集合**: `cancelled_tasks`（Redis Set，用于预检查）
- **取消通知格式**: JSON 格式，包含 action、taskId、timestamp
- **取消时机**: 可以在任务执行的任何阶段取消
- **取消结果**: 任务会被标记为失败，错误信息为 "任务已被用户取消"

**配置说明：**

Redis 连接配置存储在 `config.env` 文件中，使用 `go:embed` 在编译时嵌入到二进制文件中。

**修改配置：**

在编译前，编辑 `config.env` 文件：

```env
# Redis 配置（支持多实例）
REDIS_1_ADDR=127.0.0.1:6379
REDIS_1_PASSWORD=
REDIS_1_DB=0

# 缓存配置
CACHE_ENABLED=true
CACHE_AES_KEY=8471cbaa7ba6f001cb8c30fb0f13244b14485ecde73c232568cc563e2dda8a93
CACHE_DB_PATH=./address_cache.db
CACHE_MAX_6A=1000
CACHE_MAX_7A=1000
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

1. 程序从嵌入的 `config.env` 读取配置
2. 连接到 Redis 服务器
3. 如果启用缓存，启动后台 profanity 持续生成靓号
4. 监听输入队列 `address_producer`，等待任务
5. 优先从缓存获取结果，未命中则等待后台产出或启动专用进程
6. 将结果推送到输出队列 `address_consumer`

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
- 确保 `profanity.txt` 文件存在（用于标准任务类型）
- 任务处理超时时间为 168 小时（7 天）
- Worker 会自动发送心跳到 `worker_heartbeat` 队列
- 支持优雅退出（Ctrl+C 或 kill 命令）
- 缓存数据库文件 `*.db` 已加入 .gitignore

**测试脚本：**

```bash
# 测试心跳功能
./scripts/test_heartbeat.sh

# 测试任务取消功能
./scripts/test_cancel.sh
```

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