# Supervisor 配置功能改进总结

## 问题

用户反馈线上的 supervisorctl 没有安装成功，但是看不到详细的错误信息和安装过程。

## 解决方案

在 deploy.sh 中添加了详细的 supervisor 配置步骤，包括：

1. **询问是否配置** - 部署完成后询问是否配置 supervisor
2. **详细的输出** - 每个步骤都有清晰的输出信息
3. **错误处理** - 每个步骤都检查是否成功，失败时显示错误
4. **自动安装** - 如果 supervisor 未安装，自动安装
5. **步骤进度** - 显示 [1/6]、[2/6] 等进度信息
6. **状态检查** - 配置完成后显示服务状态

## 配置步骤

配置 supervisor 包含 6 个步骤：

1. **检查 supervisor 是否安装**
   - 如果未安装，自动执行 `apt-get install -y supervisor`
   - 显示安装结果

2. **确保 supervisor 服务开机自启**
   - 使用 `systemctl enable supervisor` 或 `update-rc.d`
   - 启动 supervisor 服务

3. **创建日志目录**
   - 创建 `/var/log/trap-factory` 目录
   - 设置权限

4. **复制配置文件**
   - 将配置文件复制到 `/etc/supervisor/conf.d/`

5. **重新加载配置**
   - 执行 `supervisorctl reread`
   - 执行 `supervisorctl update`

6. **重启服务**
   - 执行 `supervisorctl restart trap-factory`
   - 显示服务状态

## 使用示例

### 成功配置

```bash
==========================================
部署完成
==========================================
[INFO] 部署统计:
  - 成功: 3
  - 失败: 0
  - 总计: 3

[SUCCESS] 所有服务器部署成功！

是否配置 supervisor 服务? (y/n) y

==========================================
配置 supervisor 服务
==========================================
[INFO] [1/3] 配置 gpu 的 supervisor...
  [1/6] 检查 supervisor 是否安装...
  [SUCCESS] supervisor 已安装
  [2/6] 确保 supervisor 服务开机自启...
  [SUCCESS] supervisor 服务已启用
  [3/6] 创建日志目录...
  [SUCCESS] 日志目录已创建
  [4/6] 复制配置文件...
  [SUCCESS] 配置文件已复制
  [5/6] 重新加载配置...
  [SUCCESS] 配置已重新加载
  [SUCCESS] 配置已更新
  [6/6] 重启服务...
  [SUCCESS] 服务已启动

  服务状态:
trap-factory                     RUNNING   pid 12345, uptime 0:00:01
[SUCCESS] supervisor 配置成功: gpu

[INFO] [2/3] 配置 worker-01 的 supervisor...
  [1/6] 检查 supervisor 是否安装...
  [INFO] supervisor 未安装，正在安装...
  [SUCCESS] supervisor 安装成功
  [2/6] 确保 supervisor 服务开机自启...
  [SUCCESS] supervisor 服务已启用
  [3/6] 创建日志目录...
  [SUCCESS] 日志目录已创建
  [4/6] 复制配置文件...
  [SUCCESS] 配置文件已复制
  [5/6] 重新加载配置...
  [SUCCESS] 配置已重新加载
  [SUCCESS] 配置已更新
  [6/6] 重启服务...
  [SUCCESS] 服务已启动

  服务状态:
trap-factory                     RUNNING   pid 23456, uptime 0:00:01
[SUCCESS] supervisor 配置成功: worker-01

[INFO] supervisor 配置统计:
  - 成功: 2
  - 失败: 0

[SUCCESS] 所有服务器 supervisor 配置成功！
```

### 安装失败

```bash
[INFO] [1/3] 配置 gpu 的 supervisor...
  [1/6] 检查 supervisor 是否安装...
  [INFO] supervisor 未安装，正在安装...
  [ERROR] supervisor 安装失败
[ERROR] supervisor 配置失败: gpu

[INFO] supervisor 配置统计:
  - 成功: 0
  - 失败: 1

[WARNING] 部分服务器 supervisor 配置失败
```

### 服务启动失败

```bash
[INFO] [1/3] 配置 gpu 的 supervisor...
  [1/6] 检查 supervisor 是否安装...
  [SUCCESS] supervisor 已安装
  [2/6] 确保 supervisor 服务开机自启...
  [SUCCESS] supervisor 服务已启用
  [3/6] 创建日志目录...
  [SUCCESS] 日志目录已创建
  [4/6] 复制配置文件...
  [SUCCESS] 配置文件已复制
  [5/6] 重新加载配置...
  [SUCCESS] 配置已重新加载
  [SUCCESS] 配置已更新
  [6/6] 重启服务...
  [ERROR] 服务启动失败
[ERROR] supervisor 配置失败: gpu
```

## 优势

1. **可见性**
   - 每个步骤都有清晰的输出
   - 可以看到具体在哪一步失败
   - 显示详细的错误信息

2. **自动化**
   - 自动检查是否安装
   - 自动安装 supervisor
   - 自动配置和启动服务

3. **错误处理**
   - 每个步骤都检查是否成功
   - 失败时立即停止并显示错误
   - 统计成功和失败的服务器数量

4. **灵活性**
   - 可以选择是否配置 supervisor
   - 跳过环境异常和部署失败的服务器
   - 支持批量配置多个服务器

## 常见问题排查

### 1. supervisor 安装失败

**可能原因：**
- apt 源不可用
- 网络连接问题
- 权限不足

**排查方法：**
```bash
# 手动测试安装
ssh <服务器> 'apt-get update && apt-get install -y supervisor'

# 检查 apt 源
ssh <服务器> 'apt-get update'

# 检查权限
ssh <服务器> 'whoami'
```

### 2. 配置重新加载失败

**可能原因：**
- supervisor 服务未启动
- 配置文件语法错误
- 权限问题

**排查方法：**
```bash
# 检查 supervisor 服务状态
ssh <服务器> 'systemctl status supervisor'

# 检查配置文件
ssh <服务器> 'cat /etc/supervisor/conf.d/trap-factory.conf'

# 手动重新加载
ssh <服务器> 'supervisorctl reread'
```

### 3. 服务启动失败

**可能原因：**
- 程序文件不存在
- 程序执行权限问题
- 程序运行时错误（如 GPU 环境未配置）

**排查方法：**
```bash
# 检查程序文件
ssh <服务器> 'ls -la /srv/trap-factory/factory-linux'

# 检查执行权限
ssh <服务器> 'chmod +x /srv/trap-factory/factory-linux'

# 手动运行程序
ssh <服务器> '/srv/trap-factory/factory-linux server'

# 查看日志
ssh <服务器> 'tail -f /var/log/trap-factory/stderr.log'
```

## 与之前的对比

### 之前
- 没有 supervisor 配置步骤
- 需要手动配置
- 看不到配置过程
- 不知道哪里失败

### 现在
- 自动配置 supervisor
- 详细的步骤输出
- 清晰的错误信息
- 可以快速定位问题

## 后续优化建议

1. **日志保存**
   - 将配置过程保存到日志文件
   - 便于事后排查问题

2. **健康检查**
   - 配置完成后检查服务是否正常运行
   - 检查日志是否有错误

3. **回滚功能**
   - 如果配置失败，自动回滚
   - 恢复之前的配置

4. **配置验证**
   - 在上传前验证配置文件语法
   - 避免上传错误的配置
