package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/provider"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
)

// The bug on 04/09: nobody was subscribed, so nothing was ever fetched. The
// archive must not depend on whether anyone wants to be told.
func TestIngestFillsTheArchiveWithNobodyWatching(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(day time.Time) domain.Outcome {
		return domain.Found(drawFor(t, day))
	}}
	svc, store := newService(t, src, func() time.Time { return at(18, 40) })

	service.NewIngest(svc, quietLog()).CatchUp(context.Background())

	if _, found, err := store.Draw(context.Background(), today); err != nil || !found {
		t.Fatalf("ingest did not store today: found=%v err=%v", found, err)
	}
}

// LatestPublished says yesterday before 18:35, but the balls finish earlier.
// Ingest has to reach for today by name inside the landing window.
func TestIngestReachesForTodayBeforeTheMark(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(day time.Time) domain.Outcome {
		return domain.Found(drawFor(t, day))
	}}
	svc, store := newService(t, src, func() time.Time { return at(18, 30) })

	service.NewIngest(svc, quietLog()).CatchUp(context.Background())

	if _, found, _ := store.Draw(context.Background(), today); !found {
		t.Fatal("ingest ignored today at 18:30")
	}
}

// Outside the window, asking for today would be asking for a draw that has
// not happened.
func TestIngestDoesNotReachForTodayInTheMorning(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(day time.Time) domain.Outcome {
		return domain.Found(drawFor(t, day))
	}}
	svc, store := newService(t, src, func() time.Time { return at(9, 0) })

	service.NewIngest(svc, quietLog()).CatchUp(context.Background())

	if _, found, _ := store.Draw(context.Background(), today); found {
		t.Error("ingest fetched today at 09:00")
	}
	if _, found, _ := store.Draw(context.Background(), today.AddDate(0, 0, -1)); !found {
		t.Error("ingest did not fetch yesterday")
	}
}

// An empty page a minute after 18:35 is almost always mid-publication. An
// absence is a one-way door, so it must not be written on that evidence.
func TestEmptyPageJustAfterTheMarkIsNotAnAbsence(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(provider.ErrNoData)
	}}
	svc, store := newService(t, src, func() time.Time { return at(18, 36) })
	ctx := context.Background()

	_, err := svc.Get(ctx, today)
	if !errors.Is(err, service.ErrNotYet) {
		t.Fatalf("error = %v, want ErrNotYet", err)
	}
	if absent, _ := store.IsAbsent(ctx, today); absent {
		t.Fatal("the day was sealed as absent one minute after the mark")
	}
}

// Well past the grace, an empty page does mean the day was empty.
func TestEmptyPageLongAfterTheMarkIsAnAbsence(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(provider.ErrNoData)
	}}
	svc, store := newService(t, src, func() time.Time { return at(21, 0) })
	ctx := context.Background()

	if _, err := svc.Get(ctx, today); !errors.Is(err, service.ErrNoResult) {
		t.Fatalf("error = %v, want ErrNoResult", err)
	}
	if absent, _ := store.IsAbsent(ctx, today); !absent {
		t.Error("a confirmed empty day was not recorded")
	}
}

// Single-flight only collapses overlapping requests. A run of commands spaced
// apart used to be a run of crawls.
func TestSequentialMissesDoNotEachCrawl(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(time.Time) domain.Outcome {
		return domain.Failed(domain.ErrIncomplete)
	}}
	svc, _ := newService(t, src, func() time.Time { return at(18, 30) })
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := svc.Get(ctx, today); !errors.Is(err, service.ErrNotYet) {
			t.Fatalf("call %d: error = %v", i, err)
		}
	}
	if src.Calls() != 1 {
		t.Errorf("crawled %d times, want 1", src.Calls())
	}
}

// The note must never outrank the archive: a day Ingest has just written is
// served, not refused.
func TestANoteDoesNotHideADayThatLanded(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	ready := false
	src := &fakeProvider{fallback: func(day time.Time) domain.Outcome {
		if !ready {
			return domain.Failed(domain.ErrIncomplete)
		}
		return domain.Found(drawFor(t, day))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(18, 30) })
	ctx := context.Background()

	if _, err := svc.Get(ctx, today); !errors.Is(err, service.ErrNotYet) {
		t.Fatalf("first call: %v", err)
	}

	// Ingest gets it moments later, inside the note's lifetime.
	ready = true
	service.NewIngest(svc, quietLog()).CatchUp(ctx)

	if _, err := svc.Get(ctx, today); err != nil {
		t.Fatalf("the note hid a stored day: %v", err)
	}
}

// Before 18:35 LatestPublished says yesterday. Once the draw has settled,
// Latest should still find today.
func TestLatestReachesForTodayOnceTheDrawSettles(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(day time.Time) domain.Outcome {
		return domain.Found(drawFor(t, day))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(18, 30) })

	draw, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !draw.Date.Equal(today) {
		t.Errorf("Latest gave %s, want today", domain.FormatVN(draw.Date))
	}
}

// If today is genuinely not up yet, yesterday is the honest answer, not an
// error.
func TestLatestFallsBackToYesterdayWhenTodayIsNotUp(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(day time.Time) domain.Outcome {
		if day.Equal(today) {
			return domain.Failed(domain.ErrIncomplete)
		}
		return domain.Found(drawFor(t, day))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(18, 30) })

	draw, err := svc.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !draw.Date.Equal(today.AddDate(0, 0, -1)) {
		t.Errorf("Latest gave %s, want yesterday", domain.FormatVN(draw.Date))
	}
}

// In the morning Latest must not go poking at a draw that has not happened.
func TestLatestDoesNotReachForTodayInTheMorning(t *testing.T) {
	today := domain.NewDate(2026, 8, 21)
	src := &fakeProvider{fallback: func(day time.Time) domain.Outcome {
		if day.Equal(today) {
			t.Error("Latest asked for today at 09:00")
		}
		return domain.Found(drawFor(t, day))
	}}
	svc, _ := newService(t, src, func() time.Time { return at(9, 0) })

	if _, err := svc.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
}
