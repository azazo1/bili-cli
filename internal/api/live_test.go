package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractLiveRoomID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"5440", "5440"},
		{"https://live.bilibili.com/5440?spm_id_from=333", "5440"},
	}
	for _, item := range cases {
		got, err := ExtractLiveRoomID(item.input)
		if err != nil || got != item.want {
			t.Fatalf("ExtractLiveRoomID(%q) = %q, %v", item.input, got, err)
		}
	}
	if _, err := ExtractLiveRoomID("https://example.com/5440"); err == nil {
		t.Fatal("expected invalid live host error")
	}
}

func TestLiveStreamFromData(t *testing.T) {
	stream := liveStreamFromData(map[string]any{
		"room_id":     float64(5440),
		"title":       "demo live",
		"live_status": float64(1),
		"playurl_info": map[string]any{
			"playurl": map[string]any{
				"stream": []any{
					map[string]any{
						"protocol_name": "http_stream",
						"format": []any{map[string]any{
							"format_name": "flv",
							"codec": []any{map[string]any{
								"codec_name": "avc",
								"base_url":   "/live/demo.flv",
								"url_info": []any{map[string]any{
									"host":  "https://cdn.example.com",
									"extra": "?token=1",
								}},
							}},
						}},
					},
				},
			},
		},
	}, "flv")
	if stream.URL != "https://cdn.example.com/live/demo.flv?token=1" || stream.Title != "demo live" || stream.LiveStatus != 1 {
		t.Fatalf("unexpected live stream: %#v", stream)
	}
}

func TestGetLiveRoomInfoParsesH5Payload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xlive/web-room/v1/index/getH5InfoByRoom" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("room_id") != "5440" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"code":0,"data":{"room_info":{"uid":12,"room_id":5440,"title":"demo live","cover":"https://example.test/cover.jpg","live_status":1},"anchor_info":{"base_info":{"uname":"up"}}}}`)
	}))
	defer server.Close()
	client := NewClient()
	client.LiveBaseURL = server.URL
	client.HTTP = server.Client()
	info, err := client.GetLiveRoomInfo(context.Background(), "5440", nil)
	if err != nil || info.RoomID != "5440" || info.UID != 12 || info.Title != "demo live" || info.LiveStatus != 1 || info.UName != "up" {
		t.Fatalf("GetLiveRoomInfo() = %#v, %v", info, err)
	}
}
