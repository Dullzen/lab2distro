# Makefile para entrenador

all: run

build:
	docker build -t  entrenador .

run: build
	docker rm -f entrenador || true
	docker run --rm -it -p 50053:50053 --name entrenador entrenador

clean:
	docker rmi -f entrenador