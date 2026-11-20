package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestClockNow(t *testing.T) {
	r := ClockNow{}.Exec(context.Background(), "")
	if r.Err != nil || len(r.Output) < 20 || !strings.Contains(r.Output, "T") {
		t.Fatalf("clock.now: %+v", r)
	}
	if _, err := time.Parse(time.RFC3339, r.Output); err != nil {
		t.Fatalf("clock.now format: %v", err)
	}
}

func TestEcho(t *testing.T) {
	r := Echo{}.Exec(context.Background(), `{"text":"hello"}`)
	if r.Err != nil || r.Output != "hello" {
		t.Fatalf("echo: %+v", r)
	}
	if r := (Echo{}.Exec(context.Background(), `{`)); r.Err == nil {
		t.Fatal("invalid JSON must error")
	}
	if r := (Echo{}.Exec(context.Background(), `{}`)); r.Err == nil {
		t.Fatal("missing text must error")
	}
	long := strings.Repeat("x", 5000)
	r = Echo{}.Exec(context.Background(), `{"text":"`+long+`"}`)
	if r.Err != nil || len(r.Output) != 4096 {
		t.Fatalf("bounded echo: err=%v len=%d", r.Err, len(r.Output))
	}
}

func TestProviderExecuteUnknown(t *testing.T) {
	p := BuiltinProvider{}
	if len(p.Tools()) != 2 {
		t.Fatalf("builtins: %+v", p.Tools())
	}
	r := Execute(context.Background(), p, "nope", "{}")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "unknown tool") {
		t.Fatalf("unknown: %+v", r)
	}
	if d := Lookup(p, "clock.now"); d == nil || d.Name != "clock.now" {
		t.Fatalf("lookup: %+v", d)
	}
	if d := Lookup(p, "nope"); d != nil {
		t.Fatalf("lookup miss: %+v", d)
	}
}
