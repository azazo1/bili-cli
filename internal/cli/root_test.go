package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/config"
	"github.com/azazo1/bilibili-cli/internal/output"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("BILI_CONFIG_DIR", t.TempDir())
	return NewApp()
}

func TestRootVersionOutputUsesBuildVersion(t *testing.T) {
	originalVersion := Version
	Version = "v0.1.0"
	t.Cleanup(func() {
		Version = originalVersion
	})

	app := newTestApp(t)
	root := NewRoot(app)
	stdout := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"--version"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "v0.1.0\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestVideoInvalidBVIDEmitsStructuredError(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "invalid", "--json"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	var payload map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &payload); decodeErr != nil {
		t.Fatalf("invalid JSON: %v", decodeErr)
	}
	errorData := payload["error"].(map[string]any)
	details := errorData["details"].(map[string]any)
	if payload["ok"] != false || errorData["code"] != string(api.CodeInvalidInput) || !strings.Contains(details["usage"].(string), "bili video BV_OR_URL") {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestArgumentErrorIncludesCommandUsage(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"video"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	result := stderr.String()
	if !strings.Contains(result, "accepts 1 arg(s), received 0") || !strings.Contains(result, "Usage:\n  bili video BV_OR_URL") {
		t.Fatalf("argument error did not include usage: %q", result)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestFlagErrorIncludesCommandUsage(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "--unknown"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	result := stderr.String()
	if !strings.Contains(result, "unknown flag: --unknown") || !strings.Contains(result, "Usage:\n  bili video BV_OR_URL") {
		t.Fatalf("flag error did not include usage: %q", result)
	}
}

func TestArgumentErrorJSONIncludesUsage(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "--json"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errorData := payload["error"].(map[string]any)
	details := errorData["details"].(map[string]any)
	if !strings.Contains(details["usage"].(string), "bili video BV_OR_URL") {
		t.Fatalf("structured usage is missing: %#v", payload)
	}
}

func TestInvalidFlagValueIncludesCommandUsage(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "subtitle", "BV1ABcsztEcY", "--type", "invalid"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	result := stderr.String()
	if !strings.Contains(result, "--type 仅支持 all, ai 或 non-ai") || !strings.Contains(result, "Usage:\n  bili video subtitle BV_OR_URL") {
		t.Fatalf("invalid flag value did not include usage: %q", result)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestConflictingOutputFlagsIncludeCommandUsage(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "BV1ABcsztEcY", "--json", "--yaml"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	result := stderr.String()
	if !strings.Contains(result, "不能同时使用 --json 和 --yaml") || !strings.Contains(result, "Usage:\n  bili video BV_OR_URL") {
		t.Fatalf("conflicting output flags did not include usage: %q", result)
	}
}

func TestUnknownCommandIncludesRootUsage(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"unknown-command"})
	command, err := root.ExecuteContextC(context.Background())
	if err != nil {
		var exitErr *ExitError
		if !errors.As(err, &exitErr) {
			err = app.failUsage(command, err)
		}
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	result := stderr.String()
	if !strings.Contains(result, "unknown command \"unknown-command\" for \"bili\"") || !strings.Contains(result, "Usage:\n  bili [command]") {
		t.Fatalf("unknown command did not include usage: %q", result)
	}
}

func TestHotCommandUsesNormalizedEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/popular" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("ps") != "1" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"code":0,"data":{"list":[{"bvid":"BV1ABcsztEcY","title":"demo","duration":60,"owner":{"mid":1,"name":"up"},"stat":{"view":9}}]}}`)
	}))
	defer server.Close()

	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "hot", "--max", "1", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	items := data["items"].([]any)
	item := items[0].(map[string]any)
	if envelope["ok"] != true || item["duration"] != "01:00" || item["bvid"] != "BV1ABcsztEcY" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestAnonymousVideoWarningUsesStderr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			fmt.Fprint(w, `{"code":0,"data":{"bvid":"BV1ABcsztEcY","title":"demo","duration":60,"owner":{"mid":1,"name":"up"},"stat":{"view":9}}}`)
		case "/x/player/pagelist":
			fmt.Fprint(w, `{"code":0,"data":[{"cid":42,"part":"demo"}]}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "BV1ABcsztEcY", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["ok"] != true || !strings.Contains(stderr.String(), "level=WARN") || strings.Contains(stdout.String(), "level=WARN") {
		t.Fatalf("unexpected output streams: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestReadOnlyBlocksAccountWriteCommands(t *testing.T) {
	app := newTestApp(t)
	app.Config.Safety.ReadOnly = true
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "like", "BV1ABcsztEcY", "--json"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errorData := payload["error"].(map[string]any)
	if errorData["code"] != string(api.CodePermissionDenied) {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestReadOnlyAllowsLogout(t *testing.T) {
	app := newTestApp(t)
	app.Config.Safety.ReadOnly = true
	tempDir := t.TempDir()
	app.Auth.Dir = tempDir
	app.Auth.File = filepath.Join(tempDir, "auth.json")
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"me", "logout"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("logout did not render completion")
	}
}

func TestNewAppAppliesDefaultTimeoutWithoutCreatingConfig(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("BILI_CONFIG_DIR", configDir)
	t.Setenv("BILI_HTTP_TIMEOUT", "7")
	app := NewApp()
	if app.API.HTTP.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s", app.API.HTTP.Timeout)
	}
	if _, err := os.Stat(filepath.Join(configDir, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("NewApp created config file: %v", err)
	}
}

func TestConfigInitCreatesDefaultFile(t *testing.T) {
	app := newTestApp(t)
	root := NewRoot(app)
	root.SetArgs([]string{"config", "init"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(app.ConfigStore.File); err != nil {
		t.Fatalf("config init did not create config file: %v", err)
	}
}

func TestConfigStatusReportsMissingConfig(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"config", "status", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	if data["status"] != "missing" || data["needs_upgrade"] != true {
		t.Fatalf("unexpected config status: %#v", data)
	}
	if app.ConfigStore.Exists() {
		t.Fatal("config status created a config file")
	}
}

func TestRenderConfigStatusGroupsFieldStates(t *testing.T) {
	stdout := &bytes.Buffer{}
	renderConfigStatus(stdout, config.StatusReport{
		File:         "/tmp/config.toml",
		Exists:       true,
		Loaded:       true,
		NeedsUpgrade: true,
		Fields: []config.FieldStatus{
			{Path: "version", Value: 1, Status: "set"},
			{Path: "output.format", Value: "auto", Status: "set"},
			{Path: "network.timeout_seconds", Value: 30, Status: "set"},
			{Path: "download.threads", Value: 8, Status: "missing"},
			{Path: "safety.read_only", Value: true, Status: "set"},
			{Path: "safety.confirm_dangerous_actions", Value: true, Status: "set"},
		},
	})
	result := stdout.String()
	for _, expected := range []string{
		"version = 1 (set)",
		"output:\n  format = auto (set)",
		"network:\n  timeout_seconds = 30 (set)",
		"download:\n  threads = 8 (missing)",
		"safety:\n  read_only = true (set)",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("missing grouped config field %q: %s", expected, result)
		}
	}
	if strings.Contains(result, "状态:") || strings.Contains(result, "文件状态:") {
		t.Fatalf("unexpected file-level state labels: %s", result)
	}
}

func TestConfigStatusShowsConfigError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BILI_CONFIG_DIR", dir)
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("version = 2\n[output]\nformat = \"xml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"config", "status", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	if data["status"] != "error" || data["error"] == "" {
		t.Fatalf("unexpected config error status: %#v", data)
	}
}

func TestConfigUpgradeWritesCurrentFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BILI_CONFIG_DIR", dir)
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("version = 1\n[safety]\nread_only = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.Out = &output.Writer{Stdout: io.Discard, Stderr: io.Discard, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"config", "upgrade"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "version = 3") || !strings.Contains(text, "threads = 8") || !strings.Contains(text, "read_only = true") || !strings.Contains(text, "interval_seconds = 300") {
		t.Fatalf("unexpected upgraded config: %s", text)
	}
}

func TestVideoSubtitleListsAllTracks(t *testing.T) {
	server := newSubtitleTestServer(t)
	defer server.Close()
	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "subtitle", "BV1ABcsztEcY", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	subtitles := data["subtitles"].([]any)
	first := subtitles[0].(map[string]any)
	second := subtitles[1].(map[string]any)
	if data["available_subtitle_count"] != float64(2) || data["subtitle_count"] != float64(2) || first["line_count"] != float64(2) || second["is_ai"] != true {
		t.Fatalf("unexpected subtitle payload: %#v", data)
	}
}

func TestVideoSubtitleFiltersTracks(t *testing.T) {
	server := newSubtitleTestServer(t)
	defer server.Close()
	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "subtitle", "BV1ABcsztEcY", "--language", "zh_CN", "--type", "non-ai", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	subtitles := data["subtitles"].([]any)
	item := subtitles[0].(map[string]any)
	if data["available_subtitle_count"] != float64(2) || data["subtitle_count"] != float64(1) || item["id"] != "11" {
		t.Fatalf("unexpected filtered subtitle payload: %#v", data)
	}
}

func TestVideoSubtitleDoesNotFetchFirstPageForMultiPartVideo(t *testing.T) {
	playerRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/player/pagelist":
			fmt.Fprint(w, `{"code":0,"data":[{"cid":41,"part":"first"},{"cid":42,"part":"second"}]}`)
		case "/x/player/v2":
			playerRequests++
			fmt.Fprint(w, `{"code":0,"data":{"subtitle":{"subtitles":[]}}}`)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"video", "st", "BV1ABcsztEcY", "--json"})
	err := root.ExecuteContext(context.Background())
	if api.CodeOf(err) != api.CodeInvalidInput {
		t.Fatalf("unexpected multi part error: %s", api.CodeOf(err))
	}
	var envelope map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("invalid structured page error: %v", decodeErr)
	}
	details := envelope["error"].(map[string]any)["details"].(map[string]any)
	if len(details["pages"].([]any)) != 2 {
		t.Fatalf("unexpected page details: %#v", details)
	}
	if playerRequests != 0 {
		t.Fatalf("requested first page subtitle: %d", playerRequests)
	}
}

func TestFilterSubtitleTracksSupportsIDs(t *testing.T) {
	tracks := []api.SubtitleTrack{
		{ID: "11", Language: "zh-CN"},
		{ID: "12", Language: "en-US", Type: 1},
	}
	filtered := filterSubtitleTracks(tracks, []string{"12"}, nil, "ai")
	if len(filtered) != 1 || filtered[0].ID != "12" {
		t.Fatalf("unexpected ID filter result: %#v", filtered)
	}
}

func TestVideoSubtitleAliasExportsEachTrack(t *testing.T) {
	server := newSubtitleTestServer(t)
	defer server.Close()
	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	app.Out = &output.Writer{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	outputDir := t.TempDir()
	root := NewRoot(app)
	root.SetArgs([]string{"video", "st", "BV1ABcsztEcY", "-o", outputDir})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	zhPath := filepath.Join(outputDir, "demo.zh-CN.srt")
	enPath := filepath.Join(outputDir, "demo.en-US-ai.srt")
	zhData, err := os.ReadFile(zhPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(enPath); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zhData), "00:00:00,000 --> 00:00:01,500") {
		t.Fatalf("unexpected SRT content: %q", zhData)
	}
}

func TestExportSubtitleFilesUsesVideoBaseName(t *testing.T) {
	outputDir := t.TempDir()
	videoTitle := videoDownloadFileTitle("demo", 2, 3, "second")
	items := []subtitleCommandItem{
		{Track: api.SubtitleTrack{Language: "zh-CN"}, Cues: []api.SubtitleCue{{From: 0, To: 1, Content: "normal"}}},
		{Track: api.SubtitleTrack{Language: "zh-CN", Type: 1}, Cues: []api.SubtitleCue{{From: 0, To: 1, Content: "ai"}}},
	}
	if err := exportSubtitleFiles(outputDir, videoTitle, items); err != nil {
		t.Fatal(err)
	}
	normalPath := filepath.Join(outputDir, "demo_P02_second.zh-CN.srt")
	aiPath := filepath.Join(outputDir, "demo_P02_second.zh-CN-ai.srt")
	for _, path := range []string{normalPath, aiPath} {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("unexpected suffix subtitle export: %s, %v", path, err)
		}
	}
	if items[0].OutputPath != normalPath || items[1].OutputPath != aiPath {
		t.Fatalf("unexpected subtitle output paths: %#v", items)
	}
}

func TestExportSubtitleFilesTreatsSRTPathAsDirectory(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "subtitles.srt")
	items := []subtitleCommandItem{
		{Track: api.SubtitleTrack{Language: "zh-CN"}, Cues: []api.SubtitleCue{{From: 0, To: 1, Content: "normal"}}},
	}
	if err := exportSubtitleFiles(outputDir, "demo", items); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "demo.zh-CN.srt")); err != nil {
		t.Fatal(err)
	}
}

func TestVideoSubtitleCommandReplacesLegacyFlags(t *testing.T) {
	root := NewRoot(newTestApp(t))
	video, _, err := root.Find([]string{"video"})
	if err != nil {
		t.Fatal(err)
	}
	if video.Flags().Lookup("subtitle") != nil {
		t.Fatal("legacy subtitle flag is still registered")
	}
	subtitle, _, err := root.Find([]string{"video", "st"})
	if err != nil {
		t.Fatal(err)
	}
	if subtitle.Name() != "subtitle" {
		t.Fatalf("unexpected subtitle command: %s", subtitle.Name())
	}
}

func TestCommandHierarchyUsesDomainParents(t *testing.T) {
	root := NewRoot(newTestApp(t))
	cases := [][]string{
		{"me"},
		{"me", "fav"},
		{"me", "history"},
		{"me", "dynamic"},
		{"video", "like"},
		{"video", "download"},
		{"video", "hot"},
		{"video", "watch"},
		{"user", "video"},
		{"user", "lists"},
		{"user", "follow"},
		{"dynamic", "post"},
		{"dynamic", "delete"},
		{"watch"},
		{"watch", "add"},
		{"watch", "check"},
		{"watch", "run"},
	}
	for _, args := range cases {
		command, _, err := root.Find(args)
		if err != nil {
			t.Fatalf("command %v not found: %v", args, err)
		}
		if command == nil {
			t.Fatalf("command %v resolved to nil", args)
		}
	}
	if _, _, err := root.Find([]string{"whoami"}); err == nil {
		t.Fatal("legacy whoami command is still registered")
	}
	if _, _, err := root.Find([]string{"lists"}); err == nil {
		t.Fatal("root lists command is unexpectedly registered")
	}
}

func TestMeRejectsUnknownPositionalArgument(t *testing.T) {
	app := newTestApp(t)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: stderr, DefaultMode: "rich"}
	root := NewRoot(app)
	root.SetArgs([]string{"me", "video"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	result := stderr.String()
	if !strings.Contains(result, "unknown command \"video\" for \"bili me\"") || !strings.Contains(result, "Usage:\n  bili me [flags]") {
		t.Fatalf("me argument error did not include usage: %q", result)
	}
}

func newSubtitleTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			fmt.Fprint(w, `{"code":0,"data":{"title":"demo"}}`)
		case "/x/player/pagelist":
			fmt.Fprint(w, `{"code":0,"data":[{"cid":42}]}`)
		case "/x/player/v2":
			fmt.Fprintf(w, `{"code":0,"data":{"subtitle":{"subtitles":[{"id":11,"lan":"zh-CN","lan_doc":"中文","subtitle_url":"%s/subtitles/zh.json","author":{"mid":7,"name":"up"},"type":0},{"id":12,"lan":"en-US","lan_doc":"English","subtitle_url":"%s/subtitles/en.json","type":1,"ai_type":3,"ai_status":1}]}}}`, serverURL, serverURL)
		case "/subtitles/zh.json":
			fmt.Fprint(w, `{"body":[{"from":0,"to":1.5,"content":"first"},{"from":1.5,"to":3,"content":"second"}]}`)
		case "/subtitles/en.json":
			fmt.Fprint(w, `{"body":[{"from":0,"to":1.5,"content":"one"}]}`)
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	serverURL = server.URL
	return server
}
