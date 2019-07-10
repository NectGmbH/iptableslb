package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Protocol represents a network protocol (e.g. TCP)
type Protocol byte

const (
	// ProtocolUNK represents an unknown network protocol
	ProtocolUNK Protocol = 0x00

	// ProtocolTCP represents the TCP network protocol
	ProtocolTCP Protocol = 0x01

	// ProtocolUDP represents the UDP network protocol
	ProtocolUDP Protocol = 0x02
)

// String returns the string representation of the current network protocl.
func (p Protocol) String() string {
	switch p {
	case ProtocolTCP:
		return "tcp"
	case ProtocolUDP:
		return "udp"
	default:
		return "unknown"
	}
}

// Endpoint represents an IP:Port tuple
type Endpoint struct {
	IP   net.IP
	Port uint16
}

func (e Endpoint) String() string {
	return fmt.Sprintf("%s:%d", e.IP.String(), e.Port)
}

// Equals checks whether the current endpoint is the same as the passed one
func (a Endpoint) Equals(b Endpoint) bool {
	return a.IP.Equal(b.IP) && a.Port == b.Port
}

func EndpointsContain(endpoints []Endpoint, endpoint Endpoint) bool {
	for _, e := range endpoints {
		if e.Equals(endpoint) {
			return true
		}
	}

	return false
}

func EndpointsAppendUnique(endpoints []Endpoint, endpoint Endpoint) []Endpoint {
	if EndpointsContain(endpoints, endpoint) {
		return endpoints
	}

	return append(endpoints, endpoint)
}

func EndpointsRemove(endpoints []Endpoint, endpoint Endpoint) []Endpoint {
	newEndpoints := make([]Endpoint, 0)

	for _, e := range endpoints {
		if !e.Equals(endpoint) {
			newEndpoints = append(newEndpoints, e)
		}
	}

	return newEndpoints
}

// NewEndpoint creates a new endpoint with the passed arguments.
func NewEndpoint(ip net.IP, port uint16) Endpoint {
	return Endpoint{
		IP:   ip,
		Port: port,
	}
}

// TryParseProtocolEndpoint tries to parse a protocol-endpoint tuple like "tcp://ip:port"
func TryParseProtocolEndpoint(str string) (Protocol, Endpoint, error) {
	splitted := strings.Split(str, "//")
	if len(splitted) != 2 {
		return ProtocolUNK, Endpoint{}, fmt.Errorf("expected string in format schema://ip:port but got `%s`", str)
	}

	prot := ProtocolUNK

	if splitted[0] == "tcp:" {
		prot = ProtocolTCP
	} else if splitted[0] == "udp:" {
		prot = ProtocolUDP
	} else {
		return ProtocolUNK, Endpoint{}, fmt.Errorf("unknown protocol, expected \"tcp\" or \"udp\" but got `%s`", splitted[0])
	}

	endpoint, err := TryParseEndpoint(splitted[1])
	if err != nil {
		return ProtocolUNK, Endpoint{}, fmt.Errorf("couldn't parse endpoint from `%s`, see: %v", splitted[1], err)
	}

	return prot, endpoint, nil
}

// TryParseEndpoint tries to parse to passed string in the format ip:port as endpoint
func TryParseEndpoint(str string) (Endpoint, error) {
	splitted := strings.Split(str, ":")
	if len(splitted) != 2 {
		return Endpoint{}, fmt.Errorf("expected ip:port but got `%s`", str)
	}

	ip := net.ParseIP(splitted[0]).To4()
	port, err := strconv.Atoi(splitted[1])
	if err != nil {
		return Endpoint{}, fmt.Errorf("couldnt parse port, see: %v", err)
	}

	return NewEndpoint(ip, uint16(port)), nil
}

// TryParseEndpoints tries to parse a range of endpoints, e.g. "192.168.0.1:50,192.168.0.5-255:50"
func TryParseEndpoints(ipStr string) ([]Endpoint, error) {
	// 192.168.0.1:50
	// 192.168.0.1-255:50
	// 192.168.0.1:50,192.168.0.5-255:50
	endpoints := make([]Endpoint, 0)

	parts := strings.Split(ipStr, ",")

	for _, p := range parts {
		ipPortParts := strings.Split(p, ":")
		if len(ipPortParts) != 2 {
			return nil, fmt.Errorf("expected ip:port or ip-max:port but got `%s`", p)
		}

		ipPart := ipPortParts[0]
		portPart := ipPortParts[1]
		port, err := strconv.Atoi(portPart)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse port `%s` in `%s`, see: %v", portPart, p, err)
		}

		rangeParts := strings.Split(ipPart, "-")

		if len(rangeParts) > 2 {
			return nil, fmt.Errorf("expected ip or ip range but got `%s`", p)
		}

		ip := net.ParseIP(rangeParts[0])
		if ip == nil {
			return nil, fmt.Errorf("couldn't parse `%s` as ip", p)
		}

		ip = ip.To4()

		if ip == nil {
			return nil, fmt.Errorf("couldn't convert ip `%s` to ipv4", rangeParts[0])
		}

		endpoints = append(endpoints, Endpoint{IP: ip, Port: uint16(port)})

		isRange := len(rangeParts) == 2
		if !isRange {
			continue
		}

		min := int(ip[3])

		max, err := strconv.Atoi(rangeParts[1])
		if err != nil {
			return nil, fmt.Errorf("couldn't parse max part of ip range `%s`, see: %v", p, err)
		}

		if min > max {
			return nil, fmt.Errorf("lower address specified in range `%s` is bigger than upper", p)
		}

		if min == max {
			continue
		}

		if max > 255 {
			return nil, fmt.Errorf(
				"invalid maximum ip for range `%s` given", p)
		}

		for i := min + 1; i <= max; i++ {
			endpoint := Endpoint{
				IP:   net.IPv4(ip[0], ip[1], ip[2], byte(i)),
				Port: uint16(port),
			}

			endpoints = append(endpoints, endpoint)
		}
	}

	return endpoints, nil
}
