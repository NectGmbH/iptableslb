package main

import (
	"fmt"
	"time"
)

// Loadbalancer represents an mapping between the public endpoint and all target endpoints
type Loadbalancer struct {
	LastUpdate uint32
	Protocol   Protocol
	Input      Endpoint
	Outputs    []Endpoint
}

// NewLoadbalancer creates a new loadbalancer instance from the passed arguments.
func NewLoadbalancer(proto Protocol, input Endpoint, outputs ...Endpoint) *Loadbalancer {
	lb := &Loadbalancer{
		Protocol: proto,
		Input:    input,
		Outputs:  outputs,
	}

	lb.MarkUpdated()

	return lb
}

// MarkUpdated updated the lastUpdate timer on the lb signalling the controller that it has to update the iptables rules.
func (lb *Loadbalancer) MarkUpdated() {
	lb.LastUpdate = uint32(time.Now().Unix())
}

// Key gets a key identifying the loadbalancer by IP, Port and Protocol
func (lb *Loadbalancer) Key() string {
	return GetLoadbalancerKey(lb.Protocol, lb.Input)
}

// GetChainID gets the chain identificator for the specified state
func (lb *Loadbalancer) GetChainID(state ChainState, contentHash uint32) ChainID {
	return NewChainID(lb.Protocol, lb.Input.IP, lb.Input.Port, lb.LastUpdate, state, contentHash)
}

// GetLoadbalancerKey retrieved a mapping key for a loadbalancer with the specified input
func GetLoadbalancerKey(proto Protocol, input Endpoint) string {
	return fmt.Sprintf("%s://%s", proto.String(), input.String())
}
