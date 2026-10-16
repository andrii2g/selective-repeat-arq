package sim

import (
	"reflect"
	"testing"
)

func FuzzProtocols(f *testing.F) {
	f.Add(uint8(20), uint8(4), uint16(50000), uint8(5), true)
	f.Add(uint8(3), uint8(1), uint16(1), uint8(2), false)
	f.Fuzz(func(t *testing.T, n, w uint8, timeout uint16, id uint8, ack bool) {
		c := Default()
		c.Packets = 1 + int(n%20)
		c.PacketBytes = 16
		c.Window = 1 + int(w%8)
		c.TimeoutUS = 1 + int64(timeout)
		c.MaxUS = 2000000
		c.MaxEvents = 5000
		direction := "data"
		if ack {
			direction = "ack"
		}
		c.Loss = []Loss{{direction, 1 + int(id)%c.Packets, 1}}
		for _, p := range Protocols {
			r, e := Run(c, p)
			if e != nil {
				t.Fatal(e)
			}
			if e = Verify(c, r); e != nil {
				t.Fatal(p, e)
			}
			again, e := Run(c, p)
			if e != nil || !reflect.DeepEqual(r, again) {
				t.Fatal("replay mismatch", e)
			}
		}
	})
}
func TestVerifierRejectsCorruption(t *testing.T) {
	c := Default()
	r, _ := Run(c, "selective-repeat")
	if e := Verify(c, r); e != nil {
		t.Fatal(e)
	}
	r.Delivered[0] ^= 1
	if Verify(c, r) == nil {
		t.Fatal("accepted corrupted output")
	}
}
func TestBoundsAndShortTimers(t *testing.T) {
	for _, p := range Protocols {
		for _, timeout := range []int64{1, 1000, 21099, 21100, 21101, 50000} {
			c := Default()
			c.Packets = 8
			c.TimeoutUS = timeout
			c.MaxEvents = 20000
			c.Loss = []Loss{{"data", 2, 1}, {"ack", 4, 1}}
			r, e := Run(c, p)
			if e != nil {
				t.Fatal(e)
			}
			if e = Verify(c, r); e != nil {
				t.Fatal(p, timeout, e)
			}
		}
	}
	c := Default()
	c.MaxEvents = 1
	r, _ := Run(c, "go-back-n")
	if r.Complete || r.Reason != "event limit" {
		t.Fatal("event limit not enforced")
	}
}
