.PHONY: all build run-local stop-local test clean

all: build

build:
	go build ./...

test:
	go test ./...

run-local:
	docker compose up --build -d
	@echo "Local Kiwi Compose stack is running!"
	@echo "API Server: http://localhost:8080"
	@echo "MinIO Console: http://localhost:9001 (admin/kiwipassword)"

stop-local:
	docker compose down -v
	@echo "Local Compose stack stopped and volumes removed."

clean:
	rm -f kiwid

deploy-validate:
	docker compose -f deploy/docker-compose.prod.yml config -q

