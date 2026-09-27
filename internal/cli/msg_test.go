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
