package domain

import (
	"errors"
	"fmt"
	"time"
)

// Currency distinguishes the two kinds of row a gold board carries.
type Currency string

const (
	// VND is a domestic dealer quote, in đồng per lượng, with both sides.
	VND Currency = "VND"
	// USD is the world spot price, in USD per ounce, quoted one-sided.
	USD Currency = "USD"
)

// ErrNoQuotes means a board carried nothing usable.
var ErrNoQuotes = errors.New("gold board has no usable quotes")

// GoldQuote is a dealer's buy/sell pair, or the world spot price, which has
// no sell side. The two shapes are validated differently.
type GoldQuote struct {
	Code       string
	Name       string
	Buy        float64
	Sell       float64 // zero when the source does not quote this side
	ChangeBuy  float64
	ChangeSell float64
	Currency   Currency
}

// NewGoldQuote validates one row.
func NewGoldQuote(q GoldQuote) (GoldQuote, error) {
	if q.Code == "" {
		return GoldQuote{}, errors.New("quote has no code")
	}
	if q.Buy <= 0 {
		return GoldQuote{}, fmt.Errorf("%s: buy price is %v", q.Code, q.Buy)
	}
	switch q.Currency {
	case VND:
		if q.Sell <= 0 {
			return GoldQuote{}, fmt.Errorf("%s: domestic quote has no sell price", q.Code)
		}
		if q.Sell < q.Buy {
			// Reversed columns mean the upstream shape changed.
			return GoldQuote{}, fmt.Errorf("%s: sell %v is below buy %v", q.Code, q.Sell, q.Buy)
		}
	case USD:
		if q.Sell < 0 {
			return GoldQuote{}, fmt.Errorf("%s: sell price is %v", q.Code, q.Sell)
		}
	default:
		return GoldQuote{}, fmt.Errorf("%s: unknown currency %q", q.Code, q.Currency)
	}
	if q.Name == "" {
		q.Name = q.Code
	}
	return q, nil
}

// TwoSided reports whether the quote carries a sell price worth rendering.
func (q GoldQuote) TwoSided() bool { return q.Sell > 0 }

// Spread is the dealer's margin. Zero for one-sided quotes.
func (q GoldQuote) Spread() float64 {
	if !q.TwoSided() {
		return 0
	}
	return q.Sell - q.Buy
}

// Domestic reports whether this is a đồng-denominated dealer quote.
func (q GoldQuote) Domestic() bool { return q.Currency == VND }

// GoldBoard is one snapshot of the market.
type GoldBoard struct {
	quotes    []GoldQuote
	UpdatedAt time.Time // when the source says the prices were set
	Source    string
	FetchedAt time.Time // when we asked
}

// NewGoldBoard validates and freezes a board. Order is preserved: the
// upstream payload is a JSON object and Go iterates maps at random.
func NewGoldBoard(quotes []GoldQuote, updated time.Time, source string, fetched time.Time) (GoldBoard, error) {
	if len(quotes) == 0 {
		return GoldBoard{}, ErrNoQuotes
	}
	frozen := make([]GoldQuote, 0, len(quotes))
	for _, q := range quotes {
		valid, err := NewGoldQuote(q)
		if err != nil {
			return GoldBoard{}, err
		}
		frozen = append(frozen, valid)
	}
	return GoldBoard{quotes: frozen, UpdatedAt: updated, Source: source, FetchedAt: fetched}, nil
}

// Valid reports whether the board came from the constructor.
func (b GoldBoard) Valid() bool { return len(b.quotes) > 0 }

// Quotes returns every row, in display order, as a copy.
func (b GoldBoard) Quotes() []GoldQuote {
	out := make([]GoldQuote, len(b.quotes))
	copy(out, b.quotes)
	return out
}

// Domestic returns only the đồng-denominated dealer rows.
func (b GoldBoard) Domestic() []GoldQuote {
	out := make([]GoldQuote, 0, len(b.quotes))
	for _, q := range b.quotes {
		if q.Domestic() {
			out = append(out, q)
		}
	}
	return out
}

// World returns the world spot row, if the board carries one.
func (b GoldBoard) World() (GoldQuote, bool) {
	for _, q := range b.quotes {
		if q.Currency == USD {
			return q, true
		}
	}
	return GoldQuote{}, false
}

// Age is how long ago the source set these prices.
func (b GoldBoard) Age(now time.Time) time.Duration {
	if b.UpdatedAt.IsZero() {
		return 0
	}
	return now.Sub(b.UpdatedAt)
}
