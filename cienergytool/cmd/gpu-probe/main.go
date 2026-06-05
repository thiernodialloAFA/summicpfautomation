// cienergy-gpu-probe polls nvidia-smi at a fixed interval and, on SIGTERM/SIGINT,
// appends one JSONL line to the cienergy steps file describing the GPU energy
// consumed since startup. Designed to run in background during an ML step.
//
//	cienergy-gpu-probe --name train --steps-file $WORK/steps.jsonl &
//	PROBE_PID=$!
//	./train.py
//	kill -TERM $PROBE_PID; wait
//
// Output line (one per invocation):
//
//	{"name":"train","durationSeconds":7200,"cpuUtilPct":40,
//	 "kWh":1.62,"gpuKWh":1.54,"source":"nvml"}
//
// If nvidia-smi is unavailable, the probe exits cleanly with code 0 and writes
// nothing — the aggregator will then fall back to the eco-ci CPU model.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/axa-oss/cienergytool/internal/probe/nvml"
)

type stepSample struct {
	Name            string  `json:"name"`
	DurationSeconds float64 `json:"durationSeconds"`
	CPUUtilPct      float64 `json:"cpuUtilPct,omitempty"`
	KWh             float64 `json:"kWh,omitempty"`
	GPUKWh          float64 `json:"gpuKWh,omitempty"`
	Source          string  `json:"source,omitempty"`
}

func main() {
	name := flag.String("name", "gpu-job", "step name to write")
	stepsFile := flag.String("steps-file", "", "JSONL file to append to (required)")
	intervalMs := flag.Int("interval-ms", 2000, "sampling interval in milliseconds")
	cpuUtil := flag.Float64("cpu-util", 40, "estimated average CPU utilisation (%) for the wrapping step")
	flag.Parse()

	if *stepsFile == "" {
		log.Fatal("--steps-file is required")
	}

	probe, ok := nvml.New()
	if !ok {
		log.Println("nvidia-smi not found; nothing to do.")
		return
	}
	probe.Interval = time.Duration(*intervalMs) * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() { <-sig; cancel() }()

	start := time.Now()
	gpuKWh, samples := probe.Run(ctx)
	duration := time.Since(start).Seconds()

	if samples < 2 {
		log.Printf("warning: only %d sample(s) captured; gpuKWh = 0", samples)
		gpuKWh = 0
	}

	row := stepSample{
		Name:            *name,
		DurationSeconds: round(duration, 1),
		CPUUtilPct:      *cpuUtil,
		KWh:             round(gpuKWh, 6),
		GPUKWh:          round(gpuKWh, 6),
		Source:          "nvml",
	}

	f, err := os.OpenFile(*stepsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("open steps file: %v", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(row); err != nil {
		log.Fatalf("write step: %v", err)
	}
	fmt.Fprintf(os.Stderr, "cienergy-gpu-probe: step=%q duration=%.1fs gpuKWh=%.6f samples=%d\n",
		row.Name, row.DurationSeconds, row.GPUKWh, samples)
}

func round(v float64, decimals int) float64 {
	p := 1.0
	for i := 0; i < decimals; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}

