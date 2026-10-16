package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLI(t *testing.T) {
	dir := t.TempDir()
	if e := run([]string{"compare", "--out", dir}); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"summary.json", "summary.csv", "comparison.svg", "scenario.json", "traces/selective-repeat.json"} {
		if _, e := os.Stat(filepath.Join(dir, name)); e != nil {
			t.Fatal(e)
		}
	}
	for _, args := range [][]string{nil, {"unknown"}, {"run", "--window", "0"}, {"run", "--protocol", "bad"}, {"run", "extra"}} {
		if run(args) == nil {
			t.Fatal("accepted", args)
		}
	}
}
func TestInvalidScenario(t *testing.T) {
	for _, body := range []string{`{"unexpected":1}`, `{} {}`, `{"packets":0}`, `null`} {
		p := filepath.Join(t.TempDir(), "bad.json")
		if e := os.WriteFile(p, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
		if run([]string{"compare", "--scenario", p}) == nil {
			t.Fatal("accepted", body)
		}
	}
}
