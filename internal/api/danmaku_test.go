package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// buildDanmakuSegmentReply 按服务端的 proto 结构手工构造一个 DmSegMobileReply 负载.
func buildDanmakuSegmentReply(t *testing.T, items []DanmakuItem) []byte {
	t.Helper()
	payload := make([]byte, 0, 128)
	for _, item := range items {
		element := make([]byte, 0, 64)
		appendVarint := func(number protowire.Number, value uint64) {
			element = protowire.AppendTag(element, number, protowire.VarintType)
			element = protowire.AppendVarint(element, value)
		}
		appendString := func(number protowire.Number, value string) {
			element = protowire.AppendTag(element, number, protowire.BytesType)
			element = protowire.AppendString(element, value)
		}
		appendVarint(1, uint64(item.ID))
		appendVarint(2, uint64(item.Progress))
		appendVarint(3, uint64(item.Mode))
		appendVarint(4, uint64(item.FontSize))
		appendVarint(5, uint64(item.Color))
		appendString(6, item.MidHash)
		appendString(7, item.Content)
		appendVarint(8, uint64(item.CTime))
		appendVarint(9, uint64(item.Weight))
		if item.Pool != 0 {
			appendVarint(11, uint64(item.Pool))
		}
		appendString(12, item.IDStr)
		appendVarint(13, uint64(item.Attr))
		appendVarint(15, uint64(item.LikeCount))
		if item.Colorful != 0 {
			appendVarint(24, uint64(item.Colorful))
		}
		appendVarint(27, uint64(item.DmFrom))
		// 追加一个未知字段, 验证解析器会按 wire type 跳过.
		element = protowire.AppendTag(element, 999, protowire.BytesType)
		element = protowire.AppendString(element, "future")
		payload = protowire.AppendTag(payload, 1, protowire.BytesType)
		payload = protowire.AppendBytes(payload, element)
	}
	// 追加一个顶层未知字段.
	payload = protowire.AppendTag(payload, 9, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 7)
	return payload
}

func TestEncodeDanmakuSegmentRequest(t *testing.T) {
	encoded := encodeDanmakuSegmentRequest(35980968674, 3)
	fields := make(map[protowire.Number]uint64)
	remaining := encoded
	for len(remaining) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(remaining)
		if tagLength < 0 {
			t.Fatalf("invalid tag in encoded request")
		}
		remaining = remaining[tagLength:]
		if wireType != protowire.VarintType {
			t.Fatalf("unexpected wire type %d", wireType)
		}
		value, valueLength := protowire.ConsumeVarint(remaining)
		if valueLength < 0 {
			t.Fatalf("invalid varint in encoded request")
		}
		remaining = remaining[valueLength:]
		fields[number] = value
	}
	if fields[2] != 35980968674 {
		t.Fatalf("oid = %d, want 35980968674", fields[2])
	}
	if fields[3] != 1 {
		t.Fatalf("type = %d, want 1", fields[3])
	}
	if fields[4] != 3 {
		t.Fatalf("segmentIndex = %d, want 3", fields[4])
	}
}

func TestDecodeGRPCFrames(t *testing.T) {
	payload := []byte("hello danmaku")
	frame := wrapGRPCFrame(payload)
	if frame[0] != 0 {
		t.Fatalf("frame flag = %d, want 0", frame[0])
	}
	decoded, err := decodeGRPCFrames(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded = %q", decoded)
	}

	// gzip 压缩帧.
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	gzipFrame := wrapGRPCFrame(compressed.Bytes())
	gzipFrame[0] = 1
	decoded, err = decodeGRPCFrames(gzipFrame)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("gzip decoded = %q", decoded)
	}

	// 长度与实际不符.
	broken := append([]byte(nil), frame...)
	broken[4] = 0xff
	if _, err := decodeGRPCFrames(broken); CodeOf(err) != CodeUpstream {
		t.Fatalf("broken frame error code = %q", CodeOf(err))
	}
	// 长度不足.
	if _, err := decodeGRPCFrames([]byte{0, 0}); CodeOf(err) != CodeUpstream {
		t.Fatalf("short frame error code = %q", CodeOf(err))
	}
}

func TestDecodeDanmakuSegment(t *testing.T) {
	items := []DanmakuItem{
		{ID: 1, Progress: 390, Mode: 1, FontSize: 25, Color: 0x00FF00, MidHash: "abc", Content: "第一条", CTime: 1770800435, Weight: 10, IDStr: "1", DmFrom: 1},
		{ID: 2, Progress: 9100, Mode: 5, FontSize: 18, Color: 0xFFFFFF, MidHash: "def", Content: "顶部弹幕", CTime: 1770800474, Weight: 2, IDStr: "2", LikeCount: 89, Colorful: 60001, DmFrom: 1},
	}
	decoded, err := decodeDanmakuSegment(buildDanmakuSegmentReply(t, items))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded count = %d, want 2", len(decoded))
	}
	first := decoded[0]
	if first.ID != 1 || first.Progress != 390 || first.Content != "第一条" || first.Mode != 1 {
		t.Fatalf("unexpected first item: %#v", first)
	}
	if first.Color != 0x00FF00 || first.FontSize != 25 {
		t.Fatalf("unexpected first item style: %#v", first)
	}
	second := decoded[1]
	if second.Mode != 5 || second.LikeCount != 89 || second.Colorful != 60001 || second.FontSize != 18 {
		t.Fatalf("unexpected second item: %#v", second)
	}
}

func TestDecodeDanmakuSegmentFillsMissingIDStr(t *testing.T) {
	items := []DanmakuItem{{ID: 12345, Progress: 1, Mode: 1, FontSize: 25, Content: "x"}}
	decoded, err := decodeDanmakuSegment(buildDanmakuSegmentReply(t, items))
	if err != nil {
		t.Fatal(err)
	}
	if decoded[0].IDString() != "12345" {
		t.Fatalf("IDString = %q", decoded[0].IDString())
	}
}

func TestGetVideoDanmakuMergesSegments(t *testing.T) {
	segmentOne := buildDanmakuSegmentReply(t, []DanmakuItem{
		{ID: 2, Progress: 5000, Mode: 1, FontSize: 25, Color: 0xFFFFFF, Content: "later", IDStr: "2"},
		{ID: 1, Progress: 1000, Mode: 1, FontSize: 25, Color: 0xFFFFFF, Content: "earlier", IDStr: "1"},
	})
	segmentTwo := buildDanmakuSegmentReply(t, []DanmakuItem{
		{ID: 3, Progress: 400000, Mode: 5, FontSize: 25, Color: 0xFFFFFF, Content: "second segment", IDStr: "3"},
	})
	requested := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != grpcDanmakuPath {
			t.Errorf("unexpected path: %s", r.URL.Path)
			return
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/grpc" {
			t.Errorf("content type = %q", contentType)
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(r.Body); err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		payload, err := decodeGRPCFrames(body.Bytes())
		if err != nil {
			t.Errorf("decode request frame: %v", err)
			return
		}
		var segmentIndex uint64
		remaining := payload
		for len(remaining) > 0 {
			number, wireType, tagLength := protowire.ConsumeTag(remaining)
			if tagLength < 0 {
				break
			}
			remaining = remaining[tagLength:]
			if wireType != protowire.VarintType {
				break
			}
			value, valueLength := protowire.ConsumeVarint(remaining)
			if valueLength < 0 {
				break
			}
			remaining = remaining[valueLength:]
			if number == 4 {
				segmentIndex = value
			}
		}
		requested = append(requested, fmt.Sprintf("%d", segmentIndex))
		switch segmentIndex {
		case 1:
			w.Header().Set("Grpc-Status", "0")
			w.Write(wrapGRPCFrame(segmentOne))
		case 2:
			w.Header().Set("Grpc-Status", "0")
			w.Write(wrapGRPCFrame(segmentTwo))
		default:
			// 越界分段: 服务端返回 -304 与空 body.
			w.Header().Set("Grpc-Status", "2")
			w.Header().Set("Bili-Status-Code", grpcOutOfRangeStatus)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.AppBaseURL = server.URL
	client.HTTP = server.Client()
	// 时长略少于 2 个分段, 让第三个分段成为越界分段.
	result, err := client.GetVideoDanmaku(context.Background(), 42, 2*danmakuSegmentLengthMs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Segments != 2 {
		t.Fatalf("segments = %d, want 2", result.Segments)
	}
	if len(result.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(result.Items))
	}
	if result.Items[0].Content != "earlier" || result.Items[1].Content != "later" {
		t.Fatalf("items are not sorted by progress: %#v", result.Items)
	}
	if result.Items[2].Content != "second segment" {
		t.Fatalf("second segment missing: %#v", result.Items)
	}
	if len(requested) != 2 {
		t.Fatalf("requested segments = %v, want 2 requests", requested)
	}
}

func TestGetVideoDanmakuOutOfRangeSegmentIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Grpc-Status", "2")
		w.Header().Set("Bili-Status-Code", grpcOutOfRangeStatus)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := NewClient()
	client.AppBaseURL = server.URL
	client.HTTP = server.Client()
	items, err := client.GetDanmakuSegment(context.Background(), 42, 99, nil)
	if err != nil {
		t.Fatalf("out of range segment should not fail: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("out of range segment returned %d items", len(items))
	}
}

func TestGetVideoDanmakuRejectsTooManySegments(t *testing.T) {
	client := NewClient()
	_, err := client.GetVideoDanmaku(context.Background(), 42, (danmakuMaxSegments+1)*danmakuSegmentLengthMs, nil)
	if CodeOf(err) != CodeInvalidInput {
		t.Fatalf("error code = %q, want %q", CodeOf(err), CodeInvalidInput)
	}
}

func TestFormatDanmakuXML(t *testing.T) {
	items := []DanmakuItem{
		{ID: 1, Progress: 390, Mode: 1, FontSize: 25, Color: 16777215, CTime: 1770800435, Pool: 0, MidHash: "7e05bfe3", IDStr: "2044381239393894912", Weight: 2, Content: "刚刚"},
		{ID: 2, Progress: 6251, Mode: 5, FontSize: 18, Color: 15138834, CTime: 1770800441, MidHash: "4bd64778", IDStr: "2044381290186837248", Weight: 3, Content: `含有 <标签> & "引号"`},
	}
	rendered := FormatDanmakuXML(35980968674, items)
	if !strings.HasPrefix(rendered, `<?xml version="1.0" encoding="UTF-8"?><i>`) {
		t.Fatalf("unexpected xml header: %q", rendered[:60])
	}
	var document struct {
		XMLName  xml.Name `xml:"i"`
		ChatID   int64    `xml:"chatid"`
		Comments []struct {
			Parameters string `xml:"p,attr"`
			Content    string `xml:",chardata"`
		} `xml:"d"`
	}
	if err := xml.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatal(err)
	}
	if document.ChatID != 35980968674 {
		t.Fatalf("chatid = %d", document.ChatID)
	}
	if len(document.Comments) != 2 {
		t.Fatalf("comments = %d", len(document.Comments))
	}
	fields := strings.Split(document.Comments[0].Parameters, ",")
	if len(fields) != 9 {
		t.Fatalf("parameter fields = %d, want 9: %q", len(fields), document.Comments[0].Parameters)
	}
	if fields[0] != "0.390" || fields[1] != "1" || fields[2] != "25" || fields[3] != "16777215" {
		t.Fatalf("unexpected parameters: %v", fields)
	}
	if fields[6] != "7e05bfe3" || fields[7] != "2044381239393894912" {
		t.Fatalf("unexpected identity fields: %v", fields)
	}
	if document.Comments[0].Content != "刚刚" {
		t.Fatalf("content = %q", document.Comments[0].Content)
	}
	if document.Comments[1].Content != `含有 <标签> & "引号"` {
		t.Fatalf("escaped content = %q", document.Comments[1].Content)
	}
	if !strings.Contains(document.Comments[1].Parameters, ",18,") {
		t.Fatalf("font size not preserved: %q", document.Comments[1].Parameters)
	}
}

func TestFormatDanmakuASS(t *testing.T) {
	items := []DanmakuItem{
		{ID: 1, Progress: 0, Mode: 1, FontSize: 25, Color: 0xFFFFFF, Content: "滚动"},
		{ID: 2, Progress: 0, Mode: 1, FontSize: 25, Color: 0xFFFFFF, Content: "同时滚动"},
		{ID: 3, Progress: 100, Mode: 5, FontSize: 25, Color: 0xFF0000, Content: "顶部"},
		{ID: 4, Progress: 200, Mode: 4, FontSize: 25, Color: 0xFFFFFF, Content: "底部"},
		{ID: 5, Progress: 300, Mode: 8, FontSize: 25, Color: 0xFFFFFF, Content: "代码弹幕"},
		{ID: 6, Progress: 400, Mode: 1, FontSize: 25, Color: 0xFFFFFF, Content: `花括号{和}反斜杠\与前导空格`},
		{ID: 7, Progress: 500, Mode: 1, FontSize: 25, Color: 0x000000, Content: " 前后空格 "},
	}
	rendered, warnings := FormatDanmakuASS(items, DanmakuASSOptions{Title: "probe", FontFace: "sans-serif", FontSize: 56})
	if !strings.Contains(rendered, "[Script Info]") || !strings.Contains(rendered, "[Events]") {
		t.Fatalf("missing ass sections")
	}
	lines := make([]string, 0)
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "Dialogue:") {
			lines = append(lines, line)
		}
	}
	// 代码弹幕被跳过, 其余 6 条保留.
	if len(lines) != 6 {
		t.Fatalf("dialogue lines = %d, want 6", len(lines))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "代码弹幕") {
		t.Fatalf("warnings = %v", warnings)
	}
	// 同时刻的两条滚动弹幕必须落在不同行, 避免重叠.
	firstRow := assDialogueRow(t, lines[0])
	secondRow := assDialogueRow(t, lines[1])
	if firstRow == secondRow {
		t.Fatalf("simultaneous scrolling danmaku share row %s", firstRow)
	}
	// 顶部使用 \an8, 底部使用 \an2.
	if !strings.Contains(rendered, `\an8\pos(`) {
		t.Fatalf("top danmaku missing \\an8: %s", rendered)
	}
	if !strings.Contains(rendered, `\an2\pos(`) {
		t.Fatalf("bottom danmaku missing \\an2: %s", rendered)
	}
	// 红色弹幕需要输出 ASS 颜色, 顺序为 BBGGRR.
	if !strings.Contains(rendered, `\c&H0000FF&`) {
		t.Fatalf("red danmaku color missing: %s", rendered)
	}
	// 黑色弹幕需要白色描边, 否则不可见.
	if !strings.Contains(rendered, `\3c&HFFFFFF&`) {
		t.Fatalf("black danmaku outline missing: %s", rendered)
	}
	// 特殊字符转义.
	if !strings.Contains(rendered, `\{和\}`) {
		t.Fatalf("braces not escaped: %s", rendered)
	}
	if !strings.Contains(rendered, "\\\u200b") {
		t.Fatalf("backslash not escaped: %s", rendered)
	}
	if !strings.Contains(rendered, "\u200b 前后空格") {
		t.Fatalf("leading space not protected: %s", rendered)
	}
	// 字号不等于基准字号时需要显式 \fs.
	custom, _ := FormatDanmakuASS([]DanmakuItem{{ID: 1, Progress: 0, Mode: 1, FontSize: 18, Color: 0xFFFFFF, Content: "小字"}}, DanmakuASSOptions{FontSize: 56})
	if !strings.Contains(custom, `\fs40`) {
		t.Fatalf("custom font size missing: %s", custom)
	}
}

func assDialogueRow(t *testing.T, line string) string {
	t.Helper()
	start := strings.Index(line, `\move(`)
	if start < 0 {
		start = strings.Index(line, `\pos(`)
	}
	if start < 0 {
		t.Fatalf("dialogue has no positioning tag: %s", line)
	}
	rest := line[start:]
	parts := strings.Split(strings.TrimSuffix(strings.SplitN(rest, ")", 2)[0], ","), ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	return last
}

func TestFormatDanmakuASSSortsByLaneWithoutOverlapOnSparseInput(t *testing.T) {
	items := make([]DanmakuItem, 0, 8)
	for index := 0; index < 8; index++ {
		items = append(items, DanmakuItem{
			ID:       int64(index + 1),
			Progress: index * 3000,
			Mode:     1,
			FontSize: 25,
			Color:    0xFFFFFF,
			Content:  "字",
		})
	}
	rendered, warnings := FormatDanmakuASS(items, DanmakuASSOptions{FontSize: 56, ScrollDuration: 5})
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	rows := make(map[string]struct{})
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(line, "Dialogue:") {
			rows[assDialogueRow(t, line)] = struct{}{}
		}
	}
	// 稀疏输入下同一行可以复用, 但不应退化为每条一行.
	if len(rows) == 0 || len(rows) > 8 {
		t.Fatalf("unexpected lane count %d", len(rows))
	}
}

func TestPostVideoDanmakuForm(t *testing.T) {
	var received map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/frontend/finger/spi":
			fmt.Fprint(w, `{"code":0,"data":{"b_3":"device3","b_4":"device4"}}`)
		case "/x/internal/gaia-gateway/ExClimbWuzhi":
			fmt.Fprint(w, `{"code":0}`)
		case "/x/v2/dm/post":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			received = map[string][]string(r.Form)
			fmt.Fprint(w, `{"code":0,"data":{"dmid":"123456789","dmid_str":"123456789"}}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient()
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	credential := &Credential{Sessdata: "x", BiliJct: "token", AccessKey: "key", DedeUserID: "7"}
	result, err := client.PostVideoDanmaku(context.Background(), DanmakuPostRequest{
		BVID:     "BV1ABcsztEcY",
		OID:      35980968674,
		Content:  "测试弹幕",
		Progress: 12500,
		Mode:     DanmakuModeTop,
		Color:    0xFF0000,
		FontSize: 25,
	}, credential)
	if err != nil {
		t.Fatal(err)
	}
	if result.DMID != "123456789" {
		t.Fatalf("dmid = %q", result.DMID)
	}
	expected := map[string]string{
		"type":     "1",
		"oid":      "35980968674",
		"bvid":     "BV1ABcsztEcY",
		"msg":      "测试弹幕",
		"mode":     "5",
		"progress": "12500",
		"color":    "16711680",
		"fontsize": "25",
		"csrf":     "token",
	}
	for key, value := range expected {
		if got := firstFormValue(received, key); got != value {
			t.Fatalf("form[%s] = %q, want %q", key, got, value)
		}
	}
	if firstFormValue(received, "rnd") == "" {
		t.Fatalf("rnd is empty")
	}
}

func TestPostVideoDanmakuValidatesInput(t *testing.T) {
	client := NewClient()
	credential := &Credential{Sessdata: "x", BiliJct: "token"}
	cases := map[string]DanmakuPostRequest{
		"empty content": {BVID: "BV1ABcsztEcY", OID: 1, Content: "   "},
		"missing cid":   {BVID: "BV1ABcsztEcY", Content: "x"},
		"missing bvid":  {OID: 1, Content: "x"},
		"long content":  {BVID: "BV1ABcsztEcY", OID: 1, Content: strings.Repeat("字", danmakuMaxContentRunes+1)},
		"negative time": {BVID: "BV1ABcsztEcY", OID: 1, Content: "x", Progress: -1},
	}
	for name, request := range cases {
		if _, err := client.PostVideoDanmaku(context.Background(), request, credential); CodeOf(err) != CodeInvalidInput {
			t.Fatalf("%s: error code = %q, want %q", name, CodeOf(err), CodeInvalidInput)
		}
	}
	if _, err := client.PostVideoDanmaku(context.Background(), DanmakuPostRequest{BVID: "BV1ABcsztEcY", OID: 1, Content: "x"}, nil); CodeOf(err) != CodeNotAuthenticated {
		t.Fatalf("missing credential error code = %q", CodeOf(err))
	}
}

func firstFormValue(values map[string][]string, key string) string {
	if items, ok := values[key]; ok && len(items) > 0 {
		return items[0]
	}
	return ""
}
