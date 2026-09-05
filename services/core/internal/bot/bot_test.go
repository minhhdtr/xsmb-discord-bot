package bot_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/minhhdtr/xsmb-discord-bot/internal/bot"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

func memStore() *storage.Memory { return storage.NewMemory() }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func samplePrizes(t *testing.T) domain.Prizes {
	t.Helper()
	numbers := make([]string, 0, domain.TotalNumbers)
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			numbers = append(numbers, fmt.Sprintf("%0*d", spec.Digits, n+1))
		}
	}
	p, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func sampleDraw(t *testing.T, day time.Time) domain.Draw {
	t.Helper()
	return domain.Draw{Date: domain.DayOf(day), Prizes: samplePrizes(t),
		Source: "xoso.com.vn", FetchedAt: day.Add(18*time.Hour + 36*time.Minute)}
}

type stubProvider struct {
	mu     sync.Mutex
	calls  int
	answer func(day time.Time) domain.Outcome
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) Fetch(_ context.Context, day time.Time) domain.Outcome {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.answer(day)
}
func (s *stubProvider) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func at(hour, minute int) time.Time {
	return time.Date(2026, 8, 21, hour, minute, 0, 0, domain.Location())
}

func TestDrawEmbedFitsDiscordLimits(t *testing.T) {
	embed := bot.DrawEmbed(sampleDraw(t, domain.NewDate(2026, 8, 20)), true)
	if len(embed.Description) > 4096 {
		t.Fatalf("description is %d chars", len(embed.Description))
	}
	for _, f := range embed.Fields {
		if len(f.Value) > 1024 {
			t.Fatalf("field %q is %d chars", f.Name, len(f.Value))
		}
	}
	if !strings.Contains(embed.Title, "20/08/2026") || !strings.Contains(embed.Title, "Thứ Năm") {
		t.Fatalf("title = %q", embed.Title)
	}
	if !strings.Contains(embed.Description, "00001") {
		t.Fatal("special prize missing from description")
	}
	if bot.DrawEmbed(sampleDraw(t, domain.NewDate(2026, 8, 20)), false).Color == embed.Color {
		t.Fatal("announcement and query embeds share a colour")
	}
}

// --- command parsing ---

func TestParseCommand(t *testing.T) {
	cases := []struct {
		content string
		ok      bool
		args    []string
	}{
		{"!xsmb", true, []string{}},
		{"  !xsmb  ", true, []string{}},
		{"!XSMB", true, []string{}},
		{"!xsmb 14/08/2026", true, []string{"14/08/2026"}},
		{"!xsmb   14/08/2026  ", true, []string{"14/08/2026"}},
		{"!xsmb sub", true, []string{"sub"}},
		{"!xsmbfoo", false, nil},
		{"xsmb", false, nil},
		{"hello !xsmb", false, nil},
		{"", false, nil},
	}
	for _, c := range cases {
		args, ok := bot.ParseCommand(c.content, "!xsmb")
		if ok != c.ok {
			t.Fatalf("ParseCommand(%q) ok = %v, want %v", c.content, ok, c.ok)
		}
		if ok && strings.Join(args, ",") != strings.Join(c.args, ",") {
			t.Fatalf("ParseCommand(%q) args = %v, want %v", c.content, args, c.args)
		}
	}
}

// --- routing ---

func newRouter(t *testing.T, clock func() time.Time, answer func(time.Time) domain.Outcome) (*bot.Router, *storage.Memory, *stubProvider) {
	t.Helper()
	store := storage.NewMemory()
	src := &stubProvider{answer: answer}
	svc := service.New(store, src, clock, quiet())
	return bot.NewRouter(coreOver(t, svc, store, nil), svc.Now, "!xsmb", "!gold", quiet()), store, src
}

func found(t *testing.T) func(time.Time) domain.Outcome {
	return func(day time.Time) domain.Outcome { return domain.Found(sampleDraw(t, day)) }
}

func TestRouterLatest(t *testing.T) {
	r, _, _ := newRouter(t, func() time.Time { return at(20, 0) }, found(t))
	embed := r.Handle(context.Background(), bot.Request{}).Embed
	if !strings.Contains(embed.Title, "21/08/2026") {
		t.Fatalf("title = %q", embed.Title)
	}
}

func TestRouterByDate(t *testing.T) {
	r, _, _ := newRouter(t, func() time.Time { return at(20, 0) }, found(t))
	for _, arg := range []string{"14/08/2026", "14-08-2026", "2026-08-14"} {
		embed := r.Handle(context.Background(), bot.Request{Args: []string{arg}}).Embed
		if !strings.Contains(embed.Title, "14/08/2026") {
			t.Fatalf("%s: title = %q", arg, embed.Title)
		}
	}
}

func TestRouterUnreadableDate(t *testing.T) {
	r, _, src := newRouter(t, func() time.Time { return at(20, 0) }, found(t))
	embed := r.Handle(context.Background(), bot.Request{Args: []string{"hôm", "qua"}}).Embed
	if !strings.Contains(embed.Title, "Không đọc được ngày") {
		t.Fatalf("title = %q", embed.Title)
	}
	if src.Calls() != 0 {
		t.Fatal("a bad date reached the network")
	}
}

func TestRouterExplainsNotYetVersusNoResult(t *testing.T) {
	// Before 18:35, today missing means "not out yet".
	r, _, _ := newRouter(t, func() time.Time { return at(10, 0) },
		func(time.Time) domain.Outcome { return domain.Absent() })
	embed := r.Handle(context.Background(), bot.Request{Args: []string{"21/08/2026"}}).Embed
	if !strings.Contains(embed.Title, "Chưa có kết quả") {
		t.Fatalf("title = %q", embed.Title)
	}
	if !strings.Contains(embed.Description, "18h35") {
		t.Fatalf("description should mention the draw time: %q", embed.Description)
	}

	// A settled past day with no draw is a different message.
	r2, _, _ := newRouter(t, func() time.Time { return at(20, 0) },
		func(time.Time) domain.Outcome { return domain.Absent() })
	embed2 := r2.Handle(context.Background(), bot.Request{Args: []string{"07/03/2011"}}).Embed
	if !strings.Contains(embed2.Title, "Không có kết quả") {
		t.Fatalf("title = %q", embed2.Title)
	}
}

func TestRouterRejectsOutOfRangeDate(t *testing.T) {
	r, _, src := newRouter(t, func() time.Time { return at(20, 0) }, found(t))
	for _, arg := range []string{"01/01/1999", "25/12/2026"} {
		embed := r.Handle(context.Background(), bot.Request{Args: []string{arg}}).Embed
		if !strings.Contains(embed.Title, "Ngày không hợp lệ") {
			t.Fatalf("%s: title = %q", arg, embed.Title)
		}
	}
	if src.Calls() != 0 {
		t.Fatal("an out-of-range date reached the network")
	}
}

func TestRouterSubscriptionRequiresPermission(t *testing.T) {
	r, store, _ := newRouter(t, func() time.Time { return at(20, 0) }, found(t))
	ctx := context.Background()

	denied := r.Handle(ctx, bot.Request{Args: []string{"sub"}, GuildID: "g1", ChannelID: "c1"}).Embed
	if !strings.Contains(denied.Title, "Không đủ quyền") {
		t.Fatalf("title = %q", denied.Title)
	}
	if subs, _ := store.Subscriptions(ctx); len(subs) != 0 {
		t.Fatal("subscription created without permission")
	}

	allowed := r.Handle(ctx, bot.Request{Args: []string{"sub"}, GuildID: "g1", ChannelID: "c1", CanManage: true}).Embed
	if !strings.Contains(allowed.Title, "Đã bật thông báo") {
		t.Fatalf("title = %q", allowed.Title)
	}
	again := r.Handle(ctx, bot.Request{Args: []string{"sub"}, GuildID: "g1", ChannelID: "c1", CanManage: true}).Embed
	if !strings.Contains(again.Title, "Đã bật từ trước") {
		t.Fatalf("title = %q", again.Title)
	}
	off := r.Handle(ctx, bot.Request{Args: []string{"unsub"}, GuildID: "g1", ChannelID: "c1", CanManage: true}).Embed
	if !strings.Contains(off.Title, "Đã tắt thông báo") {
		t.Fatalf("title = %q", off.Title)
	}
}

func TestRouterSubscriptionRejectedInDirectMessages(t *testing.T) {
	r, _, _ := newRouter(t, func() time.Time { return at(20, 0) }, found(t))
	embed := r.Handle(context.Background(), bot.Request{Args: []string{"sub"}, ChannelID: "dm", CanManage: true}).Embed
	if !strings.Contains(embed.Title, "Chỉ dùng trong server") {
		t.Fatalf("title = %q", embed.Title)
	}
}

func TestRouterHelpAndStatus(t *testing.T) {
	r, _, _ := newRouter(t, func() time.Time { return at(20, 0) }, found(t))
	ctx := context.Background()
	help := r.Handle(ctx, bot.Request{Args: []string{"help"}}).Embed
	var listed strings.Builder
	for _, f := range help.Fields {
		listed.WriteString(f.Value)
	}
	for _, want := range []string{"/xsmb", "/thongke logan", "/thongbao"} {
		if !strings.Contains(listed.String(), want) {
			t.Fatalf("help does not list %s:\n%s", want, listed.String())
		}
	}
	// The alias forms are gone; anything else is read as a date.
	for _, alias := range []string{"?", "huong", "huongdan"} {
		if embed := r.Handle(ctx, bot.Request{Args: []string{alias}}).Embed; embed.Title == "Hướng dẫn" {
			t.Fatalf("%q still reaches help", alias)
		}
	}
	status := r.Handle(ctx, bot.Request{Args: []string{"status"}}).Embed
	if !strings.Contains(status.Description, "0") {
		t.Fatalf("status = %q", status.Description)
	}
}

// --- announcer ---

type recorder struct {
	mu    sync.Mutex
	sent  []string
	fail  bool
	calls int
}

func (r *recorder) post(_ context.Context, channelID string, _ *discordgo.MessageEmbed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.fail {
		return errors.New("discord: 500")
	}
	r.sent = append(r.sent, channelID)
	return nil
}

func (r *recorder) channels() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

func newAnnouncer(t *testing.T, clock func() time.Time, answer func(time.Time) domain.Outcome) (*bot.Announcer, *storage.Memory, *recorder, *stubProvider) {
	t.Helper()
	store := storage.NewMemory()
	src := &stubProvider{answer: answer}
	svc := service.New(store, src, clock, quiet())
	rec := &recorder{}
	return bot.NewAnnouncer(coreOver(t, svc, store, nil), svc.Now, rec.post, quiet()), store, rec, src
}

func TestAnnouncerPostsToEverySubscriber(t *testing.T) {
	a, store, rec, _ := newAnnouncer(t, func() time.Time { return at(18, 36) }, found(t))
	ctx := context.Background()
	store.Subscribe(ctx, "g1", "c1")
	store.Subscribe(ctx, "g1", "c2")

	a.RunFor(ctx, domain.NewDate(2026, 8, 21))
	if got := len(rec.channels()); got != 2 {
		t.Fatalf("posted to %d channels, want 2", got)
	}
}

// A restart inside the announcement window must not repeat the message.
func TestAnnouncerIsIdempotentAcrossRuns(t *testing.T) {
	a, store, rec, _ := newAnnouncer(t, func() time.Time { return at(18, 36) }, found(t))
	ctx := context.Background()
	store.Subscribe(ctx, "g1", "c1")
	day := domain.NewDate(2026, 8, 21)

	a.RunFor(ctx, day)
	a.RunFor(ctx, day)
	a.RunFor(ctx, day)
	if got := rec.channels(); len(got) != 1 {
		t.Fatalf("posted %d times, want 1", len(got))
	}
}

// A rejected message must release its claim so the next run retries.
func TestAnnouncerRetriesAfterAFailedSend(t *testing.T) {
	a, store, rec, _ := newAnnouncer(t, func() time.Time { return at(18, 36) }, found(t))
	ctx := context.Background()
	store.Subscribe(ctx, "g1", "c1")
	day := domain.NewDate(2026, 8, 21)

	rec.fail = true
	a.RunFor(ctx, day)
	if len(rec.channels()) != 0 {
		t.Fatal("recorded a send that failed")
	}
	rec.fail = false
	a.RunFor(ctx, day)
	if got := rec.channels(); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("retry sent %v", got)
	}
}

func TestAnnouncerSkipsCrawlWithNoSubscribers(t *testing.T) {
	a, _, rec, src := newAnnouncer(t, func() time.Time { return at(18, 36) }, found(t))
	a.RunFor(context.Background(), domain.NewDate(2026, 8, 21))
	if src.Calls() != 0 {
		t.Fatalf("crawled %d times with nobody subscribed", src.Calls())
	}
	if len(rec.channels()) != 0 {
		t.Fatal("posted with nobody subscribed")
	}
}

func TestAnnouncerStaysSilentWhenThereIsNoDraw(t *testing.T) {
	a, store, rec, _ := newAnnouncer(t, func() time.Time { return at(20, 0) },
		func(time.Time) domain.Outcome { return domain.Absent() })
	ctx := context.Background()
	store.Subscribe(ctx, "g1", "c1")
	a.RunFor(ctx, domain.NewDate(2026, 8, 21))
	if len(rec.channels()) != 0 {
		t.Fatal("announced a day with no result")
	}
}

// 200 channels, one message each, no duplicates, and a failed send still
// releases its claim.
func TestAnnouncerFansOutWithoutLosingGuarantees(t *testing.T) {
	a, store, rec, _ := newAnnouncer(t, func() time.Time { return at(18, 36) }, found(t))
	a.SetFanOut(8, 0)
	ctx := context.Background()

	const channels = 200
	for i := 0; i < channels; i++ {
		if _, err := store.Subscribe(ctx, "g1", fmt.Sprintf("c%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	day := domain.NewDate(2026, 8, 21)

	start := time.Now()
	a.RunFor(ctx, day)
	elapsed := time.Since(start)

	got := rec.channels()
	if len(got) != channels {
		t.Fatalf("posted to %d channels, want %d", len(got), channels)
	}
	seen := make(map[string]int, channels)
	for _, id := range got {
		seen[id]++
	}
	for id, times := range seen {
		if times != 1 {
			t.Fatalf("%s got %d messages", id, times)
		}
	}
	// Sequential posting at the old 300ms gap would have taken a minute.
	if elapsed > 20*time.Second {
		t.Fatalf("fan-out took %v for %d channels", elapsed, channels)
	}

	// A second run must still be a no-op.
	a.RunFor(ctx, day)
	if len(rec.channels()) != channels {
		t.Fatalf("a repeat run posted again: %d messages", len(rec.channels()))
	}
}

func TestAnnouncerFanOutReleasesClaimsOnFailure(t *testing.T) {
	a, store, rec, _ := newAnnouncer(t, func() time.Time { return at(18, 36) }, found(t))
	a.SetFanOut(4, 0)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		store.Subscribe(ctx, "g1", fmt.Sprintf("c%02d", i))
	}
	day := domain.NewDate(2026, 8, 21)

	rec.fail = true
	a.RunFor(ctx, day)
	if len(rec.channels()) != 0 {
		t.Fatal("recorded sends that failed")
	}
	rec.fail = false
	a.RunFor(ctx, day)
	if got := len(rec.channels()); got != 12 {
		t.Fatalf("retry delivered %d of 12", got)
	}
}
