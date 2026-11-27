package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestHappenings(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.ClaimAdmin(ctx, "g", "a")
	now := time.Unix(1_000_000, 0)
	a, err := s.StartHappening(ctx, "g", "manticore", now, now.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.StartHappening(ctx, "g", "troll", now, now.Add(3*time.Hour), nil)
	if act, _ := s.ActiveHappenings(ctx, "g"); len(act) != 2 || act[0].Key != "manticore" {
		t.Fatalf("active: %+v", act)
	}
	if due, _ := s.DueHappenings(ctx, now.Add(2*time.Hour)); len(due) != 1 || due[0].ID != a.ID {
		t.Fatalf("due: %+v", due)
	}
	if err := s.EndHappening(ctx, b.ID, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := s.EndHappening(ctx, b.ID, 0, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("second end: %v", err)
	}
	if last, _ := s.LastHappening(ctx, "g"); last != "troll" {
		t.Fatalf("last: %q", last)
	}
	if act, _ := s.ActiveHappenings(ctx, "g"); len(act) != 1 {
		t.Fatalf("after end: %+v", act)
	}
}
