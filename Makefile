export GOOS=linux
IMAGE=kavatech/iptableslb
TAG=0.0.0

all: build

build:
	go build
	go test -c

docker-build: build
	docker build -t $(IMAGE):$(TAG) . -f Dockerfile

tests: docker-build
	docker run --privileged -it $(IMAGE):$(TAG) /usr/bin/iptableslb.test -test.failfast -test.v #-v 4 -alsologtostderr

sh: docker-build
	docker run --privileged -it $(IMAGE):$(TAG) /bin/bash