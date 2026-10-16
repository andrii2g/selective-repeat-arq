package sim

import (
	"bytes"
	"testing"
)

var Protocols = []string{"stop-and-wait", "go-back-n", "selective-repeat"}

func TestLossFive(t *testing.T) {
	for _, p := range Protocols {
		c := Default()
		c.Loss = []Loss{{"data", 5, 1}}
		r, e := Run(c, p)
		if e != nil || !r.Complete {
			t.Fatal(p, e)
		}
		want := 1
		if p == "go-back-n" {
			want = 4
		}
		if r.Metrics.Retransmissions != want {
			t.Fatalf("%s: %+v", p, r.Metrics)
		}
		if p == "selective-repeat" && r.Metrics.PeakBuffer != 3 {
			t.Fatal(r.Metrics)
		}
		t.Log(p, r.Metrics)
	}
}
func TestLossMatrix(t *testing.T) {
	for _, p := range Protocols {
		for w := 1; w <= 5; w++ {
			for id := 1; id <= 6; id++ {
				for _, direction := range []string{"data", "ack"} {
					c := Default()
					c.Packets = 6
					c.Window = w
					c.Loss = []Loss{{direction, id, 1}, {direction, id, 2}}
					r, e := Run(c, p)
					if e != nil || !r.Complete || !bytes.Equal(r.Original, r.Delivered) || r.Metrics.PeakBuffer >= w {
						t.Fatalf("%s w%d id%d %s: %+v %v", p, w, id, direction, r.Metrics, e)
					}
					next := 1
					for _, v := range r.Trace {
						if v.Kind == "deliver" {
							if v.ID != next {
								t.Fatal("delivery order")
							}
							next++
						}
					}
				}
			}
		}
	}
}
func TestCumulativeACK(t *testing.T) {
	c := Default()
	c.Loss = []Loss{{"ack", 1, 0}}
	r, e := Run(c, "go-back-n")
	if e != nil || !r.Complete || r.Metrics.Retransmissions != 0 {
		t.Fatal(r.Metrics, e)
	}
}
