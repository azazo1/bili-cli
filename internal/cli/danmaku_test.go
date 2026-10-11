package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/output"
)

// danmakuGRPCSegmentReply 构造 gRPC 长度前缀帧, 负载为 DmSegMobileReply.
func danmakuGRPCSegmentReply(t *testing.T, entries []struct {
	id       int64
	progress int
	mode     int
	content  string
}) []byte {
	t.Helper()
	payload := make([]byte, 0, 64)
	for _, entry := range entries {
		element := make([]byte, 0, 48)
		element = protowire.AppendTag(element, 1, protowire.VarintType)
		element = protowire.AppendVarint(element, uint64(entry.id))
		element = protowire.AppendTag(element, 2, protowire.VarintType)
		element = protowire.AppendVarint(element, uint64(entry.progress))
		element = protowire.AppendTag(element, 3, protowire.VarintType)
		element = protowire.AppendVarint(element, uint64(entry.mode))
		element = protowire.AppendTag(element, 4, protowire.VarintType)
		element = protowire.AppendVarint(element, 25)
		element = protowire.AppendTag(element, 5, protowire.VarintType)
		element = protowire.AppendVarint(element, 16777215)
		element = protowire.AppendTag(element, 6, protowire.BytesType)
		element = protowire.AppendString(element, "abcdef01")
		element = protowire.AppendTag(element, 7, protowire.BytesType)
		element = protowire.AppendString(element, entry.content)
		element = protowire.AppendTag(element, 8, protowire.VarintType)
		element = protowire.AppendVarint(element, 1770800435)
		element = protowire.AppendTag(element, 9, protowire.VarintType)
		element = protowire.AppendVarint(element, 10)
		element = protowire.AppendTag(element, 12, protowire.BytesType)
		element = protowire.AppendString(element, fmt.Sprintf("%d", entry.id))
		element = protowire.AppendTag(element, 27, protowire.VarintType)
		element = protowire.AppendVarint(element, 1)
		payload = protowire.AppendTag(payload, 1, protowire.BytesType)
		payload = protowire.AppendBytes(payload, element)
	}
	frame := make([]byte, 5+len(payload))
	frame[0] = 0
	length := len(payload)
	frame[1] = byte(length >> 24)
	frame[2] = byte(length >> 16)
	frame[3] = byte(length >> 8)
	frame[4] = byte(length)
	copy(frame[5:], payload)
	return frame
}

// newDanmakuTestServer 提供视频详情, 分P列表与弹幕 gRPC 的桩实现.
func newDanmakuTestServer(t *testing.T, capture *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/frontend/finger/spi":
			fmt.Fprint(w, `{"code":0,"data":{"b_3":"device3","b_4":"device4"}}`)
		case "/x/internal/gaia-gateway/ExClimbWuzhi":
			fmt.Fprint(w, `{"code":0}`)
		case "/x/web-interface/view":
			fmt.Fprint(w, `{"code":0,"data":{"bvid":"BV1ABcsztEcY","title":"demo","duration":700,"owner":{"mid":1,"name":"up"},"stat":{"view":9}}}`)
		case "/x/player/pagelist":
			fmt.Fprint(w, `{"code":0,"data":[{"page":1,"cid":41,"part":"first","duration":60},{"page":2,"cid":42,"part":"second","duration":700}]}`)
		case "/bilibili.community.service.dm.v1.DM/DmSegMobile":
			if capture != nil {
				*capture = append(*capture, r.URL.Path)
			}
			body := new(bytes.Buffer)
			if _, err := body.ReadFrom(r.Body); err != nil {
				t.Errorf("read grpc request: %v", err)
				return
			}
			raw := body.Bytes()
			if len(raw) < 5 {
				t.Errorf("grpc frame too short: %d", len(raw))
				return
			}
			payload := raw[5:]
			segmentIndex := uint64(0)
			remaining := payload
			for len(remaining) > 0 {
				number, wireType, tagLength := protowire.ConsumeTag(remaining)
				if tagLength < 0 {
					t.Errorf("invalid tag in grpc request")
					return
				}
				remaining = remaining[tagLength:]
				if wireType != protowire.VarintType {
					t.Errorf("unexpected wire type %d in request", wireType)
					return
				}
				value, valueLength := protowire.ConsumeVarint(remaining)
				if valueLength < 0 {
					t.Errorf("invalid varint in grpc request")
					return
				}
				remaining = remaining[valueLength:]
				if number == 4 {
					segmentIndex = value
				}
			}
			w.Header().Set("Grpc-Status", "0")
			switch segmentIndex {
			case 1:
				w.Write(danmakuGRPCSegmentReply(t, []struct {
					id       int64
					progress int
					mode     int
					content  string
				}{
					{id: 1, progress: 1000, mode: 1, content: "滚动弹幕"},
					{id: 2, progress: 2000, mode: 5, content: "顶部弹幕"},
				}))
			case 2:
				w.Write(danmakuGRPCSegmentReply(t, []struct {
					id       int64
					progress int
					mode     int
					content  string
				}{
					{id: 3, progress: 400000, mode: 4, content: "底部弹幕"},
				}))
			default:
				w.Header().Set("Grpc-Status", "2")
				w.Header().Set("Bili-Status-Code", "-304")
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
}

func newDanmakuTestApp(t *testing.T, server *httptest.Server, stdout *bytes.Buffer) *App {
	t.Helper()
	app := newTestApp(t)
	app.API.BaseURL = server.URL
	app.API.AppBaseURL = server.URL
	app.API.HTTP = server.Client()
	app.Out = &output.Writer{Stdout: stdout, Stderr: &bytes.Buffer{}, DefaultMode: "rich"}
	return app
}

// newDenseDanmakuTestServer 让 P2 的段 1 返回 count 条弹幕, 用于验证默认预览上限是否生效.
func newDenseDanmakuTestServer(t *testing.T, count int) *httptest.Server {
	t.Helper()
	entries := make([]struct {
		id       int64
		progress int
		mode     int
		content  string
	}, 0, count)
	for index := 0; index < count; index++ {
		entries = append(entries, struct {
			id       int64
			progress int
			mode     int
			content  string
		}{
			id:       int64(index + 1),
			progress: index * 100,
			mode:     1,
			content:  fmt.Sprintf("弹幕-%03d", index+1),
		})
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/frontend/finger/spi":
			fmt.Fprint(w, `{"code":0,"data":{"b_3":"device3","b_4":"device4"}}`)
		case "/x/internal/gaia-gateway/ExClimbWuzhi":
			fmt.Fprint(w, `{"code":0}`)
		case "/x/web-interface/view":
			fmt.Fprint(w, `{"code":0,"data":{"bvid":"BV1ABcsztEcY","title":"demo","duration":700,"owner":{"mid":1,"name":"up"},"stat":{"view":9}}}`)
		case "/x/player/pagelist":
			fmt.Fprint(w, `{"code":0,"data":[{"page":1,"cid":41,"part":"first","duration":60},{"page":2,"cid":42,"part":"second","duration":700}]}`)
		case "/bilibili.community.service.dm.v1.DM/DmSegMobile":
			w.Header().Set("Grpc-Status", "0")
			w.Write(danmakuGRPCSegmentReply(t, entries))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
}

func TestDanmakuCommandExportsAssAndXML(t *testing.T) {
	server := newDanmakuTestServer(t, nil)
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	outputDir := t.TempDir()
	root := NewRoot(app)
	root.SetArgs([]string{
		"video", "danmaku", "https://www.bilibili.com/video/BV1ABcsztEcY?p=2",
		"--format", "ass,xml", "-o", outputDir,
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 多分P视频使用 P2 的标题作为基名.
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("exported files = %d, want 2: %v", len(entries), entries)
	}
	var assPath, xmlPath string
	for _, entry := range entries {
		switch filepath.Ext(entry.Name()) {
		case ".ass":
			assPath = filepath.Join(outputDir, entry.Name())
		case ".xml":
			xmlPath = filepath.Join(outputDir, entry.Name())
		}
	}
	if assPath == "" || xmlPath == "" {
		t.Fatalf("missing export: ass=%q xml=%q", assPath, xmlPath)
	}
	assContent, err := os.ReadFile(assPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(assContent), "[Script Info]") || !strings.Contains(string(assContent), "[Events]") {
		t.Fatalf("ass file missing sections")
	}
	if count := strings.Count(string(assContent), "Dialogue:"); count != 3 {
		t.Fatalf("ass dialogue count = %d, want 3", count)
	}
	xmlContent, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		XMLName  xml.Name `xml:"i"`
		ChatID   int64    `xml:"chatid"`
		Comments []struct {
			Parameters string `xml:"p,attr"`
			Content    string `xml:",chardata"`
		} `xml:"d"`
	}
	if err := xml.Unmarshal(xmlContent, &document); err != nil {
		t.Fatalf("exported xml is not parseable: %v", err)
	}
	// P2 的 cid 是 42.
	if document.ChatID != 42 {
		t.Fatalf("chatid = %d, want 42", document.ChatID)
	}
	if len(document.Comments) != 3 {
		t.Fatalf("xml comment count = %d, want 3", len(document.Comments))
	}
	if document.Comments[0].Content != "滚动弹幕" {
		t.Fatalf("first comment = %q", document.Comments[0].Content)
	}
	if !strings.Contains(stdout.String(), "弹幕数\t3") {
		t.Fatalf("summary missing danmaku count: %q", stdout.String())
	}
}

func TestDanmakuCommandRejectsUnknownFormat(t *testing.T) {
	server := newDanmakuTestServer(t, nil)
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	root := NewRoot(app)
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY?p=2", "--format", "srt"})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected failure for unsupported format")
	}
	// 格式校验必须发生在请求之前, 否则会先报分P错误而不是格式错误.
	if !strings.Contains(err.Error(), "--format") {
		t.Fatalf("format validation did not run first: %v", err)
	}
	if !strings.Contains(err.Error(), "ass") || !strings.Contains(err.Error(), "xml") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDanmakuCommandRequiresPageForMultiPartVideo(t *testing.T) {
	server := newDanmakuTestServer(t, nil)
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	root := NewRoot(app)
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY"})
	// 与 video subtitle 一致: 未指定分P时列出分P并以错误结束, 避免误导出第一P.
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("multi-part video without p parameter should fail")
	}
	if !strings.Contains(err.Error(), "p 参数") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "请先选择要下载的分P") {
		t.Fatalf("multi-part hint missing: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "bili video danmaku BV1ABcsztEcY?p=2") {
		t.Fatalf("example command missing: %q", stdout.String())
	}
}

func TestDanmakuSendBlockedInReadOnlyMode(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		fmt.Fprint(w, `{"code":0,"data":{}}`)
	}))
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	app.Config.Safety.ReadOnly = true
	root := NewRoot(app)
	root.SetArgs([]string{"video", "danmaku", "send", "BV1ABcsztEcY", "测试", "--progress", "10"})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected read-only rejection")
	}
	if !strings.Contains(err.Error(), "只读模式已启用") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("read-only mode should not send requests, got %v", requests)
	}
}

func TestParseDanmakuProgress(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"12.5":    12500,
		"12.5s":   12500,
		"1m20s":   80000,
		"1h2m3s":  3723000,
		"00:12.5": 12500,
		"1:20:00": 4800000,
		"0:01":    1000,
		"90":      90000,
	}
	for input, expected := range cases {
		got, err := parseDanmakuProgress(input)
		if err != nil {
			t.Fatalf("parseDanmakuProgress(%q) error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("parseDanmakuProgress(%q) = %d, want %d", input, got, expected)
		}
	}
	for _, input := range []string{"-1", "abc", "1x", "1:2:3:4", "1:xx"} {
		if _, err := parseDanmakuProgress(input); err == nil {
			t.Fatalf("parseDanmakuProgress(%q) should fail", input)
		}
	}
}

func TestParseDanmakuColorAndMode(t *testing.T) {
	colors := map[string]int{
		"#FF0000":  0xFF0000,
		"ff0000":   0xFF0000,
		"":         0xFFFFFF,
		"16711680": 0xFF0000,
		"0":        0,
	}
	for input, expected := range colors {
		got, err := parseDanmakuColor(input)
		if err != nil {
			t.Fatalf("parseDanmakuColor(%q) error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("parseDanmakuColor(%q) = %d, want %d", input, got, expected)
		}
	}
	for _, input := range []string{"#GGGGGG", "-1", "99999999"} {
		if _, err := parseDanmakuColor(input); err == nil {
			t.Fatalf("parseDanmakuColor(%q) should fail", input)
		}
	}
	modes := map[string]int{
		"scroll": api.DanmakuModeScroll,
		"top":    api.DanmakuModeTop,
		"bottom": api.DanmakuModeBottom,
		"1":      api.DanmakuModeScroll,
		"5":      api.DanmakuModeTop,
		"4":      api.DanmakuModeBottom,
	}
	for input, expected := range modes {
		got, err := parseDanmakuMode(input)
		if err != nil {
			t.Fatalf("parseDanmakuMode(%q) error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("parseDanmakuMode(%q) = %d, want %d", input, got, expected)
		}
	}
	if _, err := parseDanmakuMode("reverse"); err == nil {
		t.Fatal("parseDanmakuMode should reject unsupported mode")
	}
}

func TestNormalizeDanmakuFormats(t *testing.T) {
	formats, err := normalizeDanmakuFormats([]string{"ass", "xml,ass"})
	if err != nil {
		t.Fatal(err)
	}
	if len(formats) != 2 || formats[0] != "ass" || formats[1] != "xml" {
		t.Fatalf("formats = %v", formats)
	}
	formats, err = normalizeDanmakuFormats(nil)
	if err != nil || len(formats) != 1 || formats[0] != "ass" {
		t.Fatalf("default formats = %v, %v", formats, err)
	}
	if _, err := normalizeDanmakuFormats([]string{"srt"}); err == nil {
		t.Fatal("normalizeDanmakuFormats should reject srt")
	}
}

func TestDanmakuTimeLabel(t *testing.T) {
	// 这里的时间戳语义是毫秒, 时间标签曾把十分之一秒误当成秒.
	cases := map[int]string{
		0:       "0:00.0",
		100:     "0:00.1",
		1000:    "0:01.0",
		60000:   "1:00.0",
		360000:  "6:00.0",
		678400:  "11:18.4",
		719999:  "11:59.9",
		3600000: "1:00:00.0",
	}
	for input, expected := range cases {
		if got := danmakuTimeLabel(input); got != expected {
			t.Fatalf("danmakuTimeLabel(%d) = %q, want %q", input, got, expected)
		}
	}
	if got := danmakuTimeLabel(-5); got != "0:00.0" {
		t.Fatalf("negative label = %q", got)
	}
}

func TestParseDanmakuRange(t *testing.T) {
	from, to, err := parseDanmakuRange("1m20s", "2:00")
	if err != nil {
		t.Fatal(err)
	}
	if from != 80000 || to != 120000 {
		t.Fatalf("range = [%d, %d], want [80000, 120000]", from, to)
	}
	// 未设置的一侧返回 -1, 表示不限制.
	from, to, err = parseDanmakuRange("", "")
	if err != nil {
		t.Fatal(err)
	}
	if from != -1 || to != -1 {
		t.Fatalf("unset range = [%d, %d], want [-1, -1]", from, to)
	}
	from, to, err = parseDanmakuRange("30s", "")
	if err != nil || from != 30000 || to != -1 {
		t.Fatalf("from-only range = [%d, %d], %v", from, to, err)
	}
	if _, _, err := parseDanmakuRange("5:00", "1:00"); err == nil {
		t.Fatal("inverted range should fail")
	}
	if _, _, err := parseDanmakuRange("abc", ""); err == nil {
		t.Fatal("invalid from should fail")
	}
}

func TestDanmakuCommandSegmentSelectsRange(t *testing.T) {
	server := newDanmakuTestServer(t, nil)
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	root := NewRoot(app)
	// P2 时长 700 秒, 共 2 个分段; 只请求第 2 段.
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY?p=2", "--segment", "2"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, "2/2 (6:00.0 - 11:40.0)") {
		t.Fatalf("segment range missing or wrong: %q", output)
	}
	// 该分段只有底部弹幕 1 条.
	if !strings.Contains(output, "弹幕数\t1") {
		t.Fatalf("segment item count wrong: %q", output)
	}
	if !strings.Contains(output, "底部\t1") {
		t.Fatalf("segment mode breakdown wrong: %q", output)
	}
}

func TestDanmakuCommandRejectsOutOfRangeSegment(t *testing.T) {
	server := newDanmakuTestServer(t, nil)
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	root := NewRoot(app)
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY?p=2", "--segment", "9"})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("out of range segment should fail")
	}
	if !strings.Contains(err.Error(), "超出范围") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDanmakuCommandFiltersByTimeRange(t *testing.T) {
	server := newDanmakuTestServer(t, nil)
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	root := NewRoot(app)
	// P2 的弹幕: 段 1 在 1s 和 2s, 段 2 在 400s (6:40).
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY?p=2", "--from", "1s", "--to", "3s", "--limit", "0"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, "弹幕数\t2") {
		t.Fatalf("time filter did not narrow the set: %q", output)
	}
	if strings.Contains(output, "底部弹幕") {
		t.Fatalf("out-of-range danmaku leaked through: %q", output)
	}
	// 反过来只取后面的那一条.
	stdout.Reset()
	root = NewRoot(app)
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY?p=2", "--from", "6m", "--limit", "0"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	output = stdout.String()
	if !strings.Contains(output, "弹幕数\t1") || !strings.Contains(output, "底部弹幕") {
		t.Fatalf("from-only filter wrong: %q", output)
	}
}

func TestResolveDanmakuLimits(t *testing.T) {
	newCommand := func() *cobra.Command {
		return newDanmakuCommand(newTestApp(t))
	}
	// 默认: 终端预览 20 条, 结构化输出不截断.
	command := newCommand()
	payloadLimit, previewLimit := resolveDanmakuLimits(command, 0, false)
	if payloadLimit != 0 || previewLimit != danmakuPreviewLimit {
		t.Fatalf("default limits = (%d, %d)", payloadLimit, previewLimit)
	}
	// 用了时间或分段: 两者都不截断, 不再附带隐式预览上限.
	payloadLimit, previewLimit = resolveDanmakuLimits(command, 0, true)
	if payloadLimit != 0 || previewLimit != 0 {
		t.Fatalf("range limits = (%d, %d), want (0, 0)", payloadLimit, previewLimit)
	}
	// 显式 --limit 优先于范围推断.
	command = newCommand()
	if err := command.Flags().Set("limit", "5"); err != nil {
		t.Fatal(err)
	}
	payloadLimit, previewLimit = resolveDanmakuLimits(command, 5, true)
	if payloadLimit != 5 || previewLimit != 5 {
		t.Fatalf("explicit limit = (%d, %d), want (5, 5)", payloadLimit, previewLimit)
	}
	// 显式 --limit 0 表示不截断, 即使没有范围参数.
	command = newCommand()
	if err := command.Flags().Set("limit", "0"); err != nil {
		t.Fatal(err)
	}
	payloadLimit, previewLimit = resolveDanmakuLimits(command, 0, false)
	if payloadLimit != 0 || previewLimit != 0 {
		t.Fatalf("explicit zero limit = (%d, %d), want (0, 0)", payloadLimit, previewLimit)
	}
}

func TestDanmakuCommandTimeRangeBypassesPreviewLimit(t *testing.T) {
	// 段 1 有 50 条弹幕, 全部落在前 5 秒, 因此默认预览上限必然会触发.
	server := newDenseDanmakuTestServer(t, 50)
	defer server.Close()
	rangeArgs := []string{"video", "danmaku", "BV1ABcsztEcY?p=2", "--from", "0s", "--to", "10s"}

	// 未加时间筛选时, 预览被默认上限截断.
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	root := NewRoot(app)
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY?p=2"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "已预览 20 条") {
		t.Fatalf("default preview limit not applied: %q", stdout.String())
	}

	// 加了时间筛选后不再截断, 也不应再出现截断提示.
	stdout.Reset()
	app = newDanmakuTestApp(t, server, stdout)
	root = NewRoot(app)
	root.SetArgs(rangeArgs)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, "弹幕数\t50") {
		t.Fatalf("unexpected item count: %q", output)
	}
	if strings.Contains(output, "已预览") {
		t.Fatalf("time range should not report truncation: %q", output)
	}
	// 最后一条必须出现, 说明确实没有截断.
	if !strings.Contains(output, "弹幕-050") {
		t.Fatalf("last danmaku was truncated: %q", output)
	}

	// 显式 --limit 仍然优先于范围推断.
	stdout.Reset()
	app = newDanmakuTestApp(t, server, stdout)
	root = NewRoot(app)
	root.SetArgs(append(append([]string{}, rangeArgs...), "--limit", "5"))
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "已预览 5 条") {
		t.Fatalf("explicit --limit ignored: %q", stdout.String())
	}
}

func TestDanmakuCommandJSONLimitStillTruncates(t *testing.T) {
	server := newDanmakuTestServer(t, nil)
	defer server.Close()
	stdout := &bytes.Buffer{}
	app := newDanmakuTestApp(t, server, stdout)
	root := NewRoot(app)
	root.SetArgs([]string{"video", "danmaku", "BV1ABcsztEcY?p=2", "--json", "--limit", "1"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Data struct {
			Total int   `json:"total"`
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Total != 3 {
		t.Fatalf("total = %d, want 3", payload.Data.Total)
	}
	if len(payload.Data.Items) != 1 {
		t.Fatalf("items = %d, want 1 (explicit --limit)", len(payload.Data.Items))
	}
}
