.PHONY: lcp

docker-lcp:
	@-sudo docker stop lcp 2>/dev/null || true
	@-sudo docker rm lcp 2>/dev/null || true
	@sudo docker build -t lcp-image . >/dev/null
	@sudo docker run --name lcp --network host lcp-image

stop:
	@sudo docker stop lcp || true
	@sudo docker rm lcp || true

clean: stop
	@sudo docker rmi lcp-image || true

cleanall: clean
