.PHONY: gimnasios

gimnasios:
	@echo "Construyendo y desplegando Gimnasios..."
	sudo docker build -t gimnasios-image .
	sudo docker run -d --name gimnasios --network host gimnasios-image

stop:
	sudo docker stop gimnasios || true
	sudo docker rm gimnasios || true

clean: stop
	sudo docker rmi gimnasios-image || true