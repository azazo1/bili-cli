package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/azazo1/bilibili-cli/internal/output"
)

func TestWatchAddVideoCheckAndRemove(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/view" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"code":0,"data":{"bvid":"BV1ABcsztEcY","title":"demo","stat":{"view":9,"like":12000,"coin":1,"reply":2,"favorite":3,"danmaku":4,"share":5}}}`)
	}))
	defer server.Close()

	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: io.Discard}
	root := NewRoot(app)
	root.SetArgs([]string{"watch", "add", "video", "BV1ABcsztEcY", "--like", "10000", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var added map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	rules := added["data"].(map[string]any)["rules"].([]any)
	if added["ok"] != true || len(rules) != 1 {
		t.Fatalf("unexpected add payload: %#v", added)
	}

	stdout.Reset()
	root = NewRoot(app)
	root.SetArgs([]string{"watch", "check", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var checked map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &checked); err != nil {
		t.Fatal(err)
	}
	events := checked["data"].(map[string]any)["events"].([]any)
	if checked["ok"] != true || len(events) != 1 {
		t.Fatalf("unexpected check payload: %#v", checked)
	}

	stdout.Reset()
	root = NewRoot(app)
	root.SetArgs([]string{"watch", "rm", "1", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWatchWorksInReadOnly(t *testing.T) {
	app := newTestApp(t)
	app.Config.Safety.ReadOnly = true
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: io.Discard}
	root := NewRoot(app)
	root.SetArgs([]string{"watch", "add", "search", "golang", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != true {
		t.Fatalf("read_only blocked watch add: %#v", envelope)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("BILI_CONFIG_DIR"), "watch.json")); err != nil {
		t.Fatalf("watch.json was not written: %v", err)
	}
}

func TestWatchListEmpty(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: io.Discard}
	root := NewRoot(app)
	root.SetArgs([]string{"watch", "list", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	if envelope["ok"] != true || data["count"] != float64(0) {
		t.Fatalf("unexpected empty list: %#v", envelope)
	}
}
