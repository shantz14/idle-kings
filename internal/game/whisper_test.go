package game

import (
	"strings"
	"testing"
)

func TestWhisper(t *testing.T) {
	sen := []Senator{{ID: 1}, {ID: 2, Traits: []Trait{TraitIncorruptible}}}
	opinion := func(d Denunciation, senator int64) float64 {
		for _, o := range d.Opinions {
			if o.SenatorID == senator && o.CharacterID == 5 {
				return o.Delta
			}
		}
		return 0
	}
	public := Denounce(sen, "bribe", 5, 6, 10, nil)
	w := Whisper(sen, "bribe", 5)
	if w.Paid || w.Informer.Wealth != 0 || len(w.Realm) != 0 {
		t.Errorf("a whisper pays nothing and costs the treasury nothing: %+v", w)
	}
	if w.Accused.Renown >= 0 || w.Accused.Renown <= public.Accused.Renown {
		t.Errorf("a whisper should hurt, but less than going public: %v vs %v", w.Accused.Renown, public.Accused.Renown)
	}
	if o := opinion(w, 1); o >= 0 || o <= opinion(public, 1) {
		t.Errorf("whispered opinion %v vs public %v", o, opinion(public, 1))
	}
	if opinion(w, 2) != 2*opinion(w, 1) {
		t.Error("offended senators still take it twice as badly")
	}
	if !IsMisdeed(SecretWhisper("a", "b", "c").Kind, nil) {
		t.Error("whispering is itself a misdeed spies can file")
	}
	if IsCrime("whisper", nil) || !IsCrime("whisper", lawsOf(LawSlander)) {
		t.Error("whispering is only a crime under the Law of Slander")
	}
	for k, m := range MisdeedDefs {
		if strings.Contains(m.Word, "their") {
			t.Errorf("%s: %q must read after \"you've been\"", k, m.Word)
		}
	}
}
