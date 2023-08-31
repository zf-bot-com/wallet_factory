linux:
	GOOS=linux GOARCH=amd64 go build -o wallet-linux main.go

win:
	GOOS=windows GOARCH=amd64 go build -o wallet.exe main.go

mac:
	GOOS=darwin GOARCH=amd64 go build -o wallet-mac main.go

run:
	go run main.go