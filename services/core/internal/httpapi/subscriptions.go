package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// These are the only endpoints that write anything, and they are the reason
// the listener must stay off the host. Until now the API was read-only over
// numbers a public website already publishes, so exposing it would have been
// untidy rather than harmful. It is now possible to silence the daily
// announcement, or to burn a claim so a result is never posted, and that
// changes what an exposed port would cost.

type subscriptionBody struct {
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id,omitempty"`
}

type subscribeResult struct {
	ChannelID string `json:"channel_id"`
	Created   bool   `json:"created"`
}

type unsubscribeResult struct {
	ChannelID string `json:"channel_id"`
	Removed   bool   `json:"removed"`
}

func (s *Server) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.Subscriptions(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]subscriptionBody, 0, len(subs))
	for _, sub := range subs {
		out = append(out, subscriptionBody{ChannelID: sub.ChannelID, GuildID: sub.GuildID})
	}
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channel_id")
	if channelID == "" {
		s.badRequest(w, "thiếu channel_id")
		return
	}

	// The body is optional: a guild id helps an operator read the table, but
	// a subscription without one still works.
	var body subscriptionBody
	if raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<10)); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			s.badRequest(w, "body phải là JSON")
			return
		}
	}

	created, err := s.store.Subscribe(r.Context(), body.GuildID, channelID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, subscribeResult{ChannelID: channelID, Created: created})
}

func (s *Server) unsubscribe(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channel_id")
	if channelID == "" {
		s.badRequest(w, "thiếu channel_id")
		return
	}
	removed, err := s.store.Unsubscribe(r.Context(), channelID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, unsubscribeResult{ChannelID: channelID, Removed: removed})
}

// claimAnnouncement answers 201 when the caller may post and 409 when someone
// already has.
//
// The status code carries the whole answer, which is deliberate: a client that
// treats any non-2xx as failure will stay silent rather than double-post, and
// silence is the safer way to be wrong here.
func (s *Server) claimAnnouncement(w http.ResponseWriter, r *http.Request) {
	day, channelID, ok := s.announcementKey(w, r)
	if !ok {
		return
	}
	won, err := s.store.ClaimAnnouncement(r.Context(), day, channelID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !won {
		s.writeError(w, http.StatusConflict, codeAlreadyClaimed,
			"kỳ này đã được đăng cho kênh đó")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) releaseAnnouncement(w http.ResponseWriter, r *http.Request) {
	day, channelID, ok := s.announcementKey(w, r)
	if !ok {
		return
	}
	if err := s.store.ReleaseAnnouncement(r.Context(), day, channelID); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) announcementKey(w http.ResponseWriter, r *http.Request) (day time.Time, channelID string, ok bool) {
	parsed, good := parseDay(r.PathValue("date"))
	if !good {
		s.badRequest(w, "ngày phải ở dạng YYYY-MM-DD")
		return time.Time{}, "", false
	}
	channelID = r.PathValue("channel_id")
	if channelID == "" {
		s.badRequest(w, "thiếu channel_id")
		return time.Time{}, "", false
	}
	return parsed, channelID, true
}
