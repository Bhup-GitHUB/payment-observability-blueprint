.PHONY: up down logs test traffic clean build

up:
	docker compose up --build -d

down:
	docker compose down -v

logs:
	docker compose logs -f

test:
	go test ./...

traffic:
	bash scripts/traffic.sh

clean:
	docker compose down -v --rmi local
	rm -rf bin/

build:
	go build ./...
