package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The spec and the router are written by hand, in two files, in two languages.
// Nothing makes them agree on its own, so this compares them.
//
// It reads contracts/openapi.yaml with a regexp rather than a YAML parser on
// purpose: pulling in a YAML dependency to police a ten-line route table would
// cost more than the problem. The regexp only has to find lines that start a
// path and lines that start a method, which is all the shape this needs.
func TestEveryRouteInTheSpecIsServed(t *testing.T) {
	spec, err := os.ReadFile("../../../../contracts/openapi.yaml")
	if err != nil {
		t.Skipf("spec not readable from here: %v", err)
	}

	wanted := specRoutes(t, string(spec))
	if len(wanted) < 5 {
		t.Fatalf("only found %d routes in the spec, the parse is wrong", len(wanted))
	}

	s, _ := newServer(t, found(t))
	for _, route := range wanted {
		// Substitute something plausible for each path parameter. The point is
		// whether the route is registered, not what it answers.
		path := strings.NewReplacer(
			"{date}", "2026-08-20",
			"{lo}", "27",
			"{channel_id}", "chan-1",
			"{code}", "SJC",
		).Replace(route.path)
		if strings.Contains(path, "{") {
			t.Fatalf("%s has a path parameter this test does not know: %s",
				route.path, path)
		}

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(route.method, path, nil))
		if rec.Code == http.StatusNotFound && isRoutingMiss(rec.Body.String()) {
			t.Errorf("%s %s is in the spec but not routed", route.method, route.path)
		}
		if rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s is in the spec but routed under another method",
				route.method, route.path)
		}
	}
}

// isRoutingMiss separates "no such route" from a handler's own 404. The
// handler always answers JSON with a code; the mux answers plain text.
func isRoutingMiss(body string) bool { return !strings.Contains(body, `"code"`) }

type specRoute struct {
	method string
	path   string
}

var (
	pathLine   = regexp.MustCompile(`^  (/\S*):\s*$`)
	methodLine = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
)

func specRoutes(t *testing.T, spec string) []specRoute {
	t.Helper()

	// Only the paths: block, so a path-like string elsewhere in the document
	// cannot be mistaken for a route.
	start := strings.Index(spec, "\npaths:\n")
	if start < 0 {
		t.Fatal("no paths: block")
	}
	body := spec[start:]
	if end := strings.Index(body, "\ncomponents:\n"); end >= 0 {
		body = body[:end]
	}

	var (
		routes  []specRoute
		current string
	)
	for _, line := range strings.Split(body, "\n") {
		if m := pathLine.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := methodLine.FindStringSubmatch(line); m != nil && current != "" {
			routes = append(routes, specRoute{method: strings.ToUpper(m[1]), path: current})
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].path < routes[j].path })
	return routes
}
