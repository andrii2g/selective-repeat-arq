package sim

import (
	"bytes"
	"fmt"
	"sort"
)

// Verify independently replays observable deliveries and sender acknowledgments.
// It checks trace causality, payload integrity, window bounds and metric totals.
func Verify(c Config, r Result) error {
	if err := c.Validate(); err != nil {
		return err
	}
	w := c.Window
	if r.Protocol == "stop-and-wait" {
		w = 1
	}
	acked := make([]bool, c.Packets+1)
	attempts := make([]int, c.Packets+1)
	base, next := 1, 1
	dataArrivals := map[[2]int64]int{}
	ackArrivals := map[[2]int64]int{}
	trace := append([]Record(nil), r.Trace...)
	// A queued ACK transmission may have been recorded before events preceding its start.
	sort.SliceStable(trace, func(i, j int) bool { return trace[i].At < trace[j].At })
	tx, retries, timeouts, duplicates, discards, peak := 0, 0, 0, 0, 0, 0
	seen := make([]bool, c.Packets+1)
	var payload []byte
	for _, v := range trace {
		if v.ID < 0 || v.ID > c.Packets || v.At < 0 || v.Buffer < 0 || v.Buffer >= w {
			return fmt.Errorf("invalid trace record: %+v", v)
		}
		if v.Buffer > peak {
			peak = v.Buffer
		}
		key := [2]int64{v.At, int64(v.ID)}
		switch v.Kind {
		case "data_tx":
			if v.ID < base || v.ID >= base+w || v.ID == 0 || v.To != v.At+c.DataUS+c.DelayUS {
				return fmt.Errorf("transmission violates sender window or timing")
			}
			attempts[v.ID]++
			if attempts[v.ID] != v.Attempt {
				return fmt.Errorf("attempt mismatch")
			}
			tx++
			if v.Attempt > 1 {
				retries++
			}
			if !v.Lost {
				dataArrivals[[2]int64{v.To, int64(v.ID)}]++
			}
		case "ack_tx":
			if v.To != v.At+c.AckUS+c.DelayUS {
				return fmt.Errorf("ACK timing mismatch")
			}
			if !v.Lost {
				ackArrivals[[2]int64{v.To, int64(v.ID)}]++
			}
		case "data_arrive":
			if dataArrivals[key] == 0 {
				return fmt.Errorf("data arrival without transmission")
			}
			dataArrivals[key]--
			if seen[v.ID] {
				duplicates++
			}
			seen[v.ID] = true
		case "ack_arrive":
			if ackArrivals[key] == 0 {
				return fmt.Errorf("ACK arrival without transmission")
			}
			ackArrivals[key]--
			if v.ID > 0 {
				if r.Protocol == "go-back-n" {
					for id := base; id <= v.ID; id++ {
						acked[id] = true
					}
				} else {
					acked[v.ID] = true
				}
				for base <= c.Packets && acked[base] {
					base++
				}
			}
		case "deliver":
			if v.ID != next || !seen[v.ID] {
				return fmt.Errorf("invalid delivery order")
			}
			for j := 0; j < c.PacketBytes; j++ {
				payload = append(payload, byte((v.ID*31+j*17)%251))
			}
			next++
		case "timeout":
			timeouts++
		case "discard":
			discards++
		}
	}
	m := r.Metrics
	if tx != m.Transmissions || retries != m.Retransmissions || timeouts != m.Timeouts || duplicates != m.Duplicates || discards != m.Discards || next-1 != m.Delivered || peak != m.PeakBuffer {
		return fmt.Errorf("trace/metrics mismatch")
	}
	if !bytes.Equal(payload, r.Delivered) {
		return fmt.Errorf("trace/payload mismatch")
	}
	if r.Complete && (next != c.Packets+1 || base != c.Packets+1 || !bytes.Equal(r.Original, payload)) {
		return fmt.Errorf("invalid completed transfer")
	}
	return nil
}
