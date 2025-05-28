.PHONY: lcp

lcp:
	@echo "Construyendo y desplegando LCP..."
	sudo docker build -t lcp-image .
	sudo docker run --name lcp --network host lcp-image

rabbitmq:
	@echo "Desplegando RabbitMQ..."
	sudo docker run -d --name rabbitmq --network host rabbitmq:3-management

stop:
	sudo docker stop lcp || true
	sudo docker rm lcp || true

stoprabbitmq:
	sudo docker stop rabbitmq || true
	sudo docker rm rabbitmq || true

clean: stop
	sudo docker rmi lcp-image || true

cleanall: clean stoprabbitmq