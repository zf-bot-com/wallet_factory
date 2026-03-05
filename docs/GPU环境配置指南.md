# GPU 环境配置指南

## 概述

本程序使用 OpenCL 来调用 GPU 进行地址生成，需要正确配置 GPU 环境才能运行。程序启动时会自动检查 GPU 环境是否配置正确。

## 环境要求

- NVIDIA GPU（推荐 RTX 4090 或更高）
- NVIDIA 驱动
- OpenCL 运行时库

## 在 vast.ai 上配置环境

### 1. 选择实例

1. 登录 vast.ai
2. 选择一个 RTX 4090 机器
3. 模板选择 `vastai/pytorch`
4. 启动实例

### 2. 进入终端

通过 SSH 或 Jupyter Terminal 连接到实例。

### 3. 安装 OpenCL 工具

```bash
apt update && apt install -y ocl-icd-libopencl1 clinfo
```

**说明：**
- `ocl-icd-libopencl1` - OpenCL 运行时库
- `clinfo` - OpenCL 信息查询工具

### 4. 验证 GPU

运行以下命令验证 GPU 是否可用：

```bash
clinfo | grep -i "device name"
```

**预期输出：**
```
  Device Name                                     NVIDIA GeForce RTX 4090
```

如果能看到显卡信息，说明环境已经配置成功。

## 在其他环境配置

### Ubuntu/Debian

```bash
# 安装 NVIDIA 驱动（如果未安装）
sudo apt update
sudo apt install -y nvidia-driver-535

# 安装 OpenCL 工具
sudo apt install -y ocl-icd-libopencl1 clinfo

# 验证
clinfo | grep -i "device name"
```

### CentOS/RHEL

```bash
# 安装 NVIDIA 驱动（如果未安装）
sudo yum install -y nvidia-driver

# 安装 OpenCL 工具
sudo yum install -y ocl-icd clinfo

# 验证
clinfo | grep -i "device name"
```

## 程序启动检查

程序启动时会自动执行以下检查：

1. **检查 clinfo 是否安装**
   - 如果未安装，程序会提示安装命令并退出

2. **检查是否能检测到 GPU 设备**
   - 运行 `clinfo` 命令
   - 检查输出中是否包含 GPU 设备信息
   - 显示检测到的 GPU 名称

3. **检查通过后启动服务**
   - 连接 Redis
   - 开始监听任务队列

## 启动输出示例

### 成功启动

```bash
$ ./factory-linux server

检查 GPU 环境...
✓ clinfo 已安装
✓ 检测到 GPU:   Device Name                                     NVIDIA GeForce RTX 4090
✓ GPU 环境检查通过
Redis 连接成功
Worker 启动: worker-gpu-01
开始监听队列: address_producer
```

### 环境未配置

```bash
$ ./factory-linux server

检查 GPU 环境...
❌ GPU 环境检查失败:
clinfo 未安装，请先安装 OpenCL 工具:
  apt update && apt install -y ocl-icd-libopencl1 clinfo

请按照以下步骤配置环境:
1. 安装 OpenCL 工具:
   apt update && apt install -y ocl-icd-libopencl1 clinfo
2. 验证 GPU:
   clinfo | grep -i "device name"
3. 如果看到 GPU 信息（如 NVIDIA GeForce RTX 4090），说明环境已配置成功
程序退出
```

## 常见问题

### 1. clinfo 未安装

**错误信息：**
```
clinfo 未安装，请先安装 OpenCL 工具
```

**解决方法：**
```bash
apt update && apt install -y ocl-icd-libopencl1 clinfo
```

### 2. 未检测到 GPU 设备

**错误信息：**
```
未检测到 GPU 设备
```

**可能原因：**
1. NVIDIA 驱动未安装或未正确加载
2. OpenCL 运行时库未安装
3. GPU 不支持 OpenCL

**解决方法：**
```bash
# 检查 NVIDIA 驱动
nvidia-smi

# 如果 nvidia-smi 无法运行，需要安装驱动
apt install -y nvidia-driver-535

# 重新安装 OpenCL 工具
apt install -y --reinstall ocl-icd-libopencl1 clinfo

# 验证
clinfo
```

### 3. clinfo 运行失败

**错误信息：**
```
运行 clinfo 失败
```

**解决方法：**
```bash
# 查看详细错误信息
clinfo

# 检查 OpenCL 库
ldconfig -p | grep OpenCL

# 如果没有输出，重新安装
apt install -y --reinstall ocl-icd-libopencl1
```

## 手动验证环境

在启动程序前，可以手动验证环境：

```bash
# 1. 检查 clinfo 是否安装
which clinfo

# 2. 查看 GPU 信息
clinfo | grep -i "device name"

# 3. 查看完整的 OpenCL 信息
clinfo

# 4. 检查 NVIDIA 驱动
nvidia-smi
```

## 性能优化建议

1. **使用高性能 GPU**
   - 推荐 RTX 4090 或更高
   - 更多 CUDA 核心 = 更快的生成速度

2. **确保 GPU 驱动最新**
   ```bash
   # 更新驱动
   apt update && apt upgrade -y nvidia-driver-535
   ```

3. **监控 GPU 使用率**
   ```bash
   # 实时监控
   watch -n 1 nvidia-smi
   ```

## 在 Docker 中运行

如果在 Docker 容器中运行，需要：

1. **使用 NVIDIA Docker 运行时**
   ```bash
   docker run --gpus all -it your-image
   ```

2. **安装 OpenCL 工具**
   ```dockerfile
   RUN apt update && apt install -y ocl-icd-libopencl1 clinfo
   ```

3. **验证 GPU 可用**
   ```bash
   docker exec -it container-name clinfo
   ```

## 参考资料

- [NVIDIA CUDA Toolkit](https://developer.nvidia.com/cuda-toolkit)
- [OpenCL 官方文档](https://www.khronos.org/opencl/)
- [vast.ai 文档](https://vast.ai/docs/)
