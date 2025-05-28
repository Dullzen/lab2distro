.PHONY: lcp

lcp:
	@echo "Construyendo y desplegando LCP..."
	docker build -t lcp-image .
	docker run -d --name lcp --network host lcp-image

rabbitmq:
	@echo "Desplegando RabbitMQ..."
	docker run -d --name rabbitmq --network host rabbitmq:3-management

stop:
	docker stop lcp || true
	docker rm lcp || true

stoprabbitmq:
	docker stop rabbitmq || true
	docker rm rabbitmq || true

clean: stop
	docker rmi lcp-image || true

cleanall: clean stoprabbitmq