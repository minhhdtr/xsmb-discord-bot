package domain_test

import (
	"reflect"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// reportPrizes builds a draw whose 27 tails are known, so every count in the
// report can be checked by hand. tails must hold exactly 27 two-digit strings.
func reportPrizes(t *testing.T, tails []string) domain.Prizes {
	t.Helper()
	if len(tails) != domain.TotalNumbers {
		t.Fatalf("need %d tails, got %d", domain.TotalNumbers, len(tails))
	}
	numbers := make([]string, 0, domain.TotalNumbers)
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			// Pad the front so the tail survives at the requested width.
			padded := tails[at]
			for len(padded) < spec.Digits {
				padded = "1" + padded
			}
			numbers = append(numbers, padded)
			at++
		}
	}
	prizes, err := domain.NewPrizes(numbers)
	if err != nil {
		t.Fatal(err)
	}
	return prizes
}

func TestReportReadsOneDraw(t *testing.T) {
	// Special is first: đề 47. 33 lands three times, 12 twice, 88 once.
	tails := []string{
		"47", "33", "33", "33", "12", "12", "88", "05", "16",
		"21", "34", "45", "56", "67", "78", "89", "90", "01",
		"23", "36", "49", "52", "65", "70", "81", "94", "07",
	}
	got := reportPrizes(t, tails).Report()

	if got.De != "47" {
		t.Errorf("De = %q, want 47", got.De)
	}
	if got.ChamDau != 4 || got.ChamDuoi != 7 {
		t.Errorf("chạm = %d/%d, want 4/7", got.ChamDau, got.ChamDuoi)
	}
	if got.TongDe != 1 { // 4+7 = 11
		t.Errorf("TongDe = %d, want 1", got.TongDe)
	}

	// 33 is kép and appears three times, but is listed as kép only once.
	if want := []string{"33", "88"}; !reflect.DeepEqual(got.Kep, want) {
		t.Errorf("Kep = %v, want %v", got.Kep, want)
	}
	wantNhay := []domain.Nhay{{Number: "12", Hits: 2}, {Number: "33", Hits: 3}}
	if !reflect.DeepEqual(got.Nhay, wantNhay) {
		t.Errorf("Nhay = %v, want %v", got.Nhay, wantNhay)
	}

	// Heads: 0 -> 05 01 07, 3 -> 33 33 33 34 36.
	if got.Heads[0] != 3 {
		t.Errorf("Heads[0] = %d, want 3", got.Heads[0])
	}
	if got.Heads[3] != 5 {
		t.Errorf("Heads[3] = %d, want 5", got.Heads[3])
	}
	if want := []int{3}; !reflect.DeepEqual(got.TopHeads, want) {
		t.Errorf("TopHeads = %v, want %v", got.TopHeads, want)
	}
	if len(got.MuteHeads) != 0 {
		t.Errorf("MuteHeads = %v, want none", got.MuteHeads)
	}

	total := 0
	for _, n := range got.Heads {
		total += n
	}
	if total != domain.TotalNumbers {
		t.Errorf("heads sum to %d, want %d", total, domain.TotalNumbers)
	}
}

func TestReportFindsMuteDigits(t *testing.T) {
	// Every tail sits in the 10s or 20s and ends in 0-6, so eight heads and
	// three units are câm.
	tails := []string{
		"10", "11", "12", "13", "14", "15", "16",
		"20", "21", "22", "23", "24", "25", "26",
		"10", "11", "12", "13", "14", "15", "16",
		"20", "21", "22", "23", "24", "25",
	}
	got := reportPrizes(t, tails).Report()

	if want := []int{0, 3, 4, 5, 6, 7, 8, 9}; !reflect.DeepEqual(got.MuteHeads, want) {
		t.Errorf("MuteHeads = %v, want %v", got.MuteHeads, want)
	}
	if want := []int{7, 8, 9}; !reflect.DeepEqual(got.MuteTails, want) {
		t.Errorf("MuteTails = %v, want %v", got.MuteTails, want)
	}
}

func TestReportOfInvalidPrizesIsZero(t *testing.T) {
	if got := (domain.Prizes{}).Report(); !reflect.DeepEqual(got, domain.DayReport{}) {
		t.Errorf("Report() = %+v, want zero", got)
	}
}
