package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/andrii2g/selective-repeat-arq/internal/report"
	"github.com/andrii2g/selective-repeat-arq/internal/sim"
	"io"
	"os"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: arqsim compare|run|verify [flags]")
	}
	if args[0] == "verify" {
		return verify()
	}
	if args[0] != "compare" && args[0] != "run" {
		return fmt.Errorf("unknown command %q", args[0])
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	scenario := f.String("scenario", "", "JSON scenario (omitted fields use defaults)")
	out := f.String("out", "out/comparison", "output directory")
	p := f.String("protocol", "selective-repeat", "protocol for run")
	window := f.Int("window", 0, "override window")
	timeout := f.Int64("timeout-us", 0, "override timeout in microseconds")
	if e := f.Parse(args[1:]); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	c := sim.Default()
	if *scenario != "" {
		file, e := os.Open(*scenario)
		if e != nil {
			return e
		}
		defer file.Close()
		d := json.NewDecoder(file)
		d.DisallowUnknownFields()
		config := &c
		if e = d.Decode(&config); e != nil {
			return e
		}
		if config == nil {
			return fmt.Errorf("scenario must be a JSON object")
		}
		var extra any
		if e = d.Decode(&extra); e != io.EOF {
			return fmt.Errorf("scenario must contain exactly one JSON object")
		}
	}
	f.Visit(func(v *flag.Flag) {
		switch v.Name {
		case "window":
			c.Window = *window
		case "timeout-us":
			c.TimeoutUS = *timeout
		}
	})
	protocols := []string{"stop-and-wait", "go-back-n", "selective-repeat"}
	if args[0] == "run" {
		protocols = []string{*p}
	}
	var results []sim.Result
	complete := true
	fmt.Printf("%-18s %8s %8s %10s %12s\n", "protocol", "retries", "buffer", "time(ms)", "bytes/sec")
	for _, protocol := range protocols {
		r, e := sim.Run(c, protocol)
		if e != nil {
			return e
		}
		if e = sim.Verify(c, r); e != nil {
			return e
		}
		results = append(results, r)
		complete = complete && r.Complete
		fmt.Printf("%-18s %8d %8d %10.1f %12.1f\n", protocol, r.Metrics.Retransmissions, r.Metrics.PeakBuffer, float64(r.EndUS)/1000, r.Metrics.Goodput)
	}
	if e := report.Write(*out, c, results); e != nil {
		return e
	}
	fmt.Println("Reports:", *out)
	if !complete {
		return fmt.Errorf("one or more transfers incomplete; see summary.json")
	}
	return nil
}
func verify() error {
	count := 0
	for _, p := range []string{"stop-and-wait", "go-back-n", "selective-repeat"} {
		for w := 1; w <= 5; w++ {
			for mask := 0; mask < 64; mask++ {
				c := sim.Default()
				c.Packets = 6
				c.Window = w
				for id := 1; id <= 6; id++ {
					if mask&(1<<(id-1)) != 0 {
						c.Loss = append(c.Loss, sim.Loss{Direction: "data", PacketID: id, Attempt: 1}, sim.Loss{Direction: "ack", PacketID: id, Attempt: 1})
					}
				}
				r, e := sim.Run(c, p)
				if e != nil {
					return e
				}
				if e = sim.Verify(c, r); e != nil {
					return e
				}
				if !r.Complete {
					return fmt.Errorf("verification incomplete: %s window %d mask %d", p, w, mask)
				}
				next := 1
				for _, v := range r.Trace {
					if v.Kind == "deliver" {
						if v.ID != next {
							return fmt.Errorf("delivery order violation")
						}
						next++
					}
					if v.Buffer < 0 || v.Buffer >= w {
						return fmt.Errorf("buffer bound violation")
					}
				}
				if next != 7 {
					return fmt.Errorf("delivery count violation")
				}
				count++
			}
		}
	}
	fmt.Printf("Verified %d deterministic protocol/window/loss combinations\n", count)
	return nil
}
