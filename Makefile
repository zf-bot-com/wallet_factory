linux:
	GOOS=linux GOARCH=amd64 go build -o factory-linux main.go

win:
	GOOS=windows GOARCH=amd64 go build -o factory-win.exe main.go

init:
	go clean -modcache
	go mod init
	go mod tidy
	
server:
	go run main.go server

build:
	go run main.go build

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

deploy:
	@echo "使用方法: ./deploy.sh [服务器地址] [服务器路径]"
	@echo "默认: ./deploy.sh gpu /srv"
	@./deploy.sh $(SERVER) $(SERVER_PATH)

clean:
	rm -f cache*