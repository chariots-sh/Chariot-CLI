package demo

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 1, 12, 4, 31, 0, time.UTC)
}

func TestHandlerPrintsReply(t *testing.T) {
	var out strings.Builder
	h := Handler(&out, fixedNow)

	req := httptest.NewRequest("POST", "/chariot", strings.NewReader(
		`{"agent_id":"a-123","message":"Refund of $42 issued.","reply_to":"msg-7"}`))
	req.Header.Set("X-Chariot-Account", "acct-9")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := out.String()
	for _, want := range []string{"[12:04:31]", "agent a-123", "account acct-9", "reply-to msg-7", "Refund of $42 issued."} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestHandlerShowsUnrecognizedBodyRaw(t *testing.T) {
	var out strings.Builder
	h := Handler(&out, fixedNow)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("not json")))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(out.String(), "not json") {
		t.Errorf("raw body not shown:\n%s", out.String())
	}
}

func TestHandlerGetIsFriendly(t *testing.T) {
	var out strings.Builder
	h := Handler(&out, fixedNow)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if out.Len() != 0 {
		t.Errorf("GET should not print a delivery, got:\n%s", out.String())
	}
}
