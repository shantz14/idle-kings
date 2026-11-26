package game

import "testing"

// A law's crimes must be misdeeds, or the dossier never files them.
func TestLawCrimesAreMisdeeds(t *testing.T) {
	for _, d := range LawDefs {
		for _, k := range d.Effects.Crimes {
			if _, ok := MisdeedDefs[k]; !ok {
				t.Errorf("%s makes %q a crime, but it is no misdeed", d.Key, k)
			}
		}
	}
}

func TestRulesFold(t *testing.T) {
	if r := Laws(nil).Rules(); r.BribePrice != 0 || r.SecretBallot || r.IsCrime(SecretKindBribe) || !r.IsCrime(SecretKindEmbezzle) {
		t.Fatalf("no laws: %+v", r)
	}
	r := Laws{{Key: LawBribery}, {Key: LawSecretBallot}, {Key: LawInformers}}.Rules()
	if r.BribePrice != LawBribeFactor || !r.SecretBallot || !r.IsCrime(SecretKindSoldVote) || r.LeakDetail != 1 {
		t.Fatalf("folded: %+v", r)
	}
	if got := mulFactor(mulFactor(0, 1.5), 2); got != 3 {
		t.Fatalf("mulFactor = %v", got)
	}
}
