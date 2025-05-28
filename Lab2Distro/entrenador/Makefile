.PHONY: entrenador

entrenador:
	@echo "Construyendo y desplegando Entrenador..."
	docker build -t entrenador-image .
	docker run -it --name entrenador --network host entrenador-image

stop:
	docker stop entrenador || true
	docker rm entrenador || true

clean: stop
	docker rmi entrenador-image || true