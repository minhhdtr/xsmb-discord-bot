package domain

import "fmt"

// Spin is a made-up draw for the quay thử command: 27 numbers with no source
// and no date, revealed one at a time. It is deliberately not a Draw. A Draw
// is something that happened; nothing here happened.
type Spin struct {
	numbers []string // TotalNumbers, in PrizeLayout order
	order   []int    // indices into numbers, in the order they are revealed
}

// spinOrder lists the flat indices of PrizeLayout in the order a board is
// spun: first prize through seventh, then the special last.
//
// The stored order is the other way round, special first, because that is how
// a finished board is read. Deriving this from PrizeLayout rather than writing
// the indices out means a change to the layout cannot leave the two disagreeing.
func spinOrder() []int {
	order := make([]int, 0, TotalNumbers)
	special := make([]int, 0, 1)

	at := 0
	for _, spec := range PrizeLayout {
		for n := 0; n < spec.Count; n++ {
			if spec.Code == "special" {
				special = append(special, at)
			} else {
				order = append(order, at)
			}
			at++
		}
	}
	return append(order, special...)
}

// NewSpin draws a board. intn must behave like math/rand's Intn: a
// non-negative int below the bound it is given. Passing it in keeps the
// generator out of the domain and lets a test fix the numbers.
func NewSpin(intn func(int) int) Spin {
	numbers := make([]string, 0, TotalNumbers)
	for _, spec := range PrizeLayout {
		bound := 1
		for d := 0; d < spec.Digits; d++ {
			bound *= 10
		}
		for n := 0; n < spec.Count; n++ {
			// Leading zeros are ordinary: 02003 is a real prize number.
			numbers = append(numbers, fmt.Sprintf("%0*d", spec.Digits, intn(bound)))
		}
	}
	return Spin{numbers: numbers, order: spinOrder()}
}

// Steps is how many reveals a full spin takes.
func (s Spin) Steps() int { return len(s.order) }

// Board returns the 27 cells after n reveals, in PrizeLayout order. A cell not
// yet drawn is empty, which is what the caller draws a placeholder for.
//
// n is clamped, so Board(0) is a blank board and Board(Steps()) is the
// finished one.
func (s Spin) Board(n int) []string {
	cells := make([]string, len(s.numbers))
	if n < 0 {
		n = 0
	}
	if n > len(s.order) {
		n = len(s.order)
	}
	for _, at := range s.order[:n] {
		cells[at] = s.numbers[at]
	}
	return cells
}

// Just returns the index revealed by step n, counting from 1, and whether
// there was one. Callers use it to point at the number that just landed.
func (s Spin) Just(n int) (int, bool) {
	if n < 1 || n > len(s.order) {
		return 0, false
	}
	return s.order[n-1], true
}

// Prizes is the finished board. The error is the same validation a real draw
// goes through, so a generator bug cannot produce something a real board could
// never be.
func (s Spin) Prizes() (Prizes, error) { return NewPrizes(s.numbers) }

// SpinFrom rebuilds a spin from the wire. A client receives the numbers and
// the reveal order and needs the same Board and Just behaviour as the side
// that generated them; without this it would have to reimplement the slicing,
// which is exactly the sort of duplicated rule that drifts.
func SpinFrom(numbers []string, order []int) Spin {
	return Spin{numbers: append([]string(nil), numbers...),
		order: append([]int(nil), order...)}
}
