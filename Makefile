CWD=$(shell pwd)
GOPATH := $(CWD)

prep:
	if test -d pkg; then rm -rf pkg; fi

self:   prep rmdeps
	if test -d src; then rm -rf src; fi
	mkdir -p src/github.com/aaronland/go-iiif-aws
	cp -r ecs src/github.com/aaronland/go-iiif-aws/
	cp -r vendor/* src/

rmdeps:
	if test -d src; then rm -rf src; fi 

build:	fmt bin

deps:
	@GOPATH=$(GOPATH) go get -u "github.com/aaronland/go-iiif-uri"
	@GOPATH=$(GOPATH) go get -u "github.com/aws/aws-lambda-go/lambda"
	@GOPATH=$(GOPATH) go get -u "github.com/whosonfirst/go-whosonfirst-aws"
	@GOPATH=$(GOPATH) go get -u "github.com/whosonfirst/go-whosonfirst-cli"
	mv src/github.com/whosonfirst/go-whosonfirst-aws/vendor/github.com/aws/aws-sdk-go src/github.com/aws/

vendor-deps: rmdeps deps
	if test ! -d vendor; then mkdir vendor; fi
	if test -d vendor; then rm -rf vendor; fi
	cp -r src vendor
	find vendor -name '.git' -print -type d -exec rm -rf {} +
	rm -rf src

fmt:
	go fmt *.go
	go fmt cmd/*.go
	go fmt ecs/*.go

bin: 	self
	rm -rf bin/*
	@GOPATH=$(GOPATH) go build -o bin/iiif-process-ecs cmd/iiif-process-ecs.go

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
	@GOPATH=$(GOPATH) GOOS=linux go build -o main cmd/iiif-process-ecs.go
	zip process-task.zip main
	rm -f main

