.PHONY: cdp

cdp:
	@echo "Construyendo y desplegando CDP..."
	docker build -t cdp-image .
	docker run -d --name cdp --network host cdp-image

stop:
	docker stop cdp || true
	docker rm cdp || true

clean: stop
	docker rmi cdp-image || true