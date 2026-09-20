package watch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifierPostsWebhookEnvelope(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected webhook request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	notifier := Notifier{
		Webhook: server.URL,
		HTTP:    server.Client(),
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	events := []Event{{RuleID: 1, Kind: KindUpVideo, HappenedAt: time.Unix(1, 0).UTC(), Title: "demo", URL: "https://www.bilibili.com/video/BV1ABcsztEcY"}}
	if err := notifier.Send(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	data := payload["data"].(map[string]any)
	items := data["events"].([]any)
	if payload["ok"] != true || len(items) != 1 {
		t.Fatalf("unexpected webhook payload: %#v", payload)
	}
}

func TestNotifierExecSetsEnvironment(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")
	notifier := Notifier{
		Exec:   "printf '%s\\n' \"$BILI_WATCH_KIND\" \"$BILI_WATCH_TITLE\" \"$BILI_WATCH_URL\" > " + out,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err := notifier.Send(context.Background(), []Event{{
		RuleID: 2,
		Kind:   KindUpLive,
		Title:  "live title",
		URL:    "https://live.bilibili.com/5440",
	}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "up.live\nlive title\nhttps://live.bilibili.com/5440\n" {
		t.Fatalf("unexpected exec env file: %q", data)
	}
}
