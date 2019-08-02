package main

import (
    "fmt"
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics contains all logic for prometheus metrics
type Metrics struct {
    ErrorsTotal        prometheus.Counter
    LBTotal            prometheus.Counter
    LBHealthy          prometheus.Gauge
    LBHealthyEndpoints *prometheus.GaugeVec
}

// Init initializes the metrics
func (m *Metrics) Init() error {
    // -- ErrorsTotal ----------------------------------------------------------
    m.ErrorsTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Subsystem: "general",
            Name:      "errors_total",
            Help:      "Total number of errors happened.",
        })

    err := prometheus.Register(m.ErrorsTotal)
    if err != nil {
        return fmt.Errorf("couldn't register ErrorsTotal counter, see: %v", err)
    }

    // -- LBTotal --------------------------------------------------------------
    m.LBTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Subsystem: "general",
            Name:      "lb_total",
            Help:      "Amount of total configured loadbalancers",
        })

    err = prometheus.Register(m.LBTotal)
    if err != nil {
        return fmt.Errorf("couldn't register LBTotal counter, see: %v", err)
    }

    // -- LBHealthy ------------------------------------------------------------
    m.LBHealthy = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Subsystem: "general",
            Name:      "lb_healthy",
            Help:      "Amount of healthy loadbalancers",
        })

    err = prometheus.Register(m.LBHealthy)
    if err != nil {
        return fmt.Errorf("couldn't register LBHealthy counter, see: %v", err)
    }

    // -- LBHealthyEndpoints ---------------------------------------------------
    m.LBHealthyEndpoints = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Subsystem: "general",
            Name:      "lb_healthy_endpoints",
            Help:      "Loadbalancers with amount of healthy endpoints",
        },
        []string{"lb"})

    err = prometheus.Register(m.LBHealthyEndpoints)
    if err != nil {
        return fmt.Errorf("couldn't register LBHealthyEndpoints gauge, see: %v", err)
    }

    // -------------------------------------------------------------------------

    http.Handle("/metrics", promhttp.Handler())

    return nil
}
