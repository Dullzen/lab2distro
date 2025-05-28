.PHONY: gimnasios

gimnasios:
	@echo "Construyendo y desplegando Gimnasios..."
	docker build -t gimnasios-image .
	docker run -d --name gimnasios --network host gimnasios-image

stop:
	docker stop gimnasios || true
	docker rm gimnasios || true

clean: stop
	docker rmi gimnasios-image || true