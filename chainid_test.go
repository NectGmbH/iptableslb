package main

import (
	"encoding/base64"
	"encoding/binary"
	"net"
	"testing"
)

// TestChainIDSerializeDeserialize tests whether the serialization and deserialization of the chainid works.
func TestChainIDSerializeDeserialize(t *testing.T) {
	protocol := ProtocolUDP
	ip := net.IPv4(0xC0, 0xA8, 0x2A, 0x45)
	port := uint16(1337)
	lastUpdate := uint32(4294967295)
	state := ChainCreated
	contentHash := uint32(42133742)

	inChain := NewChainID(protocol, ip, port, lastUpdate, state, contentHash)
	expectedName := "LB$-7wLAqCpFBTn/////AQKC6O4="
	gotName := inChain.String()

	if gotName != expectedName {
		t.Fatalf("chain name mismatch, got %s expected %s", gotName, expectedName)
	}

	c, err := TryParseChainID(gotName)
	if err != nil {
		t.Fatalf("couldn't deserialize chain name, see: %v", err)
	}

	if c.Protocol != protocol {
		t.Fatalf("protocol mismatch after serializing, got %s expected %s", c.Protocol.String(), protocol.String())
	}

	if !c.IP.Equal(ip) {
		t.Fatalf("ip mismatch after serializing, got %s expected %s", c.IP.String(), ip.String())
	}

	if c.Port != port {
		t.Fatalf("port mismatch after serializing, got %d expected %d", c.Port, port)
	}

	if c.LastUpdate != lastUpdate {
		t.Fatalf("lastUpdate mismatch after serializing, got %d expected %d", c.LastUpdate, lastUpdate)
	}

	if c.State != state {
		t.Fatalf("state mismatch after serializing, got %s expected %s", c.State.String(), state.String())
	}
}

// TestChainIDChecksumMismatch checks whether the CRC logic works
func TestChainIDChecksumMismatch(t *testing.T) {
	protocol := ProtocolUDP
	ip := net.IPv4(0xC0, 0xA8, 0x2A, 0x45)
	port := uint16(1337)
	lastUpdate := uint32(4294967295)
	state := ChainCreated
	contentHash := uint32(42133742)

	c := NewChainID(protocol, ip, port, lastUpdate, state, contentHash)
	buf := make([]byte, 17)

	ipv4 := c.IP.To4()
	buf[2] = ipv4[0]
	buf[3] = ipv4[1]
	buf[4] = ipv4[2]
	buf[5] = ipv4[3]

	binary.BigEndian.PutUint16(buf[6:], c.Port)
	binary.BigEndian.PutUint32(buf[8:], c.LastUpdate)
	buf[12] = byte(c.State)
	binary.BigEndian.PutUint32(buf[13:], c.ContentHash)

	buf[0] = 0x42
	buf[1] = byte(c.Protocol)

	str := chainIDPrefix + base64.StdEncoding.EncodeToString(buf)

	_, err := TryParseChainID(str)
	if err == nil || err.Error() != "chain `LB$-QgLAqCpFBTn/////AQKC6O4=` has invalid CRC, got 66 expected 239" {
		t.Fatalf("Expected checksum mismatch, but got `%s`", err)
	}
}
