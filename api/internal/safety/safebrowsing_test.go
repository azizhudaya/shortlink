package safety

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testKey = "AIzaTEST-secret-key"

func serve(t *testing.T, h http.HandlerFunc) *SafeBrowsing {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewSafeBrowsing(srv.URL+"/v4/threatMatches:find", testKey, &http.Client{Timeout: 100 * time.Millisecond})
}

func TestCheckClean(t *testing.T) {
	sb := serve(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "{}") })
	if err := sb.Check(context.Background(), "https://example.com/"); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestCheckMatch(t *testing.T) {
	sb := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"matches":[{"threatType":"SOCIAL_ENGINEERING","threat":{"url":"https://bad.example/"}}]}`)
	})
	if err := sb.Check(context.Background(), "https://bad.example/"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("err = %v, want ErrUnsafe", err)
	}
}

func TestCheckUnavailable(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
		"timeout": func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(300 * time.Millisecond)
			io.WriteString(w, "{}")
		},
		"not json": func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "<html>") },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			err := serve(t, h).Check(context.Background(), "https://example.com/")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Fatalf("error leaks the API key: %v", err)
			}
		})
	}
}

func TestCheckRequestShape(t *testing.T) {
	var gotKey string
	var body struct {
		Client     map[string]string `json:"client"`
		ThreatInfo struct {
			ThreatTypes      []string            `json:"threatTypes"`
			PlatformTypes    []string            `json:"platformTypes"`
			ThreatEntryTypes []string            `json:"threatEntryTypes"`
			ThreatEntries    []map[string]string `json:"threatEntries"`
		} `json:"threatInfo"`
	}
	sb := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("key")
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("method %s, content-type %q", r.Method, r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, "{}")
	})
	if err := sb.Check(context.Background(), "https://example.com/x"); err != nil {
		t.Fatal(err)
	}

	if gotKey != testKey {
		t.Errorf("key param = %q", gotKey)
	}
	ti := body.ThreatInfo
	if strings.Join(ti.ThreatTypes, ",") != "MALWARE,SOCIAL_ENGINEERING,UNWANTED_SOFTWARE,POTENTIALLY_HARMFUL_APPLICATION" ||
		strings.Join(ti.PlatformTypes, ",") != "ANY_PLATFORM" || strings.Join(ti.ThreatEntryTypes, ",") != "URL" {
		t.Errorf("threatInfo = %+v", ti)
	}
	if len(ti.ThreatEntries) != 1 || ti.ThreatEntries[0]["url"] != "https://example.com/x" {
		t.Errorf("threatEntries = %+v", ti.ThreatEntries)
	}
	if body.Client["clientId"] != "shortlink" || body.Client["clientVersion"] != "2" {
		t.Errorf("client = %+v", body.Client)
	}
}

func TestCheckBatchMapsMatches(t *testing.T) {
	var gotEntries int
	sb := serve(t, func(w http.ResponseWriter, r *http.Request) {
		var body findRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotEntries = len(body.ThreatInfo.ThreatEntries)
		io.WriteString(w, `{"matches":[{"threatType":"MALWARE","threat":{"url":"https://b.example/"}}]}`)
	})
	flagged, err := sb.CheckBatch(context.Background(), []string{"https://a.example/", "https://b.example/", "https://c.example/"})
	if err != nil {
		t.Fatal(err)
	}
	if gotEntries != 3 || len(flagged) != 1 || flagged["https://b.example/"] != "MALWARE" {
		t.Fatalf("entries sent %d, flagged %v", gotEntries, flagged)
	}
}

func TestCheckBatchRejectsOver500(t *testing.T) {
	sb := serve(t, func(w http.ResponseWriter, _ *http.Request) { t.Error("request sent"); io.WriteString(w, "{}") })
	urls := make([]string, 501)
	for i := range urls {
		urls[i] = "https://example.com/" + strings.Repeat("x", i%7)
	}
	if _, err := sb.CheckBatch(context.Background(), urls); err == nil {
		t.Fatal("501 URLs accepted")
	}
}

func TestCheckBatchErrorIsUnavailable(t *testing.T) {
	sb := serve(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
	_, err := sb.CheckBatch(context.Background(), []string{"https://a.example/"})
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), testKey) {
		t.Fatalf("err = %v", err)
	}
}
