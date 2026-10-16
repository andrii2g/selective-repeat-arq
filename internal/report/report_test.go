package report

import (
	"encoding/csv"
	"encoding/xml"
	"github.com/andrii2g/selective-repeat-arq/internal/sim"
	"io"
	"strings"
	"testing"
)

func TestReports(t *testing.T) {
	c := sim.Default()
	c.Loss = []sim.Loss{{Direction: "data", PacketID: 5, Attempt: 1}}
	var rs []sim.Result
	for _, p := range []string{"stop-and-wait", "go-back-n", "selective-repeat"} {
		r, e := sim.Run(c, p)
		if e != nil {
			t.Fatal(e)
		}
		rs = append(rs, r)
	}
	svg := SVG(rs)
	d := xml.NewDecoder(strings.NewReader(svg))
	for {
		_, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"go-back-n", "selective-repeat", "lost=true", "buffer packet", "timeout packet"} {
		if !strings.Contains(svg, s) {
			t.Fatal("missing", s)
		}
	}
	var b strings.Builder
	if e := CSV(&b, rs); e != nil {
		t.Fatal(e)
	}
	rows, e := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if e != nil || len(rows) != 4 || len(rows[0]) != 14 {
		t.Fatal(rows, e)
	}
	if e := Write(t.TempDir(), c, rs); e != nil {
		t.Fatal(e)
	}
}
