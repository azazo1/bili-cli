package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/output"
)

func TestReadOnlyAllowsMsgListAndBlocksSend(t *testing.T) {
	app := newTestApp(t)
	app.Config.Safety.ReadOnly = true
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"msg", "send", "42", "hello", "--json"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected send error: %v", err)
	}
	var payload map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &payload); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if payload["error"].(map[string]any)["code"] != string(api.CodePermissionDenied) {
		t.Fatalf("unexpected send payload: %#v", payload)
	}

	stdout.Reset()
	saveTestCredential(t, app)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":0,"data":{"mid":100,"wbi_img":{"img_url":"https://example.test/0123456789abcdef0123456789abcdef.png","sub_url":"https://example.test/fedcba9876543210fedcba9876543210.png"}}}`)
		case "/x/frontend/finger/spi":
			fmt.Fprint(w, `{"code":0,"data":{"b_3":"device3","b_4":"device4"}}`)
		case "/x/internal/gaia-gateway/ExClimbWuzhi":
			fmt.Fprint(w, `{"code":0}`)
		case "/session_svr/v1/session_svr/get_sessions":
			fmt.Fprint(w, `{"code":0,"data":{"session_list":[]}}`)
		case "/account/v1/user/cards":
			fmt.Fprint(w, `{"code":0,"data":[]}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	app.API.BaseURL = server.URL
	app.API.VCBaseURL = server.URL
	app.API.HTTP = server.Client()
	root = NewRoot(app)
	root.SetArgs([]string{"msg", "list", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNonDestructiveAllowsSendAndBlocksRemove(t *testing.T) {
	app := newTestApp(t)
	app.Config.Safety.NonDestructive = true
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"msg", "rm", "42", "--yes", "--json"})
	err := root.ExecuteContext(context.Background())
	assertPermissionDenied(t, err, stdout)

	stdout.Reset()
	root = NewRoot(app)
	root.SetArgs([]string{"msg", "send", "42", "hello", "--json"})
	err = root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("send should fail without login: %v", err)
	}
	var payload map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &payload); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if payload["error"].(map[string]any)["code"] == string(api.CodePermissionDenied) {
		t.Fatalf("non_destructive should allow send: %#v", payload)
	}
}

func TestMsgSendEmptyTextIsInvalidInput(t *testing.T) {
	app := newTestApp(t)
	saveTestCredential(t, app)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/x/web-interface/nav" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"code":0,"data":{"mid":100,"wbi_img":{"img_url":"https://example.test/0123456789abcdef0123456789abcdef.png","sub_url":"https://example.test/fedcba9876543210fedcba9876543210.png"}}}`)
	}))
	defer server.Close()
	app.API.BaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"msg", "send", "42", "--json"})
	err := root.ExecuteContext(context.Background())
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &payload); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if payload["error"].(map[string]any)["code"] != string(api.CodeInvalidInput) {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestMsgRemoveWithoutYesDoesNotCallAPI(t *testing.T) {
	app := newTestApp(t)
	saveTestCredential(t, app)
	app.Config.Safety.ConfirmDangerousActions = true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x/web-interface/nav" {
			fmt.Fprint(w, `{"code":0,"data":{"mid":100,"wbi_img":{"img_url":"https://example.test/0123456789abcdef0123456789abcdef.png","sub_url":"https://example.test/fedcba9876543210fedcba9876543210.png"}}}`)
			return
		}
		t.Fatalf("unexpected request: %s", r.URL.Path)
	}))
	defer server.Close()
	app.API.BaseURL = server.URL
	app.API.VCBaseURL = server.URL
	app.API.HTTP = server.Client()
	app.In = strings.NewReader("\n")
	app.Out = &output.Writer{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"msg", "rm", "42"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMsgShowJSONExpandsContentAndSeq(t *testing.T) {
	app := newTestApp(t)
	saveTestCredential(t, app)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":0,"data":{"mid":100,"wbi_img":{"img_url":"https://example.test/0123456789abcdef0123456789abcdef.png","sub_url":"https://example.test/fedcba9876543210fedcba9876543210.png"}}}`)
		case "/x/frontend/finger/spi":
			fmt.Fprint(w, `{"code":0,"data":{"b_3":"device3","b_4":"device4"}}`)
		case "/x/internal/gaia-gateway/ExClimbWuzhi":
			fmt.Fprint(w, `{"code":0}`)
		case "/svr_sync/v1/svr_sync/fetch_session_msgs":
			fmt.Fprint(w, `{"code":0,"data":{"has_more":0,"max_seqno":9,"messages":[{"msg_seqno":8,"sender_uid":42,"msg_type":1,"content":"{\"content\":\"hi\"}","timestamp":1700000000},{"msg_seqno":9,"sender_uid":42,"msg_type":11,"content":"{\"title\":\"demo\",\"bvid\":\"BV1xx\",\"cover\":\"https://example.test/cover.jpg\"}","timestamp":1700000001}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	app.API.BaseURL = server.URL
	app.API.VCBaseURL = server.URL
	app.API.HTTP = server.Client()
	stdout := &bytes.Buffer{}
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}}
	root := NewRoot(app)
	root.SetArgs([]string{"msg", "show", "42", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	data := decodeSuccessData(t, stdout)
	items := data["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("unexpected items: %#v", data)
	}
	video := items[1].(map[string]any)
	detail := video["detail"].(map[string]any)
	content := video["content"].(map[string]any)
	if content["bvid"] != "BV1xx" || detail["url"] != "https://www.bilibili.com/video/BV1xx" {
		t.Fatalf("json did not expand video: %#v", video)
	}

	stdout.Reset()
	root = NewRoot(app)
	root.SetArgs([]string{"msg", "show", "42", "9", "--json"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	data = decodeSuccessData(t, stdout)
	item := data["item"].(map[string]any)
	if item["seqno"] != float64(9) && fmt.Sprint(item["seqno"]) != "9" {
		t.Fatalf("unexpected single item: %#v", data)
	}
	if _, ok := data["items"]; ok {
		t.Fatalf("single show should not include items: %#v", data)
	}
}

func decodeSuccessData(t *testing.T, stdout *bytes.Buffer) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok || payload["ok"] != true {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	return data
}

func saveTestCredential(t *testing.T, app *App) {
	t.Helper()
	app.Auth.Dir = t.TempDir()
	app.Auth.File = filepath.Join(app.Auth.Dir, "auth.json")
	if err := app.Auth.Save(&api.Credential{Sessdata: "session", BiliJct: "csrf", DedeUserID: "100"}); err != nil {
		t.Fatal(err)
	}
}

func assertPermissionDenied(t *testing.T, err error, stdout *bytes.Buffer) {
	t.Helper()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("unexpected command error: %v", err)
	}
	var payload map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &payload); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if payload["error"].(map[string]any)["code"] != string(api.CodePermissionDenied) {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}
