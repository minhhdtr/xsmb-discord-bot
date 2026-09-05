package bot_test

import (
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/coreclient"
	"github.com/minhhdtr/xsmb-discord-bot/internal/httpapi"
	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

// coreOver puts a real core API in front of a service and returns a client
// pointed at it.
//
// Every bot test now runs through HTTP rather than calling the service
// directly. That costs a little speed and buys the thing this step was for:
// the contract is exercised by the whole existing suite, so a field that
// serialises wrongly, an error code mapped to the wrong sentinel, or a date
// that loses its timezone all fail a test that already existed rather than
// waiting to be found by the TypeScript client.
func coreOver(t *testing.T, svc *service.Service, store storage.Store, gold httpapi.Gold) *coreclient.Client {
	t.Helper()
	server := httptest.NewServer(httpapi.New(svc, store, gold, quiet()))
	t.Cleanup(server.Close)
	return coreclient.New(server.URL, 5*time.Second)
}

var _ = slog.Default
