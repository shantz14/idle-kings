package game

// ---- misdeeds: what spies can expose ----
//
// Some secrets are shameful whatever the laws say: stealing, bribery, paying
// bandits, spreading lies. When spies learn enough to name the culprit, the
// report goes into the spymaster's dossier, where it keeps forever. Exposing
// it shames the culprit before the court; the Informers' Law makes it hit
// harder and pays the informer. What it does is decided when it's used,
// under the laws standing then. All numbers are placeholders.

// KindDossier is the menu key for Your Dossier (instant, not an activity).
const KindDossier = "dossier"

type MisdeedDef struct {
	Word     string  // "denounced for <Word>"
	Severity float64 // 1 = the worst
	Offended []Trait // senators who take it twice as badly
	Needs    LawKey  // only shameful while this law stands ("" = always)
}

var MisdeedDefs = map[string]MisdeedDef{
	"embezzle":  {Word: "stealing from the treasury", Severity: 1, Offended: []Trait{TraitIncorruptible, TraitEconomical}},
	"bandits":   {Word: "paying bandits to rob a traveller", Severity: 1, Offended: []Trait{TraitStern}},
	"bribe":     {Word: "bribing a senator", Severity: 0.6, Offended: []Trait{TraitIncorruptible}},
	"sold_vote": {Word: "selling votes to senators", Severity: 0.5, Offended: []Trait{TraitIncorruptible, TraitStern}},
	"vote_deal": {Word: "trading votes for gold", Severity: 0.4, Offended: []Trait{TraitIncorruptible}},
	"rumor":     {Word: "spreading lies about another house", Severity: 0.4, Offended: []Trait{TraitStern}},
	"plant":     {Word: "spying on another house", Severity: 0.3, Offended: []Trait{TraitStern}},
	"whisper":   {Word: "spreading scandal about another house", Severity: 0.3, Offended: []Trait{TraitStern}},
	"magic":     {Word: "dealing in enchanted goods", Severity: 0.6, Offended: []Trait{TraitPious, TraitConservative}, Needs: LawWitchcraftAct},
}

const (
	exposeRenown     = 2.0  // × severity, off the culprit
	exposeOpinion    = -5.5 // × severity, every senator toward the culprit
	informersBoost   = 1.5  // the Informers' Law makes exposure hit harder
	offendedMultiple = 2.0  // offended traits take it this much worse
	whisperRenown    = 1.0  // × severity: a whisper is softer than going public
	whisperOpinion   = -4.0 // × severity
)

// IsMisdeed reports whether a secret of this kind is shameful under the laws.
func IsMisdeed(kind string, ls Laws) bool {
	m, ok := MisdeedDefs[kind]
	return ok && (m.Needs == "" || ls.Has(m.Needs))
}

// CanExpose reports whether a leaked secret goes in the dossier: it's a
// misdeed, and the spies learned enough to name the culprit.
func CanExpose(kind string, d Detail, ls Laws) bool {
	return IsMisdeed(kind, ls) && d >= Partial
}

// MisdeedWord names a misdeed in public.
func MisdeedWord(kind string) string {
	if m, ok := MisdeedDefs[kind]; ok {
		return m.Word
	}
	return "wrongdoing"
}

// Denunciation is what exposing a misdeed does.
type Denunciation struct {
	Accused  Delta
	Informer Delta
	Opinions []OpinionChange
	Realm    Realm
	Paid     bool // the Informers' Law paid the informer
	Crime    bool // it's a crime under the laws, so it goes to the magistrates
}

// Denounce exposes a misdeed under the laws standing now. Without the
// Informers' Law it only shames the culprit; with it, the shame is greater
// and the treasury pays the informer.
func Denounce(senators []Senator, kind string, accusedID, informerID int64, informerInfluence float64, ls Laws) Denunciation {
	m := MisdeedDefs[kind]
	sev := m.Severity
	if sev == 0 {
		sev = 0.3
	}
	out := Denunciation{Crime: IsCrime(kind, ls)}
	if ls.Has(LawInformers) {
		sev *= informersBoost
		out.Paid = true
		out.Informer = Delta{Wealth: LawDenounceReward, Influence: ScaleInfluence(LawDenounceInfluence, informerInfluence)}
		out.Realm = Realm{AttrTreasury: -LawDenounceTreasury}
		out.Opinions = lawOpinions(senators, informerID, LawDenounceGood, TraitStern, TraitIncorruptible)
	}
	out.Accused = Delta{Renown: -exposeRenown * sev}
	out.Opinions = append(out.Opinions, misdeedOpinions(senators, m, accusedID, exposeOpinion*sev)...)
	return out
}

// Whisper spreads a misdeed quietly instead of denouncing it. It's softer,
// never reaches the magistrates (so the Informers' Law neither boosts nor
// pays it), and the culprit only hears that someone is talking.
func Whisper(senators []Senator, kind string, accusedID int64) Denunciation {
	m := MisdeedDefs[kind]
	sev := m.Severity
	if sev == 0 {
		sev = 0.3
	}
	out := Denunciation{Accused: Delta{Renown: -whisperRenown * sev}}
	out.Opinions = misdeedOpinions(senators, m, accusedID, whisperOpinion*sev)
	return out
}

// misdeedOpinions is every senator's opinion change toward the culprit,
// doubled for the offended traits.
func misdeedOpinions(senators []Senator, m MisdeedDef, accusedID int64, base float64) []OpinionChange {
	var out []OpinionChange
	for _, s := range senators {
		d := base
		for _, t := range m.Offended {
			if s.Has(t) {
				d *= offendedMultiple
				break
			}
		}
		out = append(out, OpinionChange{SenatorID: s.ID, CharacterID: accusedID, Delta: d})
	}
	return out
}
