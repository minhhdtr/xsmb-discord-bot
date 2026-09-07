package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/minhhdtr/xsmb-discord-bot/internal/chart"
	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
)

// Gold is the part of the gold service this API exposes. Narrow on purpose:
// core is the only thing that should be scraping a price site, and a client
// should get prices the same way it gets draws.
type Gold interface {
	Board(ctx context.Context) (domain.GoldBoard, error)
	History(ctx context.Context, code string, days int) (domain.GoldSeries, error)
}

type goldQuoteBody struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Buy        float64 `json:"buy"`
	Sell       float64 `json:"sell"`
	ChangeBuy  float64 `json:"change_buy"`
	ChangeSell float64 `json:"change_sell"`
	Currency   string  `json:"currency"`
}

type goldBoardBody struct {
	Quotes    []goldQuoteBody `json:"quotes"`
	UpdatedAt *string         `json:"updated_at"`
	FetchedAt *string         `json:"fetched_at"`
	Source    string          `json:"source"`
}

type goldPointBody struct {
	Date string  `json:"date"`
	Buy  float64 `json:"buy"`
	Sell float64 `json:"sell"`
}

type goldSeriesBody struct {
	Code     string          `json:"code"`
	Name     string          `json:"name"`
	Currency string          `json:"currency"`
	Points   []goldPointBody `json:"points"`
}

func (s *Server) goldBoard(w http.ResponseWriter, r *http.Request) {
	if s.gold == nil {
		s.writeError(w, http.StatusNotFound, codeNotConfigured, "giá vàng chưa được bật")
		return
	}
	board, err := s.gold.Board(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	quotes := make([]goldQuoteBody, 0, len(board.Quotes()))
	for _, q := range board.Quotes() {
		quotes = append(quotes, goldQuoteBody{
			Code: q.Code, Name: q.Name, Buy: q.Buy, Sell: q.Sell,
			ChangeBuy: q.ChangeBuy, ChangeSell: q.ChangeSell,
			Currency: string(q.Currency),
		})
	}
	s.writeJSON(w, http.StatusOK, goldBoardBody{
		Quotes:    quotes,
		UpdatedAt: optionalTime(board.UpdatedAt),
		FetchedAt: optionalTime(board.FetchedAt),
		Source:    board.Source,
	})
}

func (s *Server) goldHistory(w http.ResponseWriter, r *http.Request) {
	if s.gold == nil {
		s.writeError(w, http.StatusNotFound, codeNotConfigured, "giá vàng chưa được bật")
		return
	}
	days, ok := s.intParam(w, r, "days", domain.MaxHistoryDays, 2, domain.MaxHistoryDays)
	if !ok {
		return
	}
	code := strings.ToUpper(r.PathValue("code"))

	series, err := s.gold.History(r.Context(), code, days)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	points := make([]goldPointBody, 0, len(series.Points()))
	for _, p := range series.Points() {
		points = append(points, goldPointBody{
			Date: isoDate(p.Day), Buy: p.Buy, Sell: p.Sell,
		})
	}
	s.writeJSON(w, http.StatusOK, goldSeriesBody{
		Code: series.Code, Name: series.Name,
		Currency: string(series.Currency), Points: points,
	})
}

// goldChart answers with an image rather than JSON, the one endpoint that
// does. Drawing lives here because the code already exists, is identical for
// every client, and a chart that looks different on each platform is the sort
// of bug nobody thinks to look for.
func (s *Server) goldChart(w http.ResponseWriter, r *http.Request) {
	if s.gold == nil {
		s.writeError(w, http.StatusNotFound, codeNotConfigured, "giá vàng chưa được bật")
		return
	}
	days, ok := s.intParam(w, r, "days", domain.MaxHistoryDays, 2, domain.MaxHistoryDays)
	if !ok {
		return
	}
	code := strings.ToUpper(r.PathValue("code"))

	series, err := s.gold.History(r.Context(), code, days)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if series.Len() < 2 {
		// Not a failure: the source simply has too little history for this
		// code. A client says so rather than showing an error.
		s.writeError(w, http.StatusUnprocessableEntity, codeTooShort,
			"chưa đủ dữ liệu để vẽ biểu đồ")
		return
	}

	png, err := chart.Render(series)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if _, err := w.Write(png); err != nil {
		s.log.Error("cannot write chart", "error", err)
	}
}
