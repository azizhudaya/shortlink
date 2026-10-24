package alert

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookPostsPlainText(t *testing.T) {
	var got, ctype string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, ctype = string(b), r.Header.Get("Content-Type")
	}))
	defer srv.Close()

	if err := NewWebhook(srv.URL, &http.Client{Timeout: time.Second}).Notify(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if got != "hello" || ctype != "text/plain; charset=utf-8" {
		t.Fatalf("body %q, content-type %q", got, ctype)
	}
}

func TestWebhookErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	if err := NewWebhook(srv.URL, http.DefaultClient).Notify(context.Background(), "x"); err == nil {
		t.Fatal("500 not reported")
	}
}

func TestNop(t *testing.T) {
	if err := (Nop{}).Notify(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}
