package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetMsgUnreadUsesVCHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session_svr/v1/session_svr/single_unread" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("unread_type") != "0" || r.Header.Get("Cookie") == "" {
			t.Fatalf("unexpected unread request: %s cookie=%q", r.URL.RawQuery, r.Header.Get("Cookie"))
		}
		fmt.Fprint(w, `{"code":0,"data":{"follow_unread":2,"unfollow_unread":1}}`)
	}))
	defer server.Close()
	client := newMsgTestClient(server)
	result, err := client.GetMsgUnread(context.Background(), msgTestCredential())
	if err != nil || intValue(result["follow_unread"], 0) != 2 {
		t.Fatalf("GetMsgUnread() = %#v, %v", result, err)
	}
}

func TestGetSessionListSignsAndMergesCards(t *testing.T) {
	var sawSessions, sawCards bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			writeMsgWBI(w)
		case "/session_svr/v1/session_svr/get_sessions":
			sawSessions = true
			if r.URL.Query().Get("session_type") != "1" || r.URL.Query().Get("w_rid") == "" {
				t.Fatalf("unexpected sessions query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":{"has_more":1,"session_list":[{"talker_id":42,"unread_count":3,"is_follow":1,"session_ts":1700000000,"last_msg":{"msg_type":1,"content":"{\"content\":\"hi\"}","timestamp":1700000000}}]}}`)
		case "/account/v1/user/cards":
			sawCards = true
			if r.URL.Query().Get("uids") != "42" {
				t.Fatalf("unexpected cards query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":[{"mid":42,"name":"alice","face":"https://example.test/a.png"}]}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newMsgTestClient(server)
	result, err := client.GetSessionList(context.Background(), 0, msgTestCredential())
	if err != nil {
		t.Fatal(err)
	}
	sessions := mapList(result["sessions"])
	if !sawSessions || !sawCards || len(sessions) != 1 || stringValue(sessions[0]["name"]) != "alice" {
		t.Fatalf("unexpected sessions: %#v", result)
	}
}

func TestGetSessionMessagesSignsTalker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			writeMsgWBI(w)
		case "/svr_sync/v1/svr_sync/fetch_session_msgs":
			if r.URL.Query().Get("talker_id") != "42" || r.URL.Query().Get("w_rid") == "" || r.URL.Query().Get("end_seqno") != "" {
				t.Fatalf("unexpected messages query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":{"has_more":0,"max_seqno":9,"messages":[{"msg_seqno":9,"sender_uid":42,"receiver_id":100,"msg_type":1,"content":"{\"content\":\"hi\"}","timestamp":1700000000}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newMsgTestClient(server)
	result, err := client.GetSessionMessages(context.Background(), 42, 20, 0, msgTestCredential())
	if err != nil || len(mapList(result["messages"])) != 1 {
		t.Fatalf("GetSessionMessages() = %#v, %v", result, err)
	}
}

func TestGetSessionMessagesUsesEndSeqno(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			writeMsgWBI(w)
		case "/svr_sync/v1/svr_sync/fetch_session_msgs":
			if r.URL.Query().Get("end_seqno") != "9" || r.URL.Query().Get("begin_seqno") != "0" {
				t.Fatalf("unexpected history query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":{"has_more":1,"min_seqno":1,"max_seqno":8,"messages":[{"msg_seqno":8,"sender_uid":42,"msg_type":1,"content":"{\"content\":\"old\"}","timestamp":1700000000}]}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newMsgTestClient(server)
	result, err := client.GetSessionMessages(context.Background(), 42, 20, 9, msgTestCredential())
	if err != nil || !boolValue(result["has_more"]) || int64Value(result["min_seqno"], 0) != 1 {
		t.Fatalf("GetSessionMessages() = %#v, %v", result, err)
	}
}

func TestSendTextMessagePostsFormAndWBI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			writeMsgWBI(w)
		case "/web_im/v1/web_im/send_msg":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			body, _ := io.ReadAll(r.Body)
			form := string(body)
			if !strings.Contains(form, "msg%5Bcontent%5D=") || !strings.Contains(form, "csrf=csrf") {
				t.Fatalf("unexpected send form: %s", form)
			}
			if r.URL.Query().Get("w_sender_uid") != "100" || r.URL.Query().Get("w_rid") == "" {
				t.Fatalf("unexpected send query: %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":{"msg_key":123}}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newMsgTestClient(server)
	result, err := client.SendTextMessage(context.Background(), 42, "hello", msgTestCredential())
	if err != nil || intValue(result["msg_key"], 0) != 123 {
		t.Fatalf("SendTextMessage() = %#v, %v", result, err)
	}
}

func TestAckAndRemoveSessionIncludeTalkerAndCSRF(t *testing.T) {
	var sawAck, sawRemove bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			writeMsgWBI(w)
		case "/session_svr/v1/session_svr/update_ack":
			sawAck = true
			if r.Method != http.MethodGet || r.URL.Query().Get("talker_id") != "42" || r.URL.Query().Get("csrf") != "csrf" {
				t.Fatalf("unexpected ack request: %s %s", r.Method, r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"code":0,"data":null}`)
		case "/session_svr/v1/session_svr/remove_session":
			sawRemove = true
			body, _ := io.ReadAll(r.Body)
			form := string(body)
			if r.Method != http.MethodPost || !strings.Contains(form, "talker_id=42") || !strings.Contains(form, "csrf=csrf") {
				t.Fatalf("unexpected remove request: %s %s", r.Method, form)
			}
			fmt.Fprint(w, `{"code":0,"data":null}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newMsgTestClient(server)
	cred := msgTestCredential()
	if err := client.AckSession(context.Background(), 42, 9, cred); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveSession(context.Background(), 42, cred); err != nil {
		t.Fatal(err)
	}
	if !sawAck || !sawRemove {
		t.Fatal("missing ack or remove request")
	}
}

func TestMsgMethodsRequireLogin(t *testing.T) {
	client := NewClient()
	if _, err := client.GetMsgUnread(context.Background(), nil); CodeOf(err) != CodeNotAuthenticated {
		t.Fatalf("unread without cred: %v", err)
	}
	if _, err := client.SendTextMessage(context.Background(), 1, "hi", nil); CodeOf(err) != CodeNotAuthenticated {
		t.Fatalf("send without cred: %v", err)
	}
}

func newMsgTestClient(server *httptest.Server) *Client {
	client := NewClient()
	client.BaseURL = server.URL
	client.VCBaseURL = server.URL
	client.HTTP = server.Client()
	client.device = &Credential{Buvid3: "b3", Buvid4: "b4"}
	client.deviceExpires = time.Now().Add(time.Hour)
	return client
}

func msgTestCredential() *Credential {
	return &Credential{Sessdata: "sess", BiliJct: "csrf", DedeUserID: "100"}
}

func writeMsgWBI(w http.ResponseWriter) {
	fmt.Fprint(w, `{"code":-101,"data":{"wbi_img":{"img_url":"https://example.test/0123456789abcdef0123456789abcdef.png","sub_url":"https://example.test/fedcba9876543210fedcba9876543210.png"}}}`)
}
