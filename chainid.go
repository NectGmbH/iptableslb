package main

import (
    "encoding/base64"
    "encoding/binary"
    "fmt"
    "net"
)

// ChainState represent the state of the current chain
type ChainState byte

const (
    // ChainCreating means that the chain got created but not completly filled yet
    ChainCreating ChainState = 0x00

    // ChainCreated means that everything is added to the chain
    ChainCreated ChainState = 0x01
)

func (c ChainState) String() string {
    switch c {
    case ChainCreating:
        return "creating"
    case ChainCreated:
        return "created"
    default:
        return "unknown"
    }
}

const chainIDPrefix = "LB$-"

//   00 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17
//  +--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
//  +CR|PR|     IP    | Port|Last Update|St|ContentHash|
//  +--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+--+
//      \__________________/
//             =CR

// ChainID represents the name of a chain which contains the most important data of it
type ChainID struct {
    CRC         uint8
    Protocol    Protocol
    IP          net.IP
    Port        uint16
    LastUpdate  uint32
    State       ChainState
    ContentHash uint32
}

// NewChainID creates a new chain identification
func NewChainID(protocol Protocol, ip net.IP, port uint16, lastUpdate uint32, state ChainState, contentHash uint32) ChainID {
    id := ChainID{}

    crcBuf := make([]byte, 7)
    crcBuf[0] = byte(protocol)

    ipv4 := ip.To4()
    crcBuf[1] = ipv4[0]
    crcBuf[2] = ipv4[1]
    crcBuf[3] = ipv4[2]
    crcBuf[4] = ipv4[3]

    binary.BigEndian.PutUint16(crcBuf[5:], port)

    id.CRC = PearsonHash(crcBuf)
    id.Protocol = protocol
    id.IP = ip
    id.Port = port
    id.LastUpdate = lastUpdate
    id.State = state
    id.ContentHash = contentHash

    return id
}

// TryParseChainID tries to parse the passed chainname as ChainID
func TryParseChainID(chain string) (ChainID, error) {
    id := ChainID{}

    nameLength := len(chain)
    if len(chain) != 28 {
        return ChainID{}, fmt.Errorf("chain `%s` has invalid length, got %d expected 28", chain, nameLength)
    }

    if chain[0:len(chainIDPrefix)] != chainIDPrefix {
        return ChainID{}, fmt.Errorf("chain `%s` doens't start with prefix `%s`", chain, chainIDPrefix)
    }

    data, err := base64.StdEncoding.DecodeString(chain[len(chainIDPrefix):])
    if err != nil {
        return ChainID{}, fmt.Errorf("chain `%s` isn't valid base64", chain)
    }

    id.CRC = data[0]
    id.Protocol = Protocol(data[1])
    id.IP = net.IPv4(data[2], data[3], data[4], data[5])
    id.Port = binary.BigEndian.Uint16(data[6:8])
    id.LastUpdate = binary.BigEndian.Uint32(data[8:12])
    id.State = ChainState(data[12])
    id.ContentHash = binary.BigEndian.Uint32(data[13:17])

    checksum := PearsonHash(data[1:8])
    if checksum != id.CRC {
        return ChainID{}, fmt.Errorf("chain `%s` has invalid CRC, got %d expected %d", chain, id.CRC, checksum)
    }

    return id, nil
}

// AsLoadbalancerKey creates a token which can be used to match ChainID to loadbalancers
func (c ChainID) AsLoadbalancerKey() string {
    return fmt.Sprintf("%s://%s:%d", c.Protocol.String(), c.IP.String(), c.Port)
}

// String serializes the id to a iptables compatible chain name
func (c ChainID) String() string {
    buf := make([]byte, 17)

    buf[0] = c.CRC
    buf[1] = byte(c.Protocol)

    ipv4 := c.IP.To4()
    buf[2] = ipv4[0]
    buf[3] = ipv4[1]
    buf[4] = ipv4[2]
    buf[5] = ipv4[3]

    binary.BigEndian.PutUint16(buf[6:], c.Port)
    binary.BigEndian.PutUint32(buf[8:], c.LastUpdate)
    buf[12] = byte(c.State)
    binary.BigEndian.PutUint32(buf[13:], c.ContentHash)

    b64 := base64.StdEncoding.EncodeToString(buf)
    chainName := chainIDPrefix + b64

    return chainName
}
