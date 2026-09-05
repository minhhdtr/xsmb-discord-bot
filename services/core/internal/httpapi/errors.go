package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
)

// The error codes in contracts/openapi.yaml. Clients switch on these; the
// message beside them is for logs and is free to change.
const (
	codeBadRequest = "bad_request"
	codeOutOfRange = "out_of_range"
	codeNotYet     = "not_yet"
	codeNoDraw     = "no_draw"
	codeUpstream   = "upstream"
	codeInternal   = "internal"

	// Only claimAnnouncement uses this, and it is the whole answer there: the
	// caller must not post.
	codeAlreadyClaimed = "already_claimed"

	// codeNotConfigured means the feature is switched off in this deployment,
	// which is not the same as a failure. Flattening it into an error would
	// tell a person "lỗi" when the truth is "chưa bật", and they would go
	// looking for a problem that is not there.
	codeNotConfigured = "not_configured"

	// codeTooShort means the source has too little history to plot. Also not a
	// failure - the request was fine, the data is thin.
	codeTooShort = "too_short"
)

// errorBody is the shape every failure takes.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON sends a value and logs a failure to encode rather than swallowing
// it. By the time encoding fails the status line is already out, so there is
// nothing to tell the client.
func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.log.Error("cannot write response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, errorBody{Code: code, Message: message})
}

// fail maps a domain or service error onto the wire.
//
// The distinction that matters most is between ErrNotYet and ErrNoResult. Both
// mean "no draw here", but one is worth asking about again in twenty seconds
// and the other never will be. Flattening them into one 404 would leave a
// client unable to tell whether to keep polling.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrNotYet):
		s.writeError(w, http.StatusNotFound, codeNotYet, err.Error())

	case errors.Is(err, service.ErrNoResult):
		s.writeError(w, http.StatusNotFound, codeNoDraw, err.Error())

	case errors.Is(err, domain.ErrOutOfRange):
		s.writeError(w, http.StatusBadRequest, codeOutOfRange, err.Error())

	case errors.Is(err, r.Context().Err()) && r.Context().Err() != nil:
		// The client hung up. Nothing to send, and nothing worth logging as an
		// error either.
		return

	default:
		// Anything unrecognised is ours to explain, not the caller's to fix,
		// so it is logged in full here and summarised on the wire.
		s.log.Error("request failed", "path", r.URL.Path, "error", err)
		s.writeError(w, http.StatusBadGateway, codeUpstream,
			"không lấy được dữ liệu từ nguồn")
	}
}

// badRequest is for input this handler could not read at all.
func (s *Server) badRequest(w http.ResponseWriter, message string) {
	s.writeError(w, http.StatusBadRequest, codeBadRequest, message)
}

// internalError is for a failure that is not the caller's business.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal failure", "path", r.URL.Path, "error", err)
	s.writeError(w, http.StatusInternalServerError, codeInternal, "lỗi nội bộ")
}

// errIncompleteBoard means the archive holds a draw without 27 numbers, which
// the write path should make impossible.
var errIncompleteBoard = errors.New("stored draw is not a complete board")
