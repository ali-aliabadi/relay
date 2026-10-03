package id

import (
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func TestNew(t *testing.T) {
	t0 := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	a := New(Message, t0)
	b := New(Message, t0.Add(time.Millisecond))
	if !strings.HasPrefix(a, "msg_") || len(a) != 4+26 {
		t.Fatalf("id = %q", a)
	}
	if a >= b {
		t.Errorf("ids not ordered by time: %q >= %q", a, b)
	}
	u, err := ulid.Parse(strings.TrimPrefix(a, "msg_"))
	if err != nil || !ulid.Time(u.Time()).Equal(t0) {
		t.Errorf("ulid time = %v, %v", ulid.Time(u.Time()), err)
	}
	if New(Message, t0) == a {
		t.Error("same-millisecond ids collide")
	}
}

func TestSameMillisecondOrder(t *testing.T) {
	t0 := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	prev := New(Message, t0)
	for range 1000 {
		next := New(Message, t0)
		if next <= prev {
			t.Fatalf("ids in one millisecond out of order: %s then %s", prev, next)
		}
		prev = next
	}
}
