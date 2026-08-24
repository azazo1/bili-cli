package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azazo1/bilibili-cli/internal/output"
)

func TestLiveDownloadWorksInReadOnlyMode(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprintf(w, `{"code":0,"data":{"wbi_img":{"img_url":"%s/wbi/0123456789abcdef0123456789abcdef.png","sub_url":"%s/wbi/fedcba9876543210fedcba9876543210.png"}}}`, serverURL, serverURL)
		case "/xlive/web-room/v2/index/getRoomPlayInfo":
			fmt.Fprint(w, `{"code":0,"data":{"room_id":5440,"title":"demo live","live_status":1,"playurl_info":{"playurl":{"stream":[{"protocol_name":"http_stream","format":[{"format_name":"flv","codec":[{"codec_name":"avc","base_url":"/stream.flv","url_info":[{"host":"`+serverURL+`","extra":"?token=1"}]}]}]}]}}}}`)
		case "/stream.flv":
			w.Header().Set("Content-Type", "video/x-flv")
			_, _ = io.WriteString(w, "FLV test stream")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.LiveBaseURL = server.URL
	app.API.HTTP = server.Client()
	app.Config.Safety.ReadOnly = true
	app.Out = &output.Writer{Stdout: io.Discard, Stderr: io.Discard, DefaultMode: "rich"}
	outDir := t.TempDir()
	root := NewRoot(app)
	root.SetArgs([]string{"live", "download", "5440", "-o", outDir})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(outDir, "demo live_*.flv"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("unexpected output files: %v, %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "FLV test stream" {
		t.Fatalf("unexpected stream data: %q", data)
	}
}

func TestParseLiveQuality(t *testing.T) {
	cases := map[string]int{
		"流畅": 80,
		"高清": 150,
		"蓝光": 400,
		"原画": 10000,
		"4K": 20000,
		"杜比": 30000,
		"400": 400,
	}
	for input, want := range cases {
		got, err := parseLiveQuality(input)
		if err != nil || got != want {
			t.Fatalf("parseLiveQuality(%q) = %d, %v", input, got, err)
		}
	}
	if _, err := parseLiveQuality("未知"); err == nil {
		t.Fatal("expected unsupported quality error")
	}
}

func TestParseLiveLimits(t *testing.T) {
	duration, err := parseLiveDuration("90m")
	if err != nil || duration != 90*time.Minute {
		t.Fatalf("parseLiveDuration = %s, %v", duration, err)
	}
	size, err := parseLiveSize("1.5GB")
	if err != nil || size != int64(1.5*(1<<30)) {
		t.Fatalf("parseLiveSize = %d, %v", size, err)
	}
	if _, err := parseLiveDuration("0m"); err == nil {
		t.Fatal("expected invalid duration error")
	}
	if _, err := parseLiveSize("large"); err == nil {
		t.Fatal("expected invalid size error")
	}
}
