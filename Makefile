fmt:
	go fmt *.go
	go fmt cmd/iiif-process-ecs/*.go
	go fmt ecs/*.go

tools:
	go build -o bin/iiif-process-ecs cmd/iiif-process-ecs/main.go

docker-process:
	if test ! -f $(CONFIG); then echo "missing config file" && exit 1; fi
	if test ! -f $(INSTRUCTIONS); then echo "missing instructions file" && exit 1; fi
	if test -d tmp; then rm -rf tmp; fi
	mkdir tmp
	cp $(CONFIG) tmp/config.json
	cp $(INSTRUCTIONS) tmp/instructions.json
	docker build -f Dockerfile.process.ecs -t go-iiif-process-ecs --build-arg GO_IIIF_CONFIG=tmp/config.json --build-arg GO_IIIF_INSTRUCTIONS=tmp/instructions.json .
	rm -rf tmp

lambda: lambda-process

lambda-process:
	@make self
	if test -f main; then rm -f main; fi
	if test -f process-task.zip; then rm -f process-task.zip; fi
	GOOS=linux go build -o main cmd/iiif-process-ecs.go
	zip process-task.zip main
	rm -f main

