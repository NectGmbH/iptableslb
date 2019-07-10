package main

import (
    "fmt"
    "math/rand"
    "net"
    "net/http"
    "time"

    "github.com/golang/glog"
)

const defaultRetention = time.Second

var DefaultNoneHealthCheckProvider = &NoneHealthCheckProvider{}
var DefaultTCPHealthCheckProvider = &TCPHealthCheckProvider{}
var DefaultHTTPHealthCheckProvider = &HTTPHealthCheckProvider{}

type HealthCheck struct {
    IP              net.IP
    Port            int
    Provider        HealthCheckProvider
    Healthy         bool
    LastTimeHealthy time.Time
    LastCheck       time.Time
    LastMessage     string
    Retention       time.Duration
    MaxRetention    time.Duration
    MaxResponseTime time.Duration
}

type HealthCheckStatus struct {
    IP        net.IP
    Port      int
    Healthy   bool
    Message   string
    DidChange bool
}

func (h *HealthCheckStatus) GetEndpoint() Endpoint {
    return Endpoint{IP: h.IP, Port: uint16(h.Port)}
}

func (s *HealthCheckStatus) String() string {
    sign := "UP"

    if !s.Healthy {
        sign = "DOWN"
    }

    return fmt.Sprintf("%s %s:%d - %s", sign, s.IP, s.Port, s.Message)
}

func NewHealthCheck(
    ip net.IP,
    port int,
    provider HealthCheckProvider,
    maxRetention time.Duration,
    maxResponseTime time.Duration,
) *HealthCheck {
    h := &HealthCheck{
        IP:              ip,
        Port:            port,
        Provider:        provider,
        Healthy:         false,
        Retention:       defaultRetention,
        MaxRetention:    maxRetention,
        MaxResponseTime: maxResponseTime,
    }

    return h
}

func (h *HealthCheck) GetAddress() string {
    return fmt.Sprintf("%s:%d", h.IP, h.Port)
}

func (h *HealthCheck) Monitor(stopChan chan struct{}) chan HealthCheckStatus {
    notificationChan := make(chan HealthCheckStatus)

    go (func() {
        glog.V(5).Infof("Starting monitoring %s:%d", h.IP, h.Port)

        for {
            select {
            case <-stopChan:
                glog.V(5).Infof("Stopped monitoring %s:%d", h.IP, h.Port)
                close(notificationChan)
                return
            default:
            }

            isFirst := h.LastCheck.IsZero()
            before := h.Healthy
            h.CheckHealth()
            after := h.Healthy

            notificationChan <- HealthCheckStatus{
                IP:        h.IP,
                Port:      h.Port,
                Healthy:   h.Healthy,
                Message:   h.LastMessage,
                DidChange: isFirst || after != before,
            }

            time.Sleep(h.Retention)
        }
    })()

    return notificationChan
}

func (h *HealthCheck) CheckHealth() {
    h.LastMessage, h.Healthy = h.Provider.CheckHealth(h)

    // Add some randomness so not all checks get executed at the same time
    retention := defaultRetention + time.Duration((rand.Float64()/2)*float64(time.Second))

    h.LastCheck = time.Now()
    if h.Healthy {
        h.LastTimeHealthy = h.LastCheck
        h.Retention = retention
    } else if h.Retention < h.MaxRetention {
        h.Retention += retention
    }
}

type HealthCheckProvider interface {
    CheckHealth(healthCheck *HealthCheck) (string, bool)
}

type NoneHealthCheckProvider struct {
}

func (c *NoneHealthCheckProvider) CheckHealth(h *HealthCheck) (string, bool) {
    return "unknown", true
}

type TCPHealthCheckProvider struct {
}

func (c *TCPHealthCheckProvider) CheckHealth(h *HealthCheck) (string, bool) {
    con, err := net.DialTimeout("tcp", h.GetAddress(), h.MaxResponseTime)
    if err != nil {
        return err.Error(), false
    }

    defer con.Close()

    return "success", true
}

type HTTPHealthCheckProvider struct {
}

func (c *HTTPHealthCheckProvider) CheckHealth(h *HealthCheck) (string, bool) {
    client := &http.Client{
        Timeout: h.MaxResponseTime,
    }

    resp, err := client.Get("http://" + h.GetAddress() + "/healthz")
    if err != nil {
        return err.Error(), false
    }

    defer resp.Body.Close()

    if resp.StatusCode < 200 || resp.StatusCode > 299 {
        return fmt.Sprintf("status code is `%d`", resp.StatusCode), false
    }

    return "success", true
}
