package domain_test

import (
	"errors"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

func TestParseGrouping(t *testing.T) {
	for _, in := range []string{"dau", "Đầu", " DAU "} {
		got, err := domain.ParseGrouping(in)
		if err != nil || got != domain.ByHead {
			t.Errorf("ParseGrouping(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := domain.ParseGrouping("xyz"); !errors.Is(err, domain.ErrBadGrouping) {
		t.Errorf("ParseGrouping(\"xyz\") error = %v", err)
	}
}

func TestGroupFrequencySplitsWithoutOverlap(t *testing.T) {
	freq := []domain.Frequency{
		{Number: "12", Hits: 5},
		{Number: "17", Hits: 3},
		{Number: "42", Hits: 2},
	}

	head := domain.GroupFrequency(freq, domain.ByHead)
	if head.Buckets[1].Hits != 8 || head.Buckets[4].Hits != 2 {
		t.Errorf("đầu = %d/%d, want 8/2", head.Buckets[1].Hits, head.Buckets[4].Hits)
	}
	if head.Total != 10 {
		t.Errorf("Total = %d, want 10 (every lô counted once)", head.Total)
	}
	if head.Even != 1 {
		t.Errorf("Even = %v, want 1", head.Even)
	}

	tail := domain.GroupFrequency(freq, domain.ByTail)
	if tail.Buckets[2].Hits != 7 || tail.Buckets[7].Hits != 3 {
		t.Errorf("đuôi = %d/%d, want 7/3", tail.Buckets[2].Hits, tail.Buckets[7].Hits)
	}

	// 1+2 = 3, 1+7 = 8, 4+2 = 6.
	sum := domain.GroupFrequency(freq, domain.BySum)
	if sum.Buckets[3].Hits != 5 || sum.Buckets[8].Hits != 3 || sum.Buckets[6].Hits != 2 {
		t.Errorf("tổng buckets = %+v", sum.Buckets)
	}
	if sum.Total != 10 {
		t.Errorf("tổng Total = %d, want 10", sum.Total)
	}
}

// The sum wraps: 9+5 = 14, which is tổng 4, not tổng 14.
func TestGroupFrequencySumWraps(t *testing.T) {
	got := domain.GroupFrequency([]domain.Frequency{{Number: "95", Hits: 1}}, domain.BySum)
	if got.Buckets[4].Hits != 1 {
		t.Errorf("tổng of 95 landed in %+v, want bucket 4", got.Buckets)
	}
}

// Chạm is the one grouping where a lô lands in two buckets, and a kép in one.
func TestGroupFrequencyTouchCountsBothDigitsOnce(t *testing.T) {
	freq := []domain.Frequency{
		{Number: "87", Hits: 4}, // touches 8 and 7
		{Number: "88", Hits: 3}, // touches 8, once
	}
	got := domain.GroupFrequency(freq, domain.ByTouch)

	if got.Buckets[8].Hits != 7 {
		t.Errorf("chạm 8 = %d, want 7", got.Buckets[8].Hits)
	}
	if got.Buckets[7].Hits != 4 {
		t.Errorf("chạm 7 = %d, want 4", got.Buckets[7].Hits)
	}
	// 4 lô counted twice plus 3 counted once.
	if got.Total != 11 {
		t.Errorf("Total = %d, want 11", got.Total)
	}
	if !got.By.Overlaps() {
		t.Error("ByTouch should report that it overlaps")
	}
	if domain.ByHead.Overlaps() {
		t.Error("ByHead should not overlap")
	}
}

func TestGroupFrequencyPeak(t *testing.T) {
	got := domain.GroupFrequency([]domain.Frequency{
		{Number: "12", Hits: 5}, {Number: "42", Hits: 9},
	}, domain.ByTail)
	if got.Peak() != 14 {
		t.Errorf("Peak = %d, want 14", got.Peak())
	}
}
