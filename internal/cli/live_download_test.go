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
