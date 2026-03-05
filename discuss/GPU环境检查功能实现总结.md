# GPU 环境检查功能实现总结

## 实现内容

本次更新添加了 GPU 环境自动检查功能，确保程序启动前 OpenCL 环境已正确配置。

## 主要改动

### 1. main.go 更新

添加了 `checkGPUEnvironment()` 函数，在程序启动时自动检查：

**检查项目：**
1. **clinfo 是否安装**
   - 使用 `which clinfo` 检查
   - 如果未安装，提示安装命令

2. **GPU 设备是否可用**
   - 运行 `clinfo` 命令
   - 检查输出中是否包含 GPU 设备信息
   - 提取并显示 GPU 名称

3. **检查失败处理**
   - 显示详细的错误信息
   - 提供配置步骤说明
   - 程序退出，避免运行时错误

**代码位置：**
- `main.go:256-310` - `checkGPUEnvironment()` 函数
- `main.go:312` - 在 `server()` 函数开始时调用检查

### 2. 文档更新

创建了详细的 GPU 环境配置文档：

**新增文档：**
- `docs/GPU环境配置指南.md` - 完整的配置指南
  - vast.ai 配置步骤
  - 其他环境配置方法
  - 常见问题解决
  - 手动验证方法

**更新文档：**
- `README.md` - 添加环境要求说明

### 3. deploy.sh 修复

修复了第 14 行的错误字符 "s"。

## 使用场景

### 场景 1：在 vast.ai 上首次启动

```bash
# 1. 启动 RTX 4090 实例（模板：vastai/pytorch）

# 2. 安装 OpenCL 工具
apt update && apt install -y ocl-icd-libopencl1 clinfo

# 3. 验证 GPU
clinfo | grep -i "device name"
# 输出：Device Name    NVIDIA GeForce RTX 4090

# 4. 启动程序
./factory-linux server
```

### 场景 2：环境未配置时启动

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

### 场景 3：环境配置正确时启动

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

## 检查流程

```
程序启动
    ↓
检查 clinfo 是否安装
    ↓ 是
运行 clinfo 命令
    ↓
检查输出中是否有 GPU 设备
    ↓ 是
提取并显示 GPU 名称
    ↓
检查通过，继续启动
    ↓
连接 Redis
    ↓
开始监听任务队列
```

如果任何检查失败：
```
检查失败
    ↓
显示错误信息
    ↓
显示配置步骤
    ↓
程序退出
```

## 优势

1. **提前发现问题**
   - 在程序启动时就检查环境
   - 避免运行时错误
   - 节省调试时间

2. **友好的错误提示**
   - 清晰的错误信息
   - 详细的配置步骤
   - 易于理解和解决

3. **自动化检查**
   - 无需手动验证
   - 每次启动都会检查
   - 确保环境始终正确

4. **详细的文档**
   - 完整的配置指南
   - 常见问题解决
   - 多种环境支持

## 常见问题

### 1. clinfo 未安装

**错误：**
```
clinfo 未安装，请先安装 OpenCL 工具
```

**解决：**
```bash
apt update && apt install -y ocl-icd-libopencl1 clinfo
```

### 2. 未检测到 GPU

**错误：**
```
未检测到 GPU 设备
```

**解决：**
```bash
# 检查驱动
nvidia-smi

# 重新安装 OpenCL
apt install -y --reinstall ocl-icd-libopencl1 clinfo

# 验证
clinfo
```

### 3. clinfo 运行失败

**错误：**
```
运行 clinfo 失败
```

**解决：**
```bash
# 查看详细错误
clinfo

# 检查 OpenCL 库
ldconfig -p | grep OpenCL
```

## 测试建议

1. **测试未安装 clinfo**
   ```bash
   # 卸载 clinfo
   apt remove clinfo

   # 启动程序，应该提示安装
   ./factory-linux server
   ```

2. **测试正常环境**
   ```bash
   # 安装 clinfo
   apt install -y clinfo

   # 启动程序，应该检查通过
   ./factory-linux server
   ```

3. **测试 GPU 不可用**
   ```bash
   # 在没有 GPU 的机器上启动
   # 应该提示未检测到 GPU
   ```

## 后续优化建议

1. **更详细的 GPU 信息**
   - 显示 GPU 型号
   - 显示 CUDA 核心数
   - 显示显存大小

2. **性能基准测试**
   - 启动时运行简单的性能测试
   - 估算生成速度
   - 给出性能建议

3. **自动安装**
   - 检测到未安装时，询问是否自动安装
   - 自动执行安装命令
   - 安装后重新检查

4. **配置缓存**
   - 首次检查通过后缓存结果
   - 后续启动跳过检查（可选）
   - 提供强制检查选项

## 相关文档

- [docs/GPU环境配置指南.md](docs/GPU环境配置指南.md) - 完整配置指南
- [README.md](README.md) - 环境要求说明
