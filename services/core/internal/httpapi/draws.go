package httpapi

import (
	"math/rand"
	"net/http"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/present"
)

// isoDate is the wire form for a day. Distinct from domain.FormatVN, which is
// what a person types and reads.
func isoDate(day time.Time) string { return domain.FormatISO(day) }

// optionalDate renders a possibly-zero day, since a draw that has never been
// seen has no date to report.
func optionalDate(day time.Time) *string {
	if day.IsZero() {
		return nil
	}
	s := isoDate(day)
	return &s
}

func optionalTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.In(domain.Location()).Format(time.RFC3339)
	return &s
}

// parseDay reads a date from the wire. Only the ISO form: the Vietnamese forms
// are what people type, and turning those into a date is the client's job.
func parseDay(raw string) (time.Time, bool) {
	day, err := time.ParseInLocation("2006-01-02", raw, domain.Location())
	if err != nil {
		return time.Time{}, false
	}
	return domain.DayOf(day), true
}

func (s *Server) draw(w http.ResponseWriter, r *http.Request) {
	day, ok := parseDay(r.PathValue("date"))
	if !ok {
		s.badRequest(w, "ngày phải ở dạng YYYY-MM-DD")
		return
	}
	found, err := s.svc.Get(r.Context(), day)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, toDraw(found))
}

func (s *Server) latestDraw(w http.ResponseWriter, r *http.Request) {
	found, err := s.svc.Latest(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, toDraw(found))
}

func toDraw(d domain.Draw) drawBody {
	return drawBody{
		Date:      isoDate(d.Date),
		Weekday:   domain.WeekdayVN(d.Date),
		Special:   d.Prizes.Special(),
		De:        d.Prizes.De(),
		Numbers:   d.Prizes.Numbers(),
		Tails:     d.Prizes.Tails(),
		Table:     present.Table(d.Prizes),
		HeadTail:  present.HeadTail(d.Prizes),
		Source:    d.Source,
		FetchedAt: optionalTime(d.FetchedAt),
	}
}

// spin invents a board. It never reaches the service, and so can never reach
// the archive: keeping a made-up draw and a real one apart is easier to
// guarantee by structure than to remember.
func (s *Server) spin(w http.ResponseWriter, r *http.Request) {
	made := domain.NewSpin(rand.Intn)

	numbers := made.Board(made.Steps())
	order := make([]int, 0, made.Steps())
	for step := 1; step <= made.Steps(); step++ {
		if at, ok := made.Just(step); ok {
			order = append(order, at)
		}
	}
	s.writeJSON(w, http.StatusCreated, spinBody{Numbers: numbers, Order: order})
}

func (s *Server) archive(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.Stats(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, archiveBody{
		Draws:    stats.Draws,
		Absences: stats.Absences,
		Channels: stats.Channels,
		Earliest: optionalDate(stats.Earliest),
		Latest:   optionalDate(stats.Latest),
	})
}
