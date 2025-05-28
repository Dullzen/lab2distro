.PHONY: entrenador

entrenador:
	@echo "Construyendo y desplegando Entrenador..."
	sudo docker build -t entrenador-image .
	sudo docker run -it --name entrenador --network host entrenador-image

stop:
	sudo docker stop entrenador || true
	sudo docker rm entrenador || true

clean: stop
	sudo docker rmi entrenador-image || true