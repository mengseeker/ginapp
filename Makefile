image ?= app:latest

.PHONY: docker_build
docker_build:
	docker build -t ${image} -f docker/Dockerfile .

compose_up:
	docker-compose -f docker/docker-compose.yaml build
	docker-compose -f docker/docker-compose.yaml up

compose_down:
	docker-compose -f docker/docker-compose.yaml down

tidy:
	go mod tidy -compat=1.17

vendor: tidy
	go mod vendor

generate:
	go generate ./...