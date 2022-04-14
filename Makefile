image ?= app:latest

.PHONY: generate_src
generate_src:
	go generate ./...

.PHONY: docker_build
docker_build:
	docker build -t ${image} -f Dockerfile .

compose_up:
	docker-compose -f docker/docker-compose.yaml build app
	docker-compose -f docker/docker-compose.yaml up

tidy:
	go mod tidy -compat=1.17

vendor: tidy
	go mod vendor