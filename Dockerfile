FROM alpine:3.10

RUN apk add bash
RUN apk add iptables

COPY ./iptableslb /usr/bin/iptableslb
COPY ./iptableslb.test /usr/bin/iptableslb.test