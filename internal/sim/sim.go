// Package sim implements deterministic, timeout-driven ARQ simulations.
package sim

import (
	"bytes"
	"container/heap"
	"fmt"
)

// Times are integer microseconds; IDs start at one and never wrap.
type Config struct {
	Packets     int    `json:"packets"`
	PacketBytes int    `json:"packet_bytes"`
	Window      int    `json:"window"`
	DataUS      int64  `json:"data_us"`
	AckUS       int64  `json:"ack_us"`
	DelayUS     int64  `json:"delay_us"`
	TimeoutUS   int64  `json:"timeout_us"`
	MaxUS       int64  `json:"max_us"`
	MaxEvents   int    `json:"max_events"`
	Loss        []Loss `json:"loss"`
}
type Loss struct {
	Direction string `json:"direction"`
	PacketID  int    `json:"packet_id"`
	Attempt   int    `json:"attempt"`
}

func Default() Config {
	return Config{Packets: 20, PacketBytes: 1000, Window: 4, DataUS: 1000, AckUS: 100, DelayUS: 10000, TimeoutUS: 50000, MaxUS: 10000000, MaxEvents: 100000}
}
func (c Config) Validate() error {
	if c.Packets < 1 || c.PacketBytes < 1 || c.Window < 1 || c.DataUS < 1 || c.AckUS < 1 || c.DelayUS < 0 || c.TimeoutUS < 1 || c.MaxUS < 1 || c.MaxEvents < 1 {
		return fmt.Errorf("packet counts, sizes, windows, transmission times and limits must be positive; delay must be nonnegative")
	}
	if c.Packets > 10000 || c.PacketBytes > 1000000 || int64(c.Packets)*int64(c.PacketBytes) > 100000000 || c.MaxUS > 1000000000000 || c.DataUS > c.MaxUS || c.AckUS > c.MaxUS || c.DelayUS > c.MaxUS || c.TimeoutUS > c.MaxUS {
		return fmt.Errorf("configuration exceeds supported resource/time bounds")
	}
	for _, l := range c.Loss {
		if (l.Direction != "data" && l.Direction != "ack") || l.PacketID < 0 || l.PacketID > c.Packets || l.Attempt < 0 || (l.Direction == "data" && l.PacketID == 0) {
			return fmt.Errorf("invalid loss rule: %+v", l)
		}
	}
	return nil
}

type Record struct {
	At      int64  `json:"at_us"`
	Kind    string `json:"kind"`
	ID      int    `json:"id"`
	Attempt int    `json:"attempt,omitempty"`
	To      int64  `json:"to_us,omitempty"`
	Lost    bool   `json:"lost,omitempty"`
	Buffer  int    `json:"buffer"`
}
type Metrics struct {
	Transmissions   int     `json:"transmissions"`
	Retransmissions int     `json:"retransmissions"`
	Duplicates      int     `json:"duplicate_packets"`
	Discards        int     `json:"out_of_order_discards"`
	Delivered       int     `json:"delivered_packets"`
	Timeouts        int     `json:"timeout_events"`
	PeakBuffer      int     `json:"peak_buffer"`
	AverageBuffer   float64 `json:"average_buffer"`
	DeliveryUS      int64   `json:"delivery_us"`
	CompletionUS    int64   `json:"completion_us"`
	Goodput         float64 `json:"goodput_bytes_per_second"`
}
type Result struct {
	Protocol  string   `json:"protocol"`
	Complete  bool     `json:"complete"`
	Reason    string   `json:"reason,omitempty"`
	EndUS     int64    `json:"end_us"`
	Metrics   Metrics  `json:"metrics"`
	Trace     []Record `json:"trace"`
	Original  []byte   `json:"-"`
	Delivered []byte   `json:"-"`
}
type event struct {
	at       int64
	priority int
	order    uint64
	kind     string
	id, gen  int
}
type queue []event

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	a, b := q[i], q[j]
	if a.at != b.at {
		return a.at < b.at
	}
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	return a.order < b.order
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(v any)   { *q = append(*q, v.(event)) }
func (q *queue) Pop() any     { a := *q; v := a[len(a)-1]; *q = a[:len(a)-1]; return v }

type machine struct {
	c                              Config
	r                              Result
	q                              queue
	order                          uint64
	now, dataFree, ackFree         int64
	base, next, expected           int
	attempts, ackAttempts, gens    []int
	acked, seen, buffered, pending []bool
	buffer                         int
	area                           int64
	payload                        [][]byte
}

func Run(c Config, protocol string) (Result, error) {
	if err := c.Validate(); err != nil {
		return Result{}, err
	}
	if protocol != "stop-and-wait" && protocol != "go-back-n" && protocol != "selective-repeat" {
		return Result{}, fmt.Errorf("unknown protocol %q", protocol)
	}
	m := &machine{c: c, r: Result{Protocol: protocol}, base: 1, next: 1, expected: 1}
	n := c.Packets + 1
	m.attempts = make([]int, n)
	m.ackAttempts = make([]int, n)
	m.gens = make([]int, n)
	m.acked = make([]bool, n)
	m.seen = make([]bool, n)
	m.buffered = make([]bool, n)
	m.pending = make([]bool, n)
	m.payload = make([][]byte, n)
	for id := 1; id < n; id++ {
		p := make([]byte, c.PacketBytes)
		for j := range p {
			p[j] = byte((id*31 + j*17) % 251)
		}
		m.payload[id] = p
		m.r.Original = append(m.r.Original, p...)
	}
	m.fill()
	processed := 0
	for m.q.Len() > 0 && !m.r.Complete {
		if processed >= c.MaxEvents {
			m.r.Reason = "event limit"
			break
		}
		e := heap.Pop(&m.q).(event)
		if e.at > c.MaxUS {
			m.advance(c.MaxUS)
			m.r.Reason = "time limit"
			break
		}
		m.advance(e.at)
		processed++
		switch e.kind {
		case "send":
			m.send(e.id)
		case "data":
			m.receive(e.id)
		case "ack":
			m.ack(e.id)
		case "timeout":
			m.timeout(e.id, e.gen)
		}
	}
	m.r.EndUS = m.now
	if !m.r.Complete && m.r.Reason == "" {
		m.r.Reason = "no pending events"
	}
	if m.now > 0 {
		m.r.Metrics.AverageBuffer = float64(m.area) / float64(m.now)
	}
	if m.r.Complete && !bytes.Equal(m.r.Original, m.r.Delivered) {
		return m.r, fmt.Errorf("payload reconstruction failed")
	}
	return m.r, nil
}
func (m *machine) advance(t int64) { m.area += int64(m.buffer) * (t - m.now); m.now = t }
func (m *machine) add(at int64, kind string, id, gen int) {
	p := 0
	if kind == "send" {
		p = 1
	}
	if kind == "timeout" {
		p = 2
	}
	m.order++
	heap.Push(&m.q, event{at, p, m.order, kind, id, gen})
}
func (m *machine) record(kind string, id int) {
	m.r.Trace = append(m.r.Trace, Record{At: m.now, Kind: kind, ID: id, Buffer: m.buffer})
}
func (m *machine) lost(direction string, id, attempt int) bool {
	for _, l := range m.c.Loss {
		if l.Direction == direction && l.PacketID == id && (l.Attempt == 0 || l.Attempt == attempt) {
			return true
		}
	}
	return false
}
func (m *machine) fill() {
	w := m.c.Window
	if m.r.Protocol == "stop-and-wait" {
		w = 1
	}
	for m.next <= m.c.Packets && m.next < m.base+w {
		m.schedule(m.next)
		m.next++
	}
}
func (m *machine) schedule(id int) {
	if m.pending[id] || m.acked[id] {
		return
	}
	m.pending[id] = true
	m.add(m.now, "send", id, 0)
}
func (m *machine) send(id int) {
	if m.acked[id] {
		m.pending[id] = false
		return
	}
	if m.now < m.dataFree {
		m.add(m.dataFree, "send", id, 0)
		return
	}
	m.pending[id] = false
	m.attempts[id]++
	m.r.Metrics.Transmissions++
	if m.attempts[id] > 1 {
		m.r.Metrics.Retransmissions++
	}
	m.dataFree = m.now + m.c.DataUS
	arrival := m.dataFree + m.c.DelayUS
	lost := m.lost("data", id, m.attempts[id])
	m.r.Trace = append(m.r.Trace, Record{At: m.now, Kind: "data_tx", ID: id, Attempt: m.attempts[id], To: arrival, Lost: lost, Buffer: m.buffer})
	if !lost {
		m.add(arrival, "data", id, 0)
	}
	if m.r.Protocol != "go-back-n" || id == m.base {
		m.arm(id)
	}
}
func (m *machine) arm(id int) { m.gens[id]++; m.add(m.now+m.c.TimeoutUS, "timeout", id, m.gens[id]) }
func (m *machine) sendACK(id int) {
	start := m.now
	if start < m.ackFree {
		start = m.ackFree
	}
	m.ackFree = start + m.c.AckUS
	arrival := m.ackFree + m.c.DelayUS
	m.ackAttempts[id]++
	lost := m.lost("ack", id, m.ackAttempts[id])
	m.r.Trace = append(m.r.Trace, Record{At: start, Kind: "ack_tx", ID: id, Attempt: m.ackAttempts[id], To: arrival, Lost: lost, Buffer: m.buffer})
	if !lost {
		m.add(arrival, "ack", id, 0)
	}
}
func (m *machine) deliver(id int) {
	m.r.Delivered = append(m.r.Delivered, m.payload[id]...)
	m.r.Metrics.Delivered++
	m.expected++
	m.record("deliver", id)
	if m.r.Metrics.Delivered == m.c.Packets {
		m.r.Metrics.DeliveryUS = m.now
		m.r.Metrics.Goodput = float64(len(m.r.Delivered)) * 1000000 / float64(m.now)
	}
}
func (m *machine) receive(id int) {
	m.record("data_arrive", id)
	if m.seen[id] {
		m.r.Metrics.Duplicates++
		m.record("duplicate", id)
	}
	m.seen[id] = true
	if m.r.Protocol == "selective-repeat" {
		if id < m.expected {
			m.sendACK(id)
			return
		}
		if id >= m.expected+m.c.Window {
			m.r.Metrics.Discards++
			m.record("discard", id)
			return
		}
		if id == m.expected {
			m.deliver(id)
			for m.expected <= m.c.Packets && m.buffered[m.expected] {
				next := m.expected
				m.buffered[next] = false
				m.buffer--
				m.deliver(next)
			}
		} else if !m.buffered[id] {
			m.buffered[id] = true
			m.buffer++
			if m.buffer > m.r.Metrics.PeakBuffer {
				m.r.Metrics.PeakBuffer = m.buffer
			}
			m.record("buffer", id)
		}
		m.sendACK(id)
		return
	}
	if id == m.expected {
		m.deliver(id)
	} else if id > m.expected {
		m.r.Metrics.Discards++
		m.record("discard", id)
	}
	m.sendACK(m.expected - 1)
}
func (m *machine) ack(id int) {
	m.record("ack_arrive", id)
	if id < m.base || id >= m.next {
		return
	}
	oldBase := m.base
	first := id
	if m.r.Protocol == "go-back-n" {
		first = m.base
	}
	for i := first; i <= id; i++ {
		m.acked[i] = true
		m.gens[i]++
	}
	for m.base < m.next && m.acked[m.base] {
		m.base++
	}
	if m.base > m.c.Packets {
		m.r.Complete = true
		m.r.Metrics.CompletionUS = m.now
		return
	}
	if m.r.Protocol == "go-back-n" && m.base != oldBase && m.base < m.next && m.attempts[m.base] > 0 {
		m.arm(m.base)
	}
	m.fill()
}
func (m *machine) timeout(id, gen int) {
	if m.acked[id] || m.gens[id] != gen {
		return
	}
	if m.r.Protocol == "go-back-n" && id != m.base {
		return
	}
	m.r.Metrics.Timeouts++
	m.record("timeout", id)
	if m.r.Protocol == "go-back-n" {
		for i := m.base; i < m.next; i++ {
			m.schedule(i)
		}
	} else {
		m.schedule(id)
	}
}
