package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestMainChainCreation(t *testing.T) {
	ctrl, err := NewController(1)
	if err != nil {
		t.Fatalf("Controller couldn't start, see: %v", err)
	}

	expectedBefore := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination`

	actualBefore := iptablesLNVTNAT(t)

	if strings.TrimSpace(expectedBefore) != strings.TrimSpace(actualBefore) {
		t.Fatalf("BEFORE expected `%s` got `%s`", expectedBefore, actualBefore)
	}

	ctrl.sync()

	expectedAfter := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination`

	actualAfter := iptablesLNVTNAT(t)

	if strings.TrimSpace(expectedAfter) != strings.TrimSpace(actualAfter) {
		t.Fatalf("AFTER expected `%s` got `%s`", expectedAfter, actualAfter)
	}
}

func TestLBWithMultipleOutputsAdded(t *testing.T) {
	input, _ := TryParseEndpoint("10.50.1.1:1234")
	output1, _ := TryParseEndpoint("10.100.0.1:1001")
	output2, _ := TryParseEndpoint("10.100.0.2:1002")
	output3, _ := TryParseEndpoint("10.100.0.3:1003")

	ctrl, err := NewController(1)
	if err != nil {
		t.Fatalf("Controller couldn't start, see: %v", err)
	}

	// dont use upsert since it changes the LastUpdate date and we can't compare chain names anymore
	lb := NewLoadbalancer(ProtocolTCP, input, output1, output2, output3)
	lb.LastUpdate = uint32(12345)
	ctrl.loadbalancers[lb.Key()] = *lb

	ctrl.sync()

	expected := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-CgEKMgEBBNIAADA5AfMq03E= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.0.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.0.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 to:10.100.0.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-CgEKMgEBBNIAADA5AfMq03E=  tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234`

	actual := iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("expected `%s` got `%s`", expected, actual)
	}
}

func TestDeleteUnknownLB(t *testing.T) {
	ctrl, err := NewController(1)
	if err != nil {
		t.Fatalf("Controller couldn't start, see: %v", err)
	}

	expectedBefore := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-CgEKMgEBBNIAADA5AfMq03E= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.0.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.0.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 to:10.100.0.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-CgEKMgEBBNIAADA5AfMq03E=  tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234`

	actualBefore := iptablesLNVTNAT(t)

	if strings.TrimSpace(expectedBefore) != strings.TrimSpace(actualBefore) {
		t.Fatalf("BEFORE expected `%s` got `%s`", expectedBefore, actualBefore)
	}

	ctrl, err = NewController(1)
	if err != nil {
		t.Fatalf("Controller couldn't start, see: %v", err)
	}

	ctrl.sync()

	expectedAfter := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination`

	actualAfter := iptablesLNVTNAT(t)

	if strings.TrimSpace(expectedAfter) != strings.TrimSpace(actualAfter) {
		t.Fatalf("AFTER expected `%s` got `%s`", expectedAfter, actualAfter)
	}
}

func TestLBWithSingleOutputsAndExplicitDelete(t *testing.T) {
	input, _ := TryParseEndpoint("10.50.1.1:1234")
	output1, _ := TryParseEndpoint("10.100.0.1:1001")

	ctrl, err := NewController(1)
	if err != nil {
		t.Fatalf("Controller couldn't start, see: %v", err)
	}

	// dont use upsert since it changes the LastUpdate date and we can't compare chain names anymore
	lb := NewLoadbalancer(ProtocolTCP, input, output1)
	lb.LastUpdate = uint32(12345)
	ctrl.loadbalancers[lb.Key()] = *lb

	ctrl.sync()

	expected := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-CgEKMgEBBNIAADA5AeSXG0U= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 to:10.100.0.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-CgEKMgEBBNIAADA5AeSXG0U=  tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234`

	actual := iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("expected `%s` got `%s`", expected, actual)
	}

	ctrl.sync()

	// Expect no change since we didnt do anything
	actual = iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("expected `%s` got `%s`", expected, actual)
	}

	// Remove LB and expect cleanup
	ctrl.DeleteLoadbalancer(lb)
	ctrl.sync()

	expectedAfter := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination`

	actualAfter := iptablesLNVTNAT(t)

	if strings.TrimSpace(expectedAfter) != strings.TrimSpace(actualAfter) {
		t.Fatalf("AFTER expected `%s` got `%s`", expectedAfter, actualAfter)
	}

	// TODO: ensure main chain but no inputs / outputs
}

func TestMultipleLBs(t *testing.T) {
	ctrl, err := NewController(1)
	if err != nil {
		t.Fatalf("Controller couldn't start, see: %v", err)
	}

	input1, _ := TryParseEndpoint("10.50.1.1:1234")
	output11, _ := TryParseEndpoint("10.100.0.1:1001")
	output12, _ := TryParseEndpoint("10.100.0.2:1002")
	output13, _ := TryParseEndpoint("10.100.0.3:1003")

	// dont use upsert since it changes the LastUpdate date and we can't compare chain names anymore
	lb1 := NewLoadbalancer(ProtocolTCP, input1, output11, output12, output13)
	lb1.LastUpdate = uint32(12345)
	ctrl.loadbalancers[lb1.Key()] = *lb1

	input2, _ := TryParseEndpoint("10.50.2.1:1234")
	output21, _ := TryParseEndpoint("10.100.2.1:1001")
	output22, _ := TryParseEndpoint("10.100.2.2:1002")
	output23, _ := TryParseEndpoint("10.100.2.3:1003")

	// dont use upsert since it changes the LastUpdate date and we can't compare chain names anymore
	lb2 := NewLoadbalancer(ProtocolTCP, input2, output21, output22, output23)
	lb2.LastUpdate = uint32(456789)
	ctrl.loadbalancers[lb2.Key()] = *lb2

	ctrl.sync()

	// So, the order of the main chain rules isn't guranteed, so we simply check both possibilites
	expectedA := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-1gEKMgIBBNIABvhVAR4gROc= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.2.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.2.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 to:10.100.2.1:1001

Chain LB$-CgEKMgEBBNIAADA5AfMq03E= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.0.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.0.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 to:10.100.0.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-CgEKMgEBBNIAADA5AfMq03E=  tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234
    0     0 LB$-1gEKMgIBBNIABvhVAR4gROc=  tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234`

	expectedB := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-1gEKMgIBBNIABvhVAR4gROc= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.2.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.2.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 to:10.100.2.1:1001

Chain LB$-CgEKMgEBBNIAADA5AfMq03E= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.0.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.0.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 to:10.100.0.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-1gEKMgIBBNIABvhVAR4gROc=  tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234
    0     0 LB$-CgEKMgEBBNIAADA5AfMq03E=  tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234`

	actual := iptablesLNVTNAT(t)

	if strings.TrimSpace(expectedA) != strings.TrimSpace(actual) && strings.TrimSpace(expectedB) != strings.TrimSpace(actual) {
		t.Fatalf("expected `%s` got `%s`", expectedA, actual)
	}

	// Remove LB and expect cleanup
	ctrl.DeleteLoadbalancer(lb1)
	ctrl.sync()

	expected := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-1gEKMgIBBNIABvhVAR4gROc= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.2.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.2.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234 to:10.100.2.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-1gEKMgIBBNIABvhVAR4gROc=  tcp  --  *      *       0.0.0.0/0            10.50.2.1            tcp dpt:1234`

	actual = iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("AFTER DELETE expected `%s` got `%s`", expected, actual)
	}

	// Remove second and expect cleanup
	ctrl.DeleteLoadbalancer(lb2)
	ctrl.sync()

	expected = `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination`

	actual = iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("AFTER DELETE SCND expected `%s` got `%s`", expected, actual)
	}
}

func TestRemoveSingleEndpointFromLB(t *testing.T) {
	input, _ := TryParseEndpoint("10.50.1.1:1234")
	output1, _ := TryParseEndpoint("10.100.0.1:1001")
	output2, _ := TryParseEndpoint("10.100.0.2:1002")
	output3, _ := TryParseEndpoint("10.100.0.3:1003")

	ctrl, err := NewController(1)
	if err != nil {
		t.Fatalf("Controller couldn't start, see: %v", err)
	}

	// dont use upsert since it changes the LastUpdate date and we can't compare chain names anymore
	lb := NewLoadbalancer(ProtocolTCP, input, output1, output2, output3)
	lb.LastUpdate = uint32(12345)
	ctrl.loadbalancers[lb.Key()] = *lb

	ctrl.sync()

	expected := `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-CgEKMgEBBNIAADA5AfMq03E= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 3 to:10.100.0.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.0.2:1002
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 to:10.100.0.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-CgEKMgEBBNIAADA5AfMq03E=  tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234`

	actual := iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("expected `%s` got `%s`", expected, actual)
	}

	lb = NewLoadbalancer(ProtocolTCP, input, output1, output3)
	lb.LastUpdate = uint32(45678)
	ctrl.loadbalancers[lb.Key()] = *lb

	ctrl.sync()

	expected = `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain LB$-CgEKMgEBBNIAALJuAaZZdWA= (1 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 statistic mode nth every 2 to:10.100.0.3:1003
    0     0 DNAT       tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234 to:10.100.0.1:1001

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination         
    0     0 LB$-CgEKMgEBBNIAALJuAaZZdWA=  tcp  --  *      *       0.0.0.0/0            10.50.1.1            tcp dpt:1234`

	actual = iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("AFTER DELETE expected `%s` got `%s`", expected, actual)
	}

	// Remove second and expect cleanup
	ctrl.DeleteLoadbalancer(lb)
	ctrl.sync()

	expected = `
Chain PREROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain INPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain OUTPUT (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain POSTROUTING (policy ACCEPT 0 packets, 0 bytes)
 pkts bytes target     prot opt in     out     source               destination         

Chain iptableslb-prerouting (0 references)
 pkts bytes target     prot opt in     out     source               destination`

	actual = iptablesLNVTNAT(t)

	if strings.TrimSpace(expected) != strings.TrimSpace(actual) {
		t.Fatalf("AFTER DELETE SCND expected `%s` got `%s`", expected, actual)
	}
}

func iptablesLNVTNAT(t *testing.T) string {
	iptablesClearCounters(t)

	out, err := exec.Command("iptables", "-L", "-nv", "-t", "nat").Output()
	if err != nil {
		t.Fatalf("couldnt dump iptables, see: %v", err)
	}

	return string(out)
}

func iptablesClearCounters(t *testing.T) {
	out, err := exec.Command("iptables", "-t", "nat", "-Z").Output()
	if err != nil {
		t.Fatalf("couldnt dump iptables, see: %v %s", err, out)
	}
}

// TODO tests for the forward chain
