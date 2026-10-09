package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestWithNoSourcesErrors(t *testing.T) {
	_, err := Checker{Client: &http.Client{}}.Latest(context.Background())
	if err == nil {
		t.Fatal("Latest with no sources returned a nil error")
	}
}

func TestGetClassifiesStatusAsUnreachable(t *testing.T) {
	for _, tt := range []struct {
		status      int
		unreachable bool
	}{
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusNotFound, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tt.status)
		}))
		body, err := Checker{Client: &http.Client{}}.get(context.Background(), srv.URL)
		srv.Close()
		if body != nil {
			_ = body.Close()
		}
		if err == nil {
			t.Fatalf("status %d: get returned no error", tt.status)
		}
		if got := errors.Is(err, errUnreachable); got != tt.unreachable {
			t.Errorf("status %d: errors.Is(err, errUnreachable) = %v, want %v", tt.status, got, tt.unreachable)
		}
	}
}
