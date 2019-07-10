export GOOS=linux
IMAGE=kavatech/iptableslb
TAG=0.1.0

all: build

build:
	go build
	go test -c

docker-build: build
	docker build -t $(IMAGE):$(TAG) . -f Dockerfile

docker-push:
	docker push $(IMAGE):$(TAG)

tests: docker-build
	docker run --privileged --entrypoint /usr/bin/iptableslb.test -it $(IMAGE):$(TAG) -test.failfast -test.v #-v 4 -alsologtostderr

sh: docker-build
	docker run --privileged --entrypoint /bin/bash -it $(IMAGE):$(TAG)

run-example: docker-build
	docker run --privileged -it $(IMAGE):$(TAG) -t 1 -in tcp://192.168.0.1:80 -h http -out 192.168.1.1-5:80,192.168.2.1-200:81 -logtostderr -v 5