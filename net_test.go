package main

import (
	"gotest.tools/assert"
	"net"
	"testing"
)

func TestParseIPsSingleCorrect(t *testing.T) {
	endpoints, err := TryParseEndpoints("192.168.0.5:80")
	assert.NilError(t, err)

	expected := []Endpoint{
		{IP: net.IPv4(192, 168, 0, 5), Port: 80},
	}

	assert.DeepEqual(t, endpoints, expected)
}

func TestParseIPsSingleIncorrect(t *testing.T) {
	_, err := TryParseEndpoints("192.168.0.:80")
	assert.Error(t, err, "couldn't parse `192.168.0.:80` as ip")
}

func TestParseIPsMultipleCorrect(t *testing.T) {
	endpoints, err := TryParseEndpoints("192.168.0.5:80,192.168.14.7:81")
	assert.NilError(t, err)

	expected := []Endpoint{
		{IP: net.IPv4(192, 168, 0, 5), Port: 80},
		{IP: net.IPv4(192, 168, 14, 7), Port: 81},
	}

	assert.DeepEqual(t, endpoints, expected)
}

func TestParseIPsMultipleIncorrect(t *testing.T) {
	_, err := TryParseEndpoints("192.168.0.2:80,")
	assert.Error(t, err, "expected ip:port or ip-max:port but got ``")
}

func TestParseIPsRangeCorrect(t *testing.T) {
	endpoints, err := TryParseEndpoints("192.168.0.5-9:80")
	assert.NilError(t, err)

	expected := []Endpoint{
		{IP: net.IPv4(192, 168, 0, 5), Port: 80},
		{IP: net.IPv4(192, 168, 0, 6), Port: 80},
		{IP: net.IPv4(192, 168, 0, 7), Port: 80},
		{IP: net.IPv4(192, 168, 0, 8), Port: 80},
		{IP: net.IPv4(192, 168, 0, 9), Port: 80},
	}

	assert.DeepEqual(t, endpoints, expected)
}

func TestParseIPsRangeIncorrectMax(t *testing.T) {
	_, err := TryParseEndpoints("192.168.0.5-3:80")
	assert.Error(
		t,
		err,
		"lower address specified in range `192.168.0.5-3:80` is bigger than upper")
}

func TestParseIPsRangeIncorrectMissingMax(t *testing.T) {
	_, err := TryParseEndpoints("192.168.0.5-:80")
	assert.Error(
		t,
		err,
		"couldn't parse max part of ip range `192.168.0.5-:80`, see: strconv.Atoi: parsing \"\": invalid syntax")
}

func TestParseIPsRangeIncorrectMaxTooBig(t *testing.T) {
	_, err := TryParseEndpoints("192.168.0.5-300:80")
	assert.Error(
		t,
		err,
		"invalid maximum ip for range `192.168.0.5-300:80` given")
}
