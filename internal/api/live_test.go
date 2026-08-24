package api

import "testing"

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
