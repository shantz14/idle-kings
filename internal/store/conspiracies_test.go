package store

import (
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/shantz14/idle-kings/internal/game"
)

func TestConspiracyLifecycle(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Unix(1_000_000, 0)
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), game.NewSenate(rand.New(rand.NewPCG(1, 2)), game.SenateStandard), now); err != nil {
		t.Fatal(err)
	}
	sen, _ := s.Senators(ctx, "g")
	leader, friend, victim := newChar(t, s, "lead", "merchant"), newChar(t, s, "friend", "merchant"), newChar(t, s, "victim", "merchant")
	give(t, s, leader, 500)

	start := func() (Conspiracy, error) {
		return s.StartConspiracy(ctx, Activity{GuildID: "g", HostID: leader.ID, Goal: game.SchemeAssassinate, TargetID: victim.ID,
			StartedAt: now, EndsAt: now.Add(5 * time.Hour)}, 50, Conspiracy{TargetKind: game.TargetPlayer, Purse: "some", NextStepAt: now.Add(time.Hour)})
	}
	k, err := start()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := start(); !errors.Is(err, ErrPlotting) {
		t.Fatalf("second plot: %v", err)
	}
	if _, err := s.HostedActivity(ctx, leader.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a plot shouldn't take the leader's action: %v", err)
	}
	if got := reload(t, s, leader).Wealth; got != leader.Wealth+500-50 {
		t.Fatalf("purse not paid: %d", got)
	}

	if err := s.InviteMember(ctx, k.ID, MemberPlayer, friend.ID, MemberInvited, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := s.InviteMember(ctx, k.ID, MemberPlayer, friend.ID, MemberInvited, 0, now); !errors.Is(err, ErrInvited) {
		t.Fatalf("asked twice: %v", err)
	}
	if err := s.InviteMember(ctx, k.ID, MemberSenator, sen[0].ID, MemberJoined, 30, now); err != nil {
		t.Fatal(err)
	}
	if mine, err := s.ConspiraciesOf(ctx, friend.ID); err != nil || len(mine) != 1 {
		t.Fatalf("ConspiraciesOf = %v, %v", mine, err)
	}
	if err := s.SetMemberStatus(ctx, k.ID, MemberPlayer, friend.ID, MemberInvited, MemberJoined); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMemberStatus(ctx, k.ID, MemberPlayer, friend.ID, MemberInvited, MemberJoined); !errors.Is(err, ErrHandled) {
		t.Fatalf("answered twice: %v", err)
	}
	k, _ = s.LedConspiracy(ctx, leader.ID)
	if got := k.Joined(MemberPlayer); len(got) != 1 || got[0] != friend.ID || len(k.Joined(MemberSenator)) != 1 {
		t.Fatalf("members: %+v", k.Members)
	}

	due, err := s.DueConspiracies(ctx, now.Add(time.Hour))
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %v, %v", due, err)
	}
	if err := s.StepConspiracy(ctx, due[0], 40, game.Suspected, now.Add(2*time.Hour), false, Outcome{}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.StepConspiracy(ctx, due[0], 80, game.Suspected, now.Add(2*time.Hour), false, Outcome{}, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("a day stepped twice: %v", err)
	}
	if against, _ := s.ConspiraciesAgainst(ctx, victim.ID); len(against) != 1 || against[0].Progress != 40 || against[0].Warned != game.Suspected {
		t.Fatalf("against = %+v", against)
	}

	strike, err := s.StartStrike(ctx, k, now.Add(time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartStrike(ctx, k, now.Add(time.Hour), now); err == nil {
		t.Fatal("struck twice")
	}

	// The victim had business going: all of it ends with them.
	give(t, s, victim, 400)
	victim = reload(t, s, victim)
	if err := s.ChangeOpinions(ctx, []game.OpinionChange{{SenatorID: sen[1].ID, CharacterID: victim.ID, Delta: 40}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeSpies(ctx, []game.SpyChange{{Owner: victim.ID, Target: leader.ID, Delta: 50}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: victim.ID, Kind: game.KindRootOut, StartedAt: now, EndsAt: now.Add(time.Hour)}, 0, nil); err != nil {
		t.Fatal(err)
	}
	heir := game.Inherit("Livia", victim.Stats, victim.Renown, victim.Influence, victim.Wealth)
	newSen := game.ReplaceSenator(rand.New(rand.NewPCG(5, 6)), sen, sen[0].ID)
	err = s.ResolveStrike(ctx, strike.ID, k.ID, Outcome{
		Deaths:        []Death{{CharacterID: victim.ID, Heir: heir}},
		Senators:      []game.Senator{newSen},
		Denunciations: []Denunciation{{GuildID: "g", OwnerID: leader.ID, ActorID: friend.ID, Crime: game.SecretKindBribe, Detail: "forged"}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveStrike(ctx, strike.ID, k.ID, Outcome{}, now); !errors.Is(err, ErrEnded) {
		t.Fatalf("resolved twice: %v", err)
	}

	got := reload(t, s, victim)
	if got.Name != "Livia" || got.FamilyName != victim.FamilyName || got.Wealth != heir.Wealth || len(got.Items) != len(victim.Items) {
		t.Errorf("heir: %+v", got)
	}
	if ops, _ := s.Opinions(ctx, victim.ID); ops[sen[1].ID] != 20 {
		t.Errorf("opinions should carry over at half: %v", ops)
	}
	if lvl, _ := s.SpyLevel(ctx, victim.ID, leader.ID); lvl != 25 {
		t.Errorf("spies should carry over at half: %v", lvl)
	}
	if _, err := s.HostedActivity(ctx, victim.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the dead's business should end: %v", err)
	}
	if unnamed, _ := s.HeirUnnamed(ctx, victim.ID); !unnamed {
		t.Error("the heir is waiting for a name")
	}
	if err := s.NameHeir(ctx, victim.ID, "Junia"); err != nil || reload(t, s, victim).Name != "Junia" {
		t.Errorf("NameHeir: %v", err)
	}
	if err := s.NameHeir(ctx, victim.ID, "Again"); !errors.Is(err, ErrHandled) {
		t.Errorf("named twice: %v", err)
	}
	sen2, _ := s.Senators(ctx, "g")
	for _, x := range sen2 {
		if x.ID == sen[0].ID && x.Name != newSen.Name {
			t.Errorf("the seat should be %s's: %+v", newSen.Name, x)
		}
	}
	if d, _ := s.Dossier(ctx, leader.ID); len(d) != 1 || d[0].ActorID != friend.ID {
		t.Errorf("forgery not filed: %+v", d)
	}
	if mine, _ := s.ConspiraciesOf(ctx, friend.ID); len(mine) != 0 {
		t.Errorf("the plot is over: %+v", mine)
	}
}

func TestStealOnlyTakesPossessions(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Now()
	thief, mark := newChar(t, s, "thief", "merchant"), newChar(t, s, "mark", "merchant")
	home, _ := mark.Residence()
	var trinket Holding
	for _, h := range mark.Holdings {
		if it, ok := h.Item(); ok && !it.IsProperty() {
			trinket = h
		}
	}
	if trinket.ID == 0 {
		t.Skip("the test background has no possessions")
	}
	out := Outcome{Transfers: []ItemTransfer{
		{HoldingID: home.ID, From: mark.ID, To: thief.ID},
		{HoldingID: trinket.ID, From: mark.ID, To: thief.ID},
	}}
	if err := s.ApplyOutcome(ctx, "g", out, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := reload(t, s, mark).Holding(home.ID); !ok {
		t.Error("a house can't be carried off")
	}
	if _, ok := reload(t, s, thief).Holding(trinket.ID); !ok {
		t.Error("the trinket should be the thief's")
	}
}

func TestRushCharacter(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Unix(2_000_000, 0)
	c, other := newChar(t, s, "c", "merchant"), newChar(t, s, "o", "merchant")
	if _, err := s.StartActivity(ctx, Activity{GuildID: "g", HostID: c.ID, Kind: game.KindRootOut, StartedAt: now, EndsAt: now.Add(time.Hour)}, 0, nil); err != nil {
		t.Fatal(err)
	}
	k, err := s.StartConspiracy(ctx, Activity{GuildID: "g", HostID: c.ID, Goal: game.SchemeFrame, TargetID: other.ID, StartedAt: now,
		EndsAt: now.Add(5 * time.Hour)}, 0, Conspiracy{TargetKind: game.TargetPlayer, Purse: "none", NextStepAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RushCharacter(ctx, c.ID, now); err != nil {
		t.Fatal(err)
	}
	ended, _ := s.EndedActivities(ctx, now)
	if len(ended) != 1 || ended[0].Kind != game.KindRootOut {
		t.Errorf("only the action should end: %+v", ended)
	}
	if due, _ := s.DueConspiracies(ctx, now); len(due) != 1 || due[0].ID != k.ID {
		t.Errorf("the plot should move on a day: %+v", due)
	}
}

func TestPlotTermsPaidOnJoining(t *testing.T) {
	s, ctx := estateStore(t)
	now := time.Unix(1_000_000, 0)
	if err := s.CreateRealm(ctx, "g", game.StartingRealm(), game.NewSenate(rand.New(rand.NewPCG(1, 2)), game.SenateStandard), now); err != nil {
		t.Fatal(err)
	}
	leader, friend, poor, victim := newChar(t, s, "lead", "merchant"), newChar(t, s, "friend", "merchant"), newChar(t, s, "poor", "merchant"), newChar(t, s, "victim", "merchant")
	k, err := s.StartConspiracy(ctx, Activity{GuildID: "g", HostID: leader.ID, Goal: game.SchemeSteal, TargetID: victim.ID,
		StartedAt: now, EndsAt: now.Add(5 * time.Hour)}, 0, Conspiracy{TargetKind: game.TargetPlayer, NextStepAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	offer := game.PlotTerms{Gold: 50, Share: 25}
	if err := s.InvitePlayer(ctx, k.ID, friend.ID, offer, now); err != nil {
		t.Fatal(err)
	}
	if err := s.InvitePlayer(ctx, k.ID, poor.ID, game.PlotTerms{Gold: 100000}, now); err != nil {
		t.Fatal(err)
	}
	if terms, err := s.PlotTerms(ctx, k.ID); err != nil || terms[friend.ID] != offer {
		t.Fatalf("terms %v %v", terms, err)
	}
	if got, err := s.JoinPlot(ctx, k.ID, friend.ID); err != nil || got != offer {
		t.Fatalf("join: %+v %v", got, err)
	}
	if reload(t, s, friend).Wealth != friend.Wealth+50 || reload(t, s, leader).Wealth != leader.Wealth-50 {
		t.Fatal("the gold up front should move from leader to member")
	}
	if _, err := s.JoinPlot(ctx, k.ID, friend.ID); !errors.Is(err, ErrHandled) {
		t.Fatalf("joining twice: %v", err)
	}
	// A leader who can't pay leaves the invitation open.
	if _, err := s.JoinPlot(ctx, k.ID, poor.ID); !errors.Is(err, ErrLeaderShort) {
		t.Fatalf("unpaid join: %v", err)
	}
	k, _ = s.GetConspiracy(ctx, k.ID)
	if m, _ := k.Member(MemberPlayer, poor.ID); m.Status != MemberInvited {
		t.Fatalf("still invited: %+v", m)
	}
}
