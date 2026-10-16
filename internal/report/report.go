// Package report renders simulation results without altering protocol state.
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/andrii2g/selective-repeat-arq/internal/sim"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Write(dir string, c sim.Config, results []sim.Result) error {
	if err := os.MkdirAll(filepath.Join(dir, "traces"), 0755); err != nil {
		return err
	}
	writeJSON := func(name string, v any) error {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0644)
	}
	if e := writeJSON("scenario.json", c); e != nil {
		return e
	}
	summaries := append([]sim.Result(nil), results...)
	for i, r := range results {
		summaries[i].Trace = nil
		if e := writeJSON(filepath.Join("traces", r.Protocol+".json"), r.Trace); e != nil {
			return e
		}
		if e := os.WriteFile(filepath.Join(dir, r.Protocol+".svg"), []byte(SVG([]sim.Result{r})), 0644); e != nil {
			return e
		}
	}
	if e := writeJSON("summary.json", summaries); e != nil {
		return e
	}
	var b strings.Builder
	if e := CSV(&b, results); e != nil {
		return e
	}
	if e := os.WriteFile(filepath.Join(dir, "summary.csv"), []byte(b.String()), 0644); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "comparison.svg"), []byte(SVG(results)), 0644)
}
func CSV(w io.Writer, rs []sim.Result) error {
	c := csv.NewWriter(w)
	if e := c.Write([]string{"protocol", "complete", "reason", "transmissions", "retransmissions", "duplicates", "discards", "delivered", "timeouts", "peak_buffer", "average_buffer", "delivery_us", "completion_us", "goodput_bytes_per_second"}); e != nil {
		return e
	}
	for _, r := range rs {
		m := r.Metrics
		if e := c.Write([]string{r.Protocol, fmt.Sprint(r.Complete), r.Reason, fmt.Sprint(m.Transmissions), fmt.Sprint(m.Retransmissions), fmt.Sprint(m.Duplicates), fmt.Sprint(m.Discards), fmt.Sprint(m.Delivered), fmt.Sprint(m.Timeouts), fmt.Sprint(m.PeakBuffer), fmt.Sprintf("%.6f", m.AverageBuffer), fmt.Sprint(m.DeliveryUS), fmt.Sprint(m.CompletionUS), fmt.Sprintf("%.3f", m.Goodput)}); e != nil {
			return e
		}
	}
	c.Flush()
	return c.Error()
}

// SVG uses a common vertical time scale for every panel. Full labels are in titles.
func SVG(rs []sim.Result) string {
	end := int64(1)
	for _, r := range rs {
		if r.EndUS > end {
			end = r.EndUS
		}
	}
	height := 1200
	width := len(rs)*420 + 80
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img"><title>ARQ protocol comparison</title><rect width="100%%" height="100%%" fill="#f8fafc"/><style>text{font-family:Arial,sans-serif;fill:#183047} .small{font-size:11px} .label{font-size:12px}</style><defs><marker id="arrow" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto-start-reverse"><path d="M0,0 L6,3 L0,6" fill="context-stroke"/></marker></defs>`, width, height, width, height)
	if len(rs) == 1 {
		fmt.Fprint(&b, `<text x="24" y="30" font-size="22" font-weight="bold">Reliable delivery: protocol timeline</text><text x="24" y="53" class="small">Blue: data · purple: retransmission · green dashed: ACK</text><text x="24" y="73" class="small">Red ×: lost · orange: timeout · cyan: buffered · hover for details</text>`)
	} else {
		fmt.Fprint(&b, `<text x="24" y="30" font-size="22" font-weight="bold">Reliable delivery over an unreliable channel</text><text x="24" y="53" class="label">Blue: data · purple: retransmission · green dashed: ACK · red ×: lost · orange: timeout · cyan: buffered</text><text x="24" y="73" class="small">Time runs downward. Panels share one time scale. Hover arrows and markers for exact event details.</text>`)
	}
	y := func(t int64) float64 { return 160 + float64(t)/float64(end)*980 }
	for tick := 0; tick <= 10; tick++ {
		t := end * int64(tick) / 10
		yy := y(t)
		fmt.Fprintf(&b, `<line x1="65" y1="%.1f" x2="%d" y2="%.1f" stroke="#e2e8f0"/><text x="3" y="%.1f" class="small">%.1f ms</text>`, yy, width-10, yy, yy, float64(t)/1000)
	}
	for i, r := range rs {
		s := 100 + i*420
		d := s + 235
		m := r.Metrics
		fmt.Fprintf(&b, `<text x="%d" y="102" font-size="18" font-weight="bold">%s</text><text x="%d" y="122" class="small">%d retransmissions · %d timeouts · peak buffer %d</text><text x="%d" y="145" class="label">Sender</text><text x="%d" y="145" class="label">Receiver</text><path d="M%d 155 V1145 M%d 155 V1145" stroke="#94a3b8" fill="none"/>`, s, html.EscapeString(r.Protocol), s, m.Retransmissions, m.Timeouts, m.PeakBuffer, s-20, d-20, s, d)
		for _, e := range r.Trace {
			if e.At > r.EndUS {
				continue
			}
			yy := y(e.At)
			title := html.EscapeString(fmt.Sprintf("%s packet %d attempt %d at %.3f ms; buffer %d", e.Kind, e.ID, e.Attempt, float64(e.At)/1000, e.Buffer))
			switch e.Kind {
			case "data_tx", "ack_tx":
				x1, x2 := float64(s), float64(d)
				color := "#2563eb"
				dash := ""
				label := fmt.Sprint(e.ID)
				if e.Kind == "ack_tx" {
					x1, x2 = x2, x1
					color = "#15803d"
					dash = `stroke-dasharray="4 3"`
					label = "A" + label
				} else if e.Attempt > 1 {
					color = "#9333ea"
					label = "R" + label
				}
				to := e.To
				if to > r.EndUS {
					to = r.EndUS
				}
				y2 := y(to)
				if e.Lost {
					x2 = (x1 + x2) / 2
					y2 = (yy + y2) / 2
					color = "#dc2626"
				}
				fraction := 0.15 + 0.20*float64((e.ID+e.Attempt+2)%4)
				fmt.Fprintf(&b, `<g><title>%s; arrival %.3f ms; lost=%t</title><line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" %s marker-end="url(#arrow)"/><text x="%.1f" y="%.1f" class="small" fill="%s">%s</text>`, title, float64(e.To)/1000, e.Lost, x1, yy, x2, y2, color, dash, x1+(x2-x1)*fraction, yy+(y2-yy)*fraction-3, color, label)
				if e.Lost {
					fmt.Fprintf(&b, `<path d="M%.1f %.1f l8 8 m0 -8 l-8 8" stroke="#dc2626" stroke-width="2"/>`, x2-4, y2-4)
				}
				fmt.Fprint(&b, `</g>`)
			case "timeout":
				fmt.Fprintf(&b, `<g><title>%s</title><circle cx="%d" cy="%.1f" r="4" fill="#ea580c"/><text x="%d" y="%.1f" class="small">T%d</text></g>`, title, s, yy, s-32, yy-3, e.ID)
			case "buffer", "discard", "deliver":
				color := "#0891b2"
				if e.Kind == "discard" {
					color = "#dc2626"
				}
				if e.Kind == "deliver" {
					color = "#15803d"
				}
				fmt.Fprintf(&b, `<g><title>%s</title><rect x="%d" y="%.1f" width="6" height="4" fill="%s"/></g>`, title, d+4, yy-2, color)
			}
		}
		status := "complete"
		if !r.Complete {
			status = "incomplete: " + r.Reason
		}
		fmt.Fprintf(&b, `<text x="%d" y="1173" class="label">%s · %.1f ms · %.0f bytes/s</text>`, s, html.EscapeString(status), float64(r.EndUS)/1000, m.Goodput)
	}
	fmt.Fprint(&b, `</svg>`)
	return b.String()
}
