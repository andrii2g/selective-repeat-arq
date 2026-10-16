package sim

import (
	"bytes"
	"reflect"
	"testing"
)

func TestStopAndWait(t *testing.T) {
	for _, loss := range [][]Loss{nil, {{"data", 5, 1}}, {{"ack", 5, 1}}} {
		c := Default()
		c.Loss = loss
		r, e := Run(c, "stop-and-wait")
		if e != nil || !r.Complete || !bytes.Equal(r.Original, r.Delivered) {
			t.Fatalf("failed: %+v %v", r.Metrics, e)
		}
		want := 0
		if loss != nil {
			want = 1
		}
		if r.Metrics.Retransmissions != want {
			t.Fatal(r.Metrics)
		}
		again, _ := Run(c, "stop-and-wait")
		if !reflect.DeepEqual(r, again) {
			t.Fatal("nondeterministic")
		}
	}
}
func TestPermanentLoss(t *testing.T) {
	c := Default()
	c.MaxUS = 200000
	c.Loss = []Loss{{"data", 1, 0}}
	r, e := Run(c, "stop-and-wait")
	if e != nil || r.Complete || r.Reason != "time limit" {
		t.Fatal(r.Reason, e)
	}
}
func TestDeadlineACK(t *testing.T) {
	c := Default()
	c.TimeoutUS = c.DataUS + 2*c.DelayUS + c.AckUS
	r, e := Run(c, "stop-and-wait")
	if e != nil || !r.Complete || r.Metrics.Timeouts != 0 {
		t.Fatal(r.Metrics, e)
	}
}
