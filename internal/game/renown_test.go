package game

import "testing"

func TestRenownIsWhole(t *testing.T) {
	for in, want := range map[float64]float64{0.1: 1, -0.3: -1, 2.4: 2, 2.5: 3, -7.6: -8, 0: 0} {
		if got := RoundRenown(in); got != want {
			t.Errorf("RoundRenown(%v) = %v, want %v", in, got, want)
		}
	}
	for _, b := range Backgrounds {
		if b.Renown < 1 || b.Renown != float64(int(b.Renown)) {
			t.Errorf("%s starts with %v renown; want a whole number, at least 1", b.Key, b.Renown)
		}
	}
	if s := NewSheet(nil, 0, 30, 0, 0); s[AttrRenown] != 3 {
		t.Errorf("checks see renown ÷ RenownScale: %v", s[AttrRenown])
	}
}
