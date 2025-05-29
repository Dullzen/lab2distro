.PHONY: cdp

cdp:
	-sudo docker stop cdp 2>/dev/null || true
	-sudo docker rm cdp 2>/dev/null || true
	sudo docker build -t cdp-image .
	sudo docker run --name cdp --network host cdp-image

stop:
	sudo docker stop cdp || true
	sudo docker rm cdp || true

clean: stop
	sudo docker rmi cdp-image || true