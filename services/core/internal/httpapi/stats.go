package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/present"
)

// defaultWindow matches the bot's default so a client that omits `days` and a
// client that sends 30 get the same answer, and the same cache entry.
const defaultWindow = 30

// defaultLimit caps a gan listing.
const defaultLimit = 10

func (s *Server) dayReport(w http.ResponseWriter, r *http.Request) {
	var (
		draw domain.Draw
		err  error
	)
	if raw := r.URL.Query().Get("date"); raw != "" {
		day, ok := parseDay(raw)
		if !ok {
			s.badRequest(w, "ngày phải ở dạng YYYY-MM-DD")
			return
		}
		draw, err = s.svc.Get(r.Context(), day)
	} else {
		draw, err = s.svc.Latest(r.Context())
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	report := draw.Prizes.Report()
	if report.De == "" {
		// A stored draw always has 27 numbers, so this means the archive holds
		// something it should not. Worth an error rather than a blank report.
		s.internalError(w, r, errIncompleteBoard)
		return
	}

	body := dayReportBody{
		Date:      isoDate(draw.Date),
		De:        report.De,
		ChamDau:   report.ChamDau,
		ChamDuoi:  report.ChamDuoi,
		TongDe:    report.TongDe,
		Kep:       emptyIfNil(report.Kep),
		Nhay:      make([]nhayBody, 0, len(report.Nhay)),
		Heads:     report.Heads,
		Tails:     report.Tails,
		MuteHeads: emptyInts(report.MuteHeads),
		MuteTails: emptyInts(report.MuteTails),
		TopHeads:  emptyInts(report.TopHeads),
		TopTails:  emptyInts(report.TopTails),
		Table:     present.DigitCounts(report.Heads, report.Tails),
	}
	for _, n := range report.Nhay {
		body.Nhay = append(body.Nhay, nhayBody{Number: n.Number, Hits: n.Hits})
	}
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) gan(w http.ResponseWriter, r *http.Request) {
	limit, ok := s.intParam(w, r, "limit", defaultLimit, 1, 100)
	if !ok {
		return
	}

	var (
		entries []domain.Gan
		err     error
	)
	switch scope := r.URL.Query().Get("scope"); scope {
	case "", "lo":
		entries, err = s.svc.LoGan(r.Context(), limit)
	case "de":
		entries, err = s.svc.DeGan(r.Context(), limit)
	default:
		s.badRequest(w, "scope chỉ nhận lo hoặc de")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	out := make([]ganBody, 0, len(entries))
	for _, g := range entries {
		out = append(out, toGan(g))
	}
	s.writeJSON(w, http.StatusOK, out)
}

func toGan(g domain.Gan) ganBody {
	return ganBody{
		Number:    g.Number,
		Days:      g.Days,
		Record:    g.Record,
		NewRecord: g.NewRecord(),
		LastSeen:  optionalDate(g.LastSeen),
		RecordEnd: optionalDate(g.RecordEnd),
	}
}

func (s *Server) frequency(w http.ResponseWriter, r *http.Request) {
	days, ok := s.intParam(w, r, "days", defaultWindow, 1, 1<<30)
	if !ok {
		return
	}
	freq, err := s.svc.Frequency(r.Context(), days)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	raw := r.URL.Query().Get("group")
	if raw == "" {
		entries := make([]frequencyEntry, 0, len(freq))
		for _, f := range freq {
			entries = append(entries, frequencyEntry{Number: f.Number, Hits: f.Hits, Days: f.Days})
		}
		s.writeJSON(w, http.StatusOK, frequencyListBody{Days: days, Entries: entries})
		return
	}

	by, err := domain.ParseGrouping(raw)
	if err != nil {
		s.badRequest(w, "group chỉ nhận dau, duoi, tong hoặc cham")
		return
	}
	grouped := domain.GroupFrequency(freq, by)

	buckets := make([]bucketBody, 0, len(grouped.Buckets))
	for _, b := range grouped.Buckets {
		buckets = append(buckets, bucketBody{Digit: b.Digit, Hits: b.Hits})
	}
	s.writeJSON(w, http.StatusOK, groupedFrequencyBody{
		Grouped:  true,
		Days:     days,
		Group:    raw,
		Buckets:  buckets,
		Total:    grouped.Total,
		Even:     grouped.Even,
		Overlaps: by.Overlaps(),
		Table:    present.Buckets(grouped),
	})
}

func (s *Server) numberProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.svc.Profile(r.Context(), r.PathValue("lo"))
	if err != nil {
		// Profile rejects anything that is not two digits, which is the
		// caller's mistake rather than ours.
		s.badRequest(w, err.Error())
		return
	}

	body := profileBody{
		Number:   profile.Number,
		Gan:      toGan(profile.Gan),
		Windows:  make([]windowBody, 0, len(profile.Windows)),
		Recent:   make([]dayHitBody, 0, len(profile.Recent)),
		AvgCycle: profile.AvgCycle,
		First:    optionalDate(profile.First),
		Archive:  profile.Archive,
	}
	for _, win := range profile.Windows {
		body.Windows = append(body.Windows, windowBody{Label: win.Label, Days: win.Days,
			Hits: win.Hits, Draws: win.Draws, Total: win.Total})
	}
	for _, hit := range profile.Recent {
		body.Recent = append(body.Recent, dayHitBody{Date: isoDate(hit.Day), Hits: hit.Hits})
	}
	s.writeJSON(w, http.StatusOK, body)
}

func (s *Server) specialMonth(w http.ResponseWriter, r *http.Request) {
	now := s.svc.Now()
	year, month := now.Year(), now.Month()

	if raw := r.URL.Query().Get("month"); raw != "" {
		parsed, err := time.ParseInLocation("2006-01", raw, domain.Location())
		if err != nil {
			s.badRequest(w, "tháng phải ở dạng YYYY-MM")
			return
		}
		year, month = parsed.Year(), parsed.Month()
	}

	days, err := s.svc.SpecialMonth(r.Context(), year, month)
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	out := make([]specialDayBody, 0, len(days))
	for _, d := range days {
		out = append(out, specialDayBody{Date: isoDate(d.Day), Special: d.Special, De: d.De})
	}
	s.writeJSON(w, http.StatusOK, specialMonthBody{
		Month: fmt.Sprintf("%04d-%02d", year, int(month)),
		Days:  out,
		Table: present.SpecialMonth(days),
	})
}

// intParam reads a bounded integer query parameter, or reports the problem and
// returns false. Out of range is a 400 rather than a silent clamp: a client
// asking for 500 gan entries has a bug, and quietly handing back 100 hides it.
func (s *Server) intParam(w http.ResponseWriter, r *http.Request,
	name string, fallback, min, max int) (int, bool) {

	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		s.badRequest(w, name+" phải là số nguyên")
		return 0, false
	}
	if n < min || n > max {
		s.badRequest(w, fmt.Sprintf("%s phải trong khoảng %d đến %d", name, min, max))
		return 0, false
	}
	return n, true
}

// emptyIfNil and emptyInts keep a nil slice from marshalling as null. A client
// iterating the field should not have to guard against a missing array when
// the honest answer is "none".
func emptyIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func emptyInts(in []int) []int {
	if in == nil {
		return []int{}
	}
	return in
}
