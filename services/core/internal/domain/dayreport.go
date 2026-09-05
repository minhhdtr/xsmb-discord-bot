package domain

// DayReport is what a person reads out of the đầu đuôi table by eye: which
// numbers doubled up, which heads came up empty, what the special prize
// touches. Everything here is a count of one draw that has already happened.
type DayReport struct {
	De       string // two-digit tail of the special prize
	ChamDau  int    // its tens digit
	ChamDuoi int    // its units digit
	TongDe   int    // the two digits added, last digit kept

	Kep  []string // lô with two identical digits, each listed once
	Nhay []Nhay   // lô that landed more than once

	Heads [10]int // lô per first digit
	Tails [10]int // lô per last digit

	MuteHeads []int // first digits with no lô at all
	MuteTails []int
	TopHeads  []int // first digits with the most lô; ties all listed
	TopTails  []int
}

// Nhay is one number that landed more than once in a single draw. Two hits is
// "hai nháy", three is "ba nháy".
type Nhay struct {
	Number string
	Hits   int
}

// Report reads one draw. The zero DayReport is returned for an invalid Prizes,
// so callers can check Valid once rather than at every field.
func (p Prizes) Report() DayReport {
	if !p.Valid() {
		return DayReport{}
	}

	var (
		out   DayReport
		count = make(map[string]int, TotalNumbers)
	)

	out.De = p.De()
	out.ChamDau = int(out.De[0] - '0')
	out.ChamDuoi = int(out.De[1] - '0')
	out.TongDe = (out.ChamDau + out.ChamDuoi) % 10

	// Tails() is sorted, so kép and nháy come out in order without a second
	// pass to sort them.
	for _, tail := range p.Tails() {
		head, unit := int(tail[0]-'0'), int(tail[1]-'0')
		out.Heads[head]++
		out.Tails[unit]++
		if count[tail]++; count[tail] == 1 && head == unit {
			out.Kep = append(out.Kep, tail)
		}
	}
	for _, tail := range p.Tails() {
		if hits := count[tail]; hits > 1 {
			out.Nhay = append(out.Nhay, Nhay{Number: tail, Hits: hits})
			count[tail] = 1 // already reported; don't repeat it per hit
		}
	}

	out.MuteHeads, out.TopHeads = mutesAndPeaks(out.Heads)
	out.MuteTails, out.TopTails = mutesAndPeaks(out.Tails)
	return out
}

// mutesAndPeaks finds the empty digits and the busiest ones. Ties are all
// returned, since with 27 lô over 10 digits they are common.
func mutesAndPeaks(counts [10]int) (mute, peak []int) {
	best := 0
	for _, n := range counts {
		if n > best {
			best = n
		}
	}
	for digit, n := range counts {
		switch {
		case n == 0:
			mute = append(mute, digit)
		case n == best:
			peak = append(peak, digit)
		}
	}
	return mute, peak
}
