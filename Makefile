# 环境变量，默认为 dev
ENV ?= dev

# 切换环境配置：将 config.env.$(ENV) 复制到 config.env
switch-env:
	@if [ ! -f "config.env.$(ENV)" ]; then \
		echo "❌ 配置文件不存在: config.env.$(ENV)"; \
		exit 1; \
	fi
	@cp config.env.$(ENV) config.env
	@echo "✅ 已切换到 $(ENV) 环境配置"

linux: switch-env
	@echo "编译 Linux 版本 (环境: $(ENV))..."
	GOOS=linux GOARCH=amd64 go build -o factory-linux .
	@echo "✅ 编译完成: factory-linux ($(ENV))"

win:
	@echo "编译 Windows 版本 (环境: $(ENV))..."
	GOOS=windows GOARCH=amd64 go build -o factory-win.exe .
	@echo "✅ 编译完成: factory-win.exe ($(ENV))"

mac:
	@echo "编译 macOS 版本 (环境: $(ENV))..."
	GOOS=darwin GOARCH=amd64 go build -o factory-mac .
	@echo "✅ 编译完成: factory-mac ($(ENV))"

init:
	go clean -modcache
	go mod init
	go mod tidy
	
server:
	go run .

build:
	go run . build

tar:
	rm -f wallet-factory.tar.gz
	tar -czvf wallet-factory.tar.gz factory-win.exe profanity.exe profanity.x64 README.md

tar-linux:
	rm -f trap-factory-linux.tar.gz
	rm -rf trap-factory-linux
	mkdir -p trap-factory-linux
	cp factory-linux profanity.x64 profanity.txt trap-factory-linux/
	tar -czvf trap-factory-linux.tar.gz trap-factory-linux
	rm -rf trap-factory-linux

tar-win:
	rm -f trap-factory-win.tar.gz
	rm -rf trap-factory-win
	mkdir -p trap-factory-win
	cp factory-win.exe profanity.exe profanity.txt trap-factory-win/
	tar -czvf trap-factory-win.tar.gz trap-factory-win
	rm -rf trap-factory-win

tar-mac:
	rm -f trap-factory-mac.tar.gz
	rm -rf trap-factory-mac
	mkdir -p trap-factory-mac
	cp factory-mac profanity.x64 profanity.txt trap-factory-mac/
	tar -czvf trap-factory-mac.tar.gz trap-factory-mac
	rm -rf trap-factory-mac

deploy:
	@echo "使用方法: ./deploy.sh [服务器地址] [服务器路径]"
	@echo "默认: ./deploy.sh gpu /srv"
	@./deploy.sh $(SERVER) $(SERVER_PATH)

clean:
	rm -f cache*