package adguard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/kenlasko/adguard-log-aggregator/internal/config"
)

// captureServer records the last request it received for assertion.
type captureServer struct {
	lastPath  string
	lastQuery url.Values
	lastAuth  string
	status    int
	body      string
}

func newCapture(status int, body string) (*captureServer, *httptest.Server) {
	cs := &captureServer{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.lastPath = r.URL.Path
		cs.lastQuery = r.URL.Query()
		cs.lastAuth = r.Header.Get("Authorization")
		w.WriteHeader(cs.status)
		_, _ = w.Write([]byte(cs.body))
	}))
	return cs, srv
}

func testClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	return New(config.Instance{
		Name:     "dns1",
		URL:      baseURL,
		Username: "admin",
		Password: "s3cret",
	}, &http.Client{Timeout: 2 * time.Second})
}

func TestGetSetsBasicAuth(t *testing.T) {
	cs, srv := newCapture(http.StatusOK, `{"oldest":"","data":[]}`)
	defer srv.Close()
	c := testClient(t, srv.URL)

	if _, err := c.QueryLog(context.Background(), QueryLogParams{}); err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if cs.lastAuth == "" {
		t.Fatal("expected Authorization header")
	}
	// Basic YWRtaW46czNjcmV0 == admin:s3cret
	if cs.lastAuth != "Basic YWRtaW46czNjcmV0" {
		t.Errorf("unexpected auth header: %q", cs.lastAuth)
	}
}

func TestQueryLogParamEncoding(t *testing.T) {
	cs, srv := newCapture(http.StatusOK, `{"oldest":"","data":[]}`)
	defer srv.Close()
	c := testClient(t, srv.URL)

	_, err := c.QueryLog(context.Background(), QueryLogParams{
		OlderThan:      "2026-07-10T00:00:00Z",
		Limit:          50,
		Search:         "ads",
		ResponseStatus: "blocked",
	})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	q := cs.lastQuery
	if q.Get("older_than") != "2026-07-10T00:00:00Z" {
		t.Errorf("older_than = %q", q.Get("older_than"))
	}
	if q.Get("limit") != "50" {
		t.Errorf("limit = %q", q.Get("limit"))
	}
	if q.Get("search") != "ads" {
		t.Errorf("search = %q", q.Get("search"))
	}
	if q.Get("response_status") != "blocked" {
		t.Errorf("response_status = %q", q.Get("response_status"))
	}
}

func TestQueryLogOmitsZeroParams(t *testing.T) {
	cs, srv := newCapture(http.StatusOK, `{"oldest":"","data":[]}`)
	defer srv.Close()
	c := testClient(t, srv.URL)

	if _, err := c.QueryLog(context.Background(), QueryLogParams{}); err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	for _, key := range []string{"older_than", "limit", "search", "response_status"} {
		if cs.lastQuery.Has(key) {
			t.Errorf("param %q should be omitted when zero", key)
		}
	}
}

func TestNon200BecomesTypedError(t *testing.T) {
	_, srv := newCapture(http.StatusInternalServerError, "kaboom")
	defer srv.Close()
	c := testClient(t, srv.URL)

	_, err := c.Status(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Errorf("status = %d", apiErr.Status)
	}
	if apiErr.Instance != "dns1" {
		t.Errorf("instance = %q", apiErr.Instance)
	}
}

func TestQueryLogPreservesRawTimeAndParses(t *testing.T) {
	raw := "2026-07-10T12:34:56.123456789Z"
	body := `{"oldest":"","data":[{"time":"` + raw + `","client":"1.2.3.4","question":{"name":"x.com","type":"A"},"reason":"NotFilteredNotFound","rules":[]}]}`
	_, srv := newCapture(http.StatusOK, body)
	defer srv.Close()
	c := testClient(t, srv.URL)

	resp, err := c.QueryLog(context.Background(), QueryLogParams{})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Data))
	}
	item := resp.Data[0]
	if item.RawTime != raw {
		t.Errorf("RawTime = %q, want byte-for-byte %q", item.RawTime, raw)
	}
	if !item.ParsedTime.Equal(mustParse(t, raw)) {
		t.Errorf("ParsedTime = %v", item.ParsedTime)
	}
	if item.Instance != "dns1" {
		t.Errorf("Instance = %q, want dns1", item.Instance)
	}
}

func TestQueryLogHandlesNullClientInfo(t *testing.T) {
	body := `{"oldest":"","data":[{"time":"2026-07-10T00:00:00Z","client":"1.2.3.4","client_info":null,"question":{"name":"x.com","type":"A"},"reason":"x","rules":[]}]}`
	_, srv := newCapture(http.StatusOK, body)
	defer srv.Close()
	c := testClient(t, srv.URL)

	resp, err := c.QueryLog(context.Background(), QueryLogParams{})
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if resp.Data[0].ClientInfo != nil {
		t.Errorf("expected nil ClientInfo, got %+v", resp.Data[0].ClientInfo)
	}
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tm
}
