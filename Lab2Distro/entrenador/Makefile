.PHONY: entrenador

docker-entrenadores:
	@-sudo docker stop entrenador 2>/dev/null || true
	@-sudo docker rm entrenador 2>/dev/null || true
	@sudo docker build -t entrenador-image . >/dev/null
	@sudo docker run -it --name entrenador --network host entrenador-image

stop:
	@sudo docker stop entrenador || true
	@sudo docker rm entrenador || true

clean: stop
	@sudo docker rmi entrenador-image || true