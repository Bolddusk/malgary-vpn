// Command egress-probe measures VPN failover (pilot test P-1, quickstart V3) and availability
// (SC-003). Every interval it asks an external echo service for this machine's public IP and
// writes one CSV row: time, ok, egress IP, latency. Run it on a client connected to the VPN.
//
//	go run ./infra/tools/egress-probe -expect 203.0.113.10 -out failover.csv
//
// With -listen it also serves Prometheus metrics for the continuous availability SLO (T112).
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "https://api.ipify.org", "echo service returning the caller's IP")
	expect := flag.String("expect", "", "expected egress IP (the reserved IP)")
	interval := flag.Duration("interval", time.Second, "probe interval")
	out := flag.String("out", "", "CSV output file (default stdout)")
	listen := flag.String("listen", "", "serve Prometheus metrics on this address, e.g. :9587")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		w = f
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "ok", "egress_ip", "expected_ip", "latency_ms"})

	var okTotal, failTotal, wrongIP atomic.Int64
	if *listen != "" {
		go func() {
			http.HandleFunc("/metrics", func(rw http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(rw, "egress_probe_success_total %d\negress_probe_failure_total %d\negress_probe_wrong_ip_total %d\n",
					okTotal.Load(), failTotal.Load(), wrongIP.Load())
			})
			log.Println(http.ListenAndServe(*listen, nil))
		}()
	}

	client := &http.Client{Timeout: 2 * time.Second}
	t := time.NewTicker(*interval)
	defer t.Stop()
	var downSince time.Time
	for {
		start := time.Now()
		ip, err := probe(ctx, client, *url)
		lat := time.Since(start).Milliseconds()
		ok := err == nil && (*expect == "" || ip == *expect)
		switch {
		case err != nil:
			failTotal.Add(1)
		case !ok:
			wrongIP.Add(1)
		default:
			okTotal.Add(1)
		}
		if !ok && downSince.IsZero() {
			downSince = start
		}
		if ok && !downSince.IsZero() {
			log.Printf("recovered after %.1fs (egress %s)", time.Since(downSince).Seconds(), ip)
			downSince = time.Time{}
		}
		_ = cw.Write([]string{start.UTC().Format(time.RFC3339Nano), fmt.Sprint(ok), ip, *expect, fmt.Sprint(lat)})
		cw.Flush()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func probe(ctx context.Context, c *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	return strings.TrimSpace(string(b)), err
}
