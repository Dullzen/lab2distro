.PHONY: gimnasios

docker-gimnasio:
	@-sudo docker stop gimnasios 2>/dev/null || true
	@-sudo docker rm gimnasios 2>/dev/null || true
	@sudo docker build -t gimnasios-image . >/dev/null
	@sudo docker run --name gimnasios --network host -e RABBITMQ_HOST=10.35.168.64 -e RABBITMQ_USER=pokemon -e RABBITMQ_PASS=pokemon123 gimnasios-image

stop:
	@sudo docker stop gimnasios || true
	@sudo docker rm gimnasios || true

clean: stop
	@sudo docker rmi gimnasios-image || true