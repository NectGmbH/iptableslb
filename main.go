package main

import (
	"flag"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
)

type sliceFlags []string

func (i *sliceFlags) String() string {
	return strings.Join(*i, " ")
}

func (i *sliceFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// LBHealthCheckStatus contains the status update of one output for a specific loadbalancer
type LBHealthCheckStatus struct {
	HealthCheckStatus
	LBKey string
}

func mergeHealthFeeds(cs ...chan LBHealthCheckStatus) chan LBHealthCheckStatus {
	out := make(chan LBHealthCheckStatus)

	var wg sync.WaitGroup
	wg.Add(len(cs))

	for _, c := range cs {
		go func(c <-chan LBHealthCheckStatus) {
			for v := range c {
				out <- v
			}
			wg.Done()
		}(c)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func setupHealthChecks(prot Protocol, in Endpoint, outs []Endpoint, healthProvider HealthCheckProvider) (chan struct{}, chan LBHealthCheckStatus) {
	stopChan := make(chan struct{}, 0)
	stopChans := make([]chan struct{}, 0)
	healthFeed := make(chan LBHealthCheckStatus)
	lbKey := GetLoadbalancerKey(prot, in)

	for _, endpoint := range outs {
		h := NewHealthCheck(
			endpoint.IP,
			int(endpoint.Port),
			healthProvider,
			60*time.Second,
			1*time.Second)

		stopChanOuter := make(chan struct{}, 0)
		stopChanInner := make(chan struct{}, 0)
		notificationChan := h.Monitor(stopChanInner)

		// Aggregate all health updates onto one channel
		go (func() {
			for {
				select {
				case <-stopChanOuter:
					stopChanInner <- struct{}{}
					close(stopChanInner)
					return
				case status := <-notificationChan:
					healthFeed <- LBHealthCheckStatus{
						HealthCheckStatus: status,
						LBKey:             lbKey,
					}
				}
			}
		})()

		stopChans = append(stopChans, stopChanOuter)
	}

	go (func() {
		<-stopChan
		for _, s := range stopChans {
			s <- struct{}{}
			close(s)
		}
	})()

	return stopChan, healthFeed
}

func main() {
	var inFlags sliceFlags
	var outFlags sliceFlags
	var healthFlags sliceFlags

	flag.Var(&inFlags, "in", "Input for the lb, e.g. \"tcp://192.168.0.1:80\"")
	flag.Var(&outFlags, "out", "Outputs for the lb defined in the lasdt in parameter, e.g. \"192.168.2.1:8080,192.168.2.2-255:8080\"")
	flag.Var(&healthFlags, "h", "HealthCheck which should be used, available: http, tcp, none")

	flag.Parse()

	if len(inFlags) != len(outFlags) || len(inFlags) != len(healthFlags) {
		glog.Fatalf("For every -in parameter you have to specify exactly ONE -h and ONE -out parameter")
	}

	if len(inFlags) == 0 {
		glog.Fatalf("didn't specify any loadbalancers")
	}

	ctrl, err := NewController()
	if err != nil {
		glog.Fatalf("Controller couldn't start, see: %v", err)
	}

	stopChs := make([]chan struct{}, 0)
	statusChs := make([]chan LBHealthCheckStatus, 0)
	loadbalancers := make(map[string]*Loadbalancer)

	for i := 0; i < len(inFlags); i++ {
		in := inFlags[i]
		out := outFlags[i]
		health := healthFlags[i]

		prot, inEndpoint, err := TryParseProtocolEndpoint(in)
		if err != nil {
			glog.Fatalf("couldn't parse input endpoint from `%s`, see: %v", in, err)
		}

		outEndpoints, err := TryParseEndpoints(out)
		if err != nil {
			glog.Fatalf("couldn't parse endpoints from `%s`, see: %v", out, err)
		}

		var healthProvider HealthCheckProvider
		if health == "none" {
			healthProvider = DefaultNoneHealthCheckProvider
		} else if health == "tcp" {
			healthProvider = DefaultTCPHealthCheckProvider
		} else if health == "http" {
			healthProvider = DefaultHTTPHealthCheckProvider
		} else {
			glog.Fatalf("unknown health check protocol `%s` use either \"none\", \"tcp\" or \"http\".", health)
		}

		lb := NewLoadbalancer(prot, inEndpoint, outEndpoints...)
		loadbalancers[lb.Key()] = lb
		stopCh, statusCh := setupHealthChecks(prot, inEndpoint, outEndpoints, healthProvider)
		stopChs = append(stopChs, stopCh)
		statusChs = append(statusChs, statusCh)
	}

	statusUpdated := mergeHealthFeeds(statusChs...)

	go (func() {
		for status := range statusUpdated {
			lb, found := loadbalancers[status.LBKey]
			if !found {
				glog.Warningf("Got status update `%#v` for not configured loadbalancer `%s`", status, status.LBKey)
				continue
			}

			if status.DidChange {
				glog.Info(status.String())

				endpoint := status.GetEndpoint()

				if status.Healthy {
					lb.Outputs = EndpointsAppendUnique(lb.Outputs, endpoint)
				} else {
					lb.Outputs = EndpointsRemove(lb.Outputs, endpoint)
				}

				ctrl.UpsertLoadbalancer(lb)
			} else {
				glog.V(5).Info(status.String())
			}
		}
	})()

	// Let's wait a sec so we get up to date health informations before we start the controller
	time.Sleep(2 * time.Second)

	ctrl.Run()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)
	for range signalCh {
		glog.Infof("Received ^C, shutting down...")
		ctrl.Stop()

		for _, stopCh := range stopChs {
			close(stopCh)
		}

		break
	}

	glog.Infof("Stopped.")
}
