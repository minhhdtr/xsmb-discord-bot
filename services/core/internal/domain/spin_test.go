package domain_test

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// counter hands out 0, 1, 2, ... modulo the bound, so every cell is
// predictable and distinct enough to spot a mix-up.
func counter() func(int) int {
	n := 0
	return func(bound int) int {
		n++
		return n % bound
	}
}

func TestNewSpinFillsAValidBoard(t *testing.T) {
	spin := domain.NewSpin(rand.Intn)

	prizes, err := spin.Prizes()
	if err != nil {
		t.Fatalf("a spin produced a board a real draw could not be: %v", err)
	}
	if !prizes.Valid() {
		t.Fatal("board is not valid")
	}
	// Widths must match the layout, or the columns break and the tails come
	// out wrong.
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			if got := len(prizes.Numbers()[at]); got != spec.Digits {
				t.Errorf("%s[%d] has %d digits, want %d", spec.Code, n, got, spec.Digits)
			}
			at++
		}
	}
}

// Leading zeros are real prize numbers, so the formatting must keep them.
func TestNewSpinKeepsLeadingZeros(t *testing.T) {
	spin := domain.NewSpin(func(int) int { return 3 })
	at := 0
	for _, spec := range domain.PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			cell := spin.Board(spin.Steps())[at]
			want := strings.Repeat("0", spec.Digits-1) + "3"
			if cell != want {
				t.Errorf("%s[%d] = %q, want %q", spec.Code, n, cell, want)
			}
			at++
		}
	}
}

// The special is stored first but spun last, which is the whole point of the
// command.
func TestSpinRevealsTheSpecialLast(t *testing.T) {
	spin := domain.NewSpin(counter())

	if spin.Steps() != domain.TotalNumbers {
		t.Fatalf("Steps() = %d, want %d", spin.Steps(), domain.TotalNumbers)
	}

	// One short of the end: everything but the special is showing.
	nearly := spin.Board(spin.Steps() - 1)
	if nearly[0] != "" {
		t.Errorf("the special showed early: %q", nearly[0])
	}
	for i, cell := range nearly[1:] {
		if cell == "" {
			t.Errorf("cell %d is still blank one step from the end", i+1)
		}
	}

	last, ok := spin.Just(spin.Steps())
	if !ok || last != 0 {
		t.Errorf("last reveal = %d (%v), want index 0", last, ok)
	}
}

// The first prize goes first, then the second, and so on down the board.
func TestSpinRevealsInBoardOrder(t *testing.T) {
	spin := domain.NewSpin(counter())
	for step := 1; step <= domain.TotalNumbers-1; step++ {
		at, ok := spin.Just(step)
		if !ok {
			t.Fatalf("step %d has no reveal", step)
		}
		if at != step {
			t.Fatalf("step %d revealed index %d, want %d", step, at, step)
		}
	}
}

func TestSpinBoardClampsAndGrows(t *testing.T) {
	spin := domain.NewSpin(counter())

	for _, cell := range spin.Board(0) {
		if cell != "" {
			t.Fatalf("Board(0) is not blank: %q", cell)
		}
	}
	for _, cell := range spin.Board(spin.Steps()) {
		if cell == "" {
			t.Fatal("Board(Steps()) still has a blank cell")
		}
	}
	// Out of range on both ends behaves like the nearest end.
	if got := spin.Board(-5); got[0] != "" {
		t.Errorf("Board(-5) revealed something: %q", got[0])
	}
	if got := spin.Board(999); got[0] == "" {
		t.Error("Board(999) is not the finished board")
	}

	// Each step reveals exactly one more cell.
	previous := 0
	for step := 0; step <= spin.Steps(); step++ {
		shown := 0
		for _, cell := range spin.Board(step) {
			if cell != "" {
				shown++
			}
		}
		if shown != step {
			t.Fatalf("Board(%d) shows %d cells", step, shown)
		}
		previous = shown
	}
	if previous != domain.TotalNumbers {
		t.Fatalf("finished board shows %d cells", previous)
	}
}

func TestSpinJustRejectsStepsOutOfRange(t *testing.T) {
	spin := domain.NewSpin(counter())
	for _, step := range []int{0, -1, domain.TotalNumbers + 1} {
		if _, ok := spin.Just(step); ok {
			t.Errorf("Just(%d) reported a reveal", step)
		}
	}
}
