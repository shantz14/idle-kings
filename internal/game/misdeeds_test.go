package game

import "testing"

func TestMisdeedsAreRealSecrets(t *testing.T) {
	secrets := []Secret{
		SecretBribe("a", "s", "m", "o", 1), SecretHiredBandits("a", "t", "b", 1), SecretSoldToSenator("a", "s", "m", "o", 1),
		SecretVoteOffer("a", 2, "b", "m", "o", 1, true), SecretVoteDeal("a", 2, "b", "m", "o", 1, true),
		SecretVoteDeal("a", 2, "b", "m", "o", 1, false), SecretVote("a", "m", "o"), SecretCourt("a", "s", "x"),
		SecretCourtResult("a", "s", true, false), SecretRumor("a", 2, "b"), SecretPlant("a", 2, "b"), SecretRootOut("a"),
		SecretInvest("a", "s", 1), SecretEstateOffer("a", 2, "b", "x"), SecretEstateDeal("a", 2, "b", "x"),
		SecretCanvass("a"), SecretEmbezzle("a", 1), SecretMagic("a", "i"), SecretWhisper("a", "b", "c"),
	}
	kinds := map[string]bool{}
	for _, s := range secrets {
		kinds[s.Kind] = true
	}
	for k, m := range MisdeedDefs {
		if !kinds[k] {
			t.Errorf("misdeed %q is no secret's kind", k)
		}
		if m.Word == "" || m.Severity <= 0 || m.Severity > 1 {
			t.Errorf("misdeed %q: %+v", k, m)
		}
	}
	if IsMisdeed(SecretVoteDeal("a", 2, "b", "m", "o", 1, false).Kind, nil) {
		t.Error("turning down a vote deal is nothing to be ashamed of")
	}
	if !IsMisdeed(SecretVoteDeal("a", 2, "b", "m", "o", 1, true).Kind, nil) {
		t.Error("striking a vote deal is")
	}
}

func TestCanExpose(t *testing.T) {
	if CanExpose("embezzle", Vague, nil) || !CanExpose("embezzle", Partial, nil) {
		t.Error("exposing needs enough detail to name the culprit, and no law")
	}
	if !CanExpose("bribe", Full, nil) {
		t.Error("bribery is shameful even where it's legal")
	}
	if CanExpose("magic", Full, nil) || !CanExpose("magic", Full, lawsOf(LawWitchcraftAct)) {
		t.Error("enchanted goods are only shameful under the Witchcraft Act")
	}
	if CanExpose("canvass", Full, lawsOf(LawInformers)) {
		t.Error("canvassing is no misdeed")
	}
}

func TestDenounceUnderTheLaws(t *testing.T) {
	sen := []Senator{{ID: 1}, {ID: 2, Traits: []Trait{TraitIncorruptible}}, {ID: 3, Traits: []Trait{TraitStern}}}
	opinionOf := func(d Denunciation, senator, char int64) float64 {
		var sum float64
		for _, o := range d.Opinions {
			if o.SenatorID == senator && o.CharacterID == char {
				sum += o.Delta
			}
		}
		return sum
	}

	plain := Denounce(sen, "bribe", 5, 6, 10, nil)
	if plain.Paid || plain.Crime || plain.Informer.Wealth != 0 || len(plain.Realm) != 0 {
		t.Errorf("no reward without the Informers' Law: %+v", plain)
	}
	if plain.Accused.Renown >= 0 || opinionOf(plain, 1, 5) >= 0 {
		t.Errorf("it should still shame them: %+v", plain)
	}
	if got, base := opinionOf(plain, 2, 5), opinionOf(plain, 1, 5); got != base*offendedMultiple {
		t.Errorf("the Incorruptible take bribery twice as badly: %v vs %v", got, base)
	}
	if opinionOf(plain, 3, 6) != 0 {
		t.Error("nobody thanks an informer without the law")
	}

	informed := Denounce(sen, "bribe", 5, 6, 10, lawsOf(LawInformers, LawBribery))
	if !informed.Paid || !informed.Crime || informed.Informer.Wealth != LawDenounceReward || informed.Realm[AttrTreasury] >= 0 {
		t.Errorf("the Informers' Law pays: %+v", informed)
	}
	if informed.Accused.Renown != plain.Accused.Renown*informersBoost {
		t.Errorf("and hits harder: %v vs %v", informed.Accused.Renown, plain.Accused.Renown)
	}
	if opinionOf(informed, 3, 6) <= 0 {
		t.Error("Stern senators like an informer")
	}
	if worse := Denounce(sen, "embezzle", 5, 6, 10, nil); worse.Accused.Renown >= plain.Accused.Renown {
		t.Error("stealing from the treasury is worse than a bribe")
	}
}
