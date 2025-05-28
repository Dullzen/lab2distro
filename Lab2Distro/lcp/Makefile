# Makefile para lcp

all: run

build:
	docker build -t lcp .

run: build
	docker rm -f lcp || true
	docker run --rm -it -p 50051:50051 --name lcp lcp

clean:
	docker rmi -f lcp