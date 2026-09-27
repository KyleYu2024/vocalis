BIN := vocalis
IMAGE ?= vocalis:latest

.PHONY: build test vet fmt run docker docker-multiarch clean

build:
	go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/vocalis

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

# 本地跑一份，读取 ./testlib，数据写到 ./data
run: build
	./$(BIN) \
		--library-dir ./testlib \
		--data-dir ./data \
		--addr 127.0.0.1:8080 \
		--scan-interval 0

docker:
	docker build -t $(IMAGE) .

# 同时构建群晖/威联通常见的 amd64 与 arm64
docker-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE) .

clean:
	rm -f $(BIN)
