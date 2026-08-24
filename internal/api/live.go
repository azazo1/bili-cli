package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type LiveStream struct {
	RoomID      string
	Title       string
	LiveStatus  int
	Protocol    string
	Format      string
	Codec       string
	URL         string
}

func ExtractLiveRoomID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", NewError(CodeInvalidInput, "", "直播间引用不能为空")
	}
	if id, ok := positiveNumber(value); ok {
		return id, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return "", NewError(CodeInvalidInput, "", "直播间引用必须是正整数 ID 或直播间 URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "live.bilibili.com" && host != "www.live.bilibili.com" {
		return "", NewError(CodeInvalidInput, "", "直播间 URL 必须来自 live.bilibili.com")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) > 0 {
		if id, ok := positiveNumber(parts[0]); ok {
			return id, nil
		}
	}
	return "", NewError(CodeInvalidInput, "", "直播间 URL 缺少有效房间号")
}

func positiveNumber(value string) (string, bool) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 1 {
		return "", false
	}
	return strconv.FormatInt(parsed, 10), true
}

func (c *Client) GetLiveStream(ctx context.Context, roomID string, quality int, preferredFormat string, cred *Credential) (LiveStream, error) {
	if _, ok := positiveNumber(roomID); !ok {
		return LiveStream{}, NewError(CodeInvalidInput, "获取直播地址", "直播间 ID 必须是正整数")
	}
	if quality < 1 {
		quality = 10000
	}
	preferredFormat = strings.ToLower(strings.TrimSpace(preferredFormat))
	if preferredFormat == "" {
		preferredFormat = "flv"
	}
	query := url.Values{
		"room_id":     []string{roomID},
		"protocol":    []string{"0,1"},
		"format":      []string{"0,1,2"},
		"codec":       []string{"0,1,2"},
		"qn":          []string{strconv.Itoa(quality)},
		"platform":    []string{"web"},
		"ptype":       []string{"8"},
		"dolby":       []string{"5"},
		"panorama":    []string{"1"},
		"web_location": []string{"444.8"},
	}
	requestCredential := c.credentialWithDevice(ctx, cred)
	signed, signErr := c.signWBI(ctx, query, requestCredential)
	if signErr != nil {
		return LiveStream{}, withAction("获取直播地址", signErr)
	}
	var data map[string]any
	if err := c.requestAtBase(ctx, "GET", c.LiveBaseURL, defaultLiveBaseURL, "/xlive/web-room/v2/index/getRoomPlayInfo", signed, nil, requestCredential, &data); err != nil {
		return LiveStream{}, withAction("获取直播地址", err)
	}
	stream := liveStreamFromData(data, preferredFormat)
	if stream.URL == "" {
		return LiveStream{}, NewError(CodeNotFound, "获取直播地址", fmt.Sprintf("直播间未开播或没有可用的 %s 流", preferredFormat))
	}
	return stream, nil
}

func liveStreamFromData(data map[string]any, preferredFormat string) LiveStream {
	stream := LiveStream{
		RoomID:     strconv.FormatInt(int64Value(data["room_id"], 0), 10),
		Title:      stringValue(data["title"]),
		LiveStatus: intValue(data["live_status"], 0),
	}
	playurlInfo := mapValue(data["playurl_info"])
	playurl := mapValue(playurlInfo["playurl"])
	for _, streamValue := range mapList(playurl["stream"]) {
		protocol := stringValue(streamValue["protocol_name"])
		for _, formatValue := range mapList(streamValue["format"]) {
			formatName := strings.ToLower(stringValue(formatValue["format_name"]))
			formatMatches := preferredFormat == "" || strings.Contains(formatName, preferredFormat)
			if preferredFormat == "hls" {
				formatMatches = formatMatches || strings.Contains(strings.ToLower(protocol), "hls") || formatName == "ts"
			}
			if !formatMatches {
				continue
			}
			for _, codecValue := range mapList(formatValue["codec"]) {
				baseURL := stringValue(codecValue["base_url"])
				codecName := stringValue(codecValue["codec_name"])
				for _, urlInfo := range mapList(codecValue["url_info"]) {
					host := strings.TrimRight(stringValue(urlInfo["host"]), "/")
					extra := stringValue(urlInfo["extra"])
					candidate := baseURL
					if !strings.HasPrefix(candidate, "http://") && !strings.HasPrefix(candidate, "https://") {
						candidate = host + "/" + strings.TrimLeft(candidate, "/")
					}
					candidate += extra
					if candidate != "" {
						stream.Protocol = protocol
						stream.Format = formatName
						stream.Codec = codecName
						stream.URL = candidate
						return stream
					}
				}
			}
		}
	}
	return stream
}
