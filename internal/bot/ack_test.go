package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// recorder answers every Discord REST call with {} and remembers it.
type recorder struct {
	mu   sync.Mutex
	reqs []string
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		var v struct {
			Type int `json:"type"`
		}
		_ = json.Unmarshal(b, &v)
		if v.Type != 0 {
			body = " type=" + string(rune('0'+v.Type))
		}
	}
	path := req.URL.Path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		path = path[i+1:]
	}
	r.mu.Lock()
	r.reqs = append(r.reqs, req.Method+" "+path+body)
	r.mu.Unlock()
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{"Content-Type": {"application/json"}}, Request: req}, nil
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reqs...)
}

func ackBot(t *testing.T) (*Bot, *recorder) {
	t.Helper()
	s, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s.Client = &http.Client{Transport: rec}
	return &Bot{s: s}, rec
}

func ackClick(id string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		ID: id, AppID: "app", Token: "tok", Type: discordgo.InteractionMessageComponent, Message: &discordgo.Message{},
	}}
}

func TestSlowClickIsDeferredThenEdited(t *testing.T) {
	b, rec := ackBot(t)
	i := ackClick("slow")
	stop := b.autoDefer(i)
	time.Sleep(autoDeferAfter + 300*time.Millisecond)
	b.update(i, noteView("done", colorMuted))
	b.finish(i, stop)
	got := rec.got()
	if len(got) != 2 || got[0] != "POST callback type=6" || !strings.HasPrefix(got[1], "PATCH @original") {
		t.Fatalf("requests = %q, want a deferred update then an edit", got)
	}
}

func TestFastClickIsAnsweredOnce(t *testing.T) {
	b, rec := ackBot(t)
	i := ackClick("fast")
	stop := b.autoDefer(i)
	b.update(i, noteView("done", colorMuted))
	b.finish(i, stop)
	time.Sleep(autoDeferAfter + 300*time.Millisecond) // the timer must not fire after
	if got := rec.got(); len(got) != 1 || got[0] != "POST callback type=7" {
		t.Fatalf("requests = %q, want one update", got)
	}
}

func TestUnansweredClickGetsANote(t *testing.T) {
	b, rec := ackBot(t)
	i := ackClick("silent")
	b.finish(i, b.autoDefer(i))
	if got := rec.got(); len(got) != 1 || got[0] != "POST callback type=4" {
		t.Fatalf("requests = %q, want a reply", got)
	}
}

func TestSecondAnswerFollowsUp(t *testing.T) {
	b, rec := ackBot(t)
	i := ackClick("twice")
	b.update(i, noteView("one", colorMuted))
	b.reply(i, "two")
	if got := rec.got(); len(got) != 2 || got[0] != "POST callback type=7" || !strings.HasPrefix(got[1], "POST tok") {
		t.Fatalf("requests = %q, want an update then a follow-up", got)
	}
}
