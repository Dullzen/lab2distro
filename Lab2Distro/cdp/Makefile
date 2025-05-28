.PHONY: cdp

cdp:
	@echo "Construyendo y desplegando CDP..."
	sudo docker build -t cdp-image .
	sudo docker run -d --name cdp --network host cdp-image

stop:
	sudo docker stop cdp || true
	sudo docker rm cdp || true

clean: stop
	sudo docker rmi cdp-image || true