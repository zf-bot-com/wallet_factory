# 构建和部署快速参考

## 本地编译

```bash
# 开发环境（默认）
make linux          # Linux 版本
make mac            # macOS 版本
make win            # Windows 版本

# 生产环境
make linux ENV=prod
make mac ENV=prod
make win ENV=prod
```

## 部署到服务器

### 批量部署（推荐）

```bash
# 部署生产环境到所有配置的服务器
./deploy.sh prod

# 部署开发环境到所有配置的服务器
./deploy.sh dev
```

**服务器配置：**
- 编辑 `servers.env.prod` 配置生产环境服务器列表
- 编辑 `servers.env.dev` 配置开发环境服务器列表

**配置格式：**
```bash
# 只指定服务器地址（使用默认路径 /srv）
gpu
worker-01
worker-02

# 或指定服务器地址和路径
gpu:/srv
worker-01:/home/user/trap-factory
```

详细说明请查看 [docs/批量部署说明.md](docs/批量部署说明.md)

## 清理

```bash
# 清理缓存文件
make clean
```

## 配置文件

### 环境配置
- `config.env.dev` - 开发环境（本地 Redis）
- `config.env.prod` - 生产环境（远程 Redis）
- `config.env` - 临时文件（自动生成，不要手动编辑）

### 服务器配置
- `servers.env.dev` - 开发环境服务器列表
- `servers.env.prod` - 生产环境服务器列表
- `servers.env.example` - 配置示例

## 详细文档

- [docs/多环境配置说明.md](docs/多环境配置说明.md) - 多环境配置详细说明
- [docs/批量部署说明.md](docs/批量部署说明.md) - 批量部署详细说明
- [docs/多环境配置使用示例.md](docs/多环境配置使用示例.md) - 使用示例
