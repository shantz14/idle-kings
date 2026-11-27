package game

import (
	"strings"
	"testing"
)

func TestContractsGrowTheirSector(t *testing.T) {
	for key, k := range contractOptions {
		ev, opt, _ := strings.Cut(key, "/")
		e, ok := MatterByKey(ev)
		if !ok {
			t.Errorf("%s: no such event", key)
			continue
		}
		o, ok := e.Option(opt)
		if !ok {
			t.Errorf("%s: no such option", key)
			continue
		}
		if ContractPay(o.Success, k) <= 0 {
			t.Errorf("%s: its success doesn't grow %s", key, k)
		}
		if c, ok := Contract(ev, opt); !ok || c != k {
			t.Errorf("%s: lookup", key)
		}
	}
	if _, ok := Contract("river_floods", "wait"); ok {
		t.Error("only hand-picked options carry contracts")
	}
	if ContractHint(SectorMining) == "" {
		t.Error("hint")
	}
}

func TestContractWinnerAndLeader(t *testing.T) {
	if id, ok := ContractWinner(map[int64]float64{3: 100, 2: 250, 5: 250, 9: 0}); !ok || id != 2 {
		t.Fatalf("winner %d %v", id, ok)
	}
	if _, ok := ContractWinner(map[int64]float64{1: 0}); ok {
		t.Fatal("nobody with a stake, no contract")
	}
	d, op := LeaderResult(true, 0)
	if d.Renown != leaderRenown || d.Influence != leaderInfluence || op != 0 {
		t.Fatalf("credit %+v %v", d, op)
	}
	d, op = LeaderResult(false, 0)
	if d.Renown != leaderBlame || op != leaderBlameOp || d.Influence != 0 {
		t.Fatalf("blame %+v %v", d, op)
	}
}
