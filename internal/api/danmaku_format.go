package api

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

// 弹幕模式取值, 与服务端 mode 字段一致.
const (
	DanmakuModeScroll   = 1
	DanmakuModeBottom   = 4
	DanmakuModeTop      = 5
	DanmakuModeReverse  = 6
	DanmakuModeAdvanced = 7
	DanmakuModeScript   = 8
	DanmakuModeBAS      = 9
)

// ASS 画布固定为 1920x1080, 播放器会按视频分辨率缩放.
const (
	danmakuASSStageWidth  = 1920
	danmakuASSStageHeight = 1080
)

// DanmakuASSOptions 控制 ASS 导出效果.
type DanmakuASSOptions struct {
	// Title 写入 ASS 头部的 Title 字段.
	Title string
	// FontFace 是 ASS 使用的字体名.
	FontFace string
	// FontSize 是基准字号, 对应服务端 fontsize 为 25 的弹幕.
	FontSize float64
	// ScrollDuration 是滚动弹幕的显示时长, 单位秒.
	ScrollDuration float64
	// StillDuration 是顶部和底部弹幕的显示时长, 单位秒.
	StillDuration float64
	// Alpha 是文字不透明度, 取值 0 到 1.
	Alpha float64
}

// DefaultDanmakuASSOptions 返回与 danmaku2ass 默认行为接近的 ASS 导出参数.
func DefaultDanmakuASSOptions() DanmakuASSOptions {
	return DanmakuASSOptions{
		FontFace:       "sans-serif",
		FontSize:       56,
		ScrollDuration: 8,
		StillDuration:  5,
		Alpha:          1,
	}
}

func (options DanmakuASSOptions) withDefaults() DanmakuASSOptions {
	defaults := DefaultDanmakuASSOptions()
	if strings.TrimSpace(options.FontFace) == "" {
		options.FontFace = defaults.FontFace
	}
	if options.FontSize <= 0 {
		options.FontSize = defaults.FontSize
	}
	if options.ScrollDuration <= 0 {
		options.ScrollDuration = defaults.ScrollDuration
	}
	if options.StillDuration <= 0 {
		options.StillDuration = defaults.StillDuration
	}
	if options.Alpha <= 0 {
		options.Alpha = defaults.Alpha
	}
	if options.Alpha > 1 {
		options.Alpha = 1
	}
	return options
}

// danmakuXMLDocument 是 Bilibili 标准弹幕 XML 的根节点.
type danmakuXMLDocument struct {
	XMLName    xml.Name            `xml:"i"`
	ChatServer string              `xml:"chatserver"`
	ChatID     int64               `xml:"chatid"`
	Mission    int                 `xml:"mission"`
	MaxLimit   int                 `xml:"maxlimit"`
	State      int                 `xml:"state"`
	RealName   int                 `xml:"real_name"`
	Source     string              `xml:"source"`
	Comments   []danmakuXMLComment `xml:"d"`
}

type danmakuXMLComment struct {
	Parameters string `xml:"p,attr"`
	Content    string `xml:",chardata"`
}

// FormatDanmakuXML 把弹幕渲染成 Bilibili 标准 XML.
// p 属性沿用服务端顺序: 时间秒, 模式, 字号, 颜色, 发送时间戳, 弹幕池, 用户 hash, 弹幕 ID, 权重.
func FormatDanmakuXML(cid int64, items []DanmakuItem) string {
	document := danmakuXMLDocument{
		ChatServer: "chat.bilibili.com",
		ChatID:     cid,
		Mission:    0,
		MaxLimit:   0,
		State:      0,
		RealName:   0,
		Source:     "k-v",
		Comments:   make([]danmakuXMLComment, 0, len(items)),
	}
	for _, item := range items {
		parameters := strings.Join([]string{
			strconv.FormatFloat(float64(item.Progress)/1000, 'f', 3, 64),
			strconv.Itoa(item.Mode),
			strconv.Itoa(item.FontSize),
			strconv.Itoa(item.Color),
			strconv.FormatInt(item.CTime, 10),
			strconv.Itoa(item.Pool),
			item.MidHash,
			item.IDString(),
			strconv.Itoa(item.Weight),
		}, ",")
		document.Comments = append(document.Comments, danmakuXMLComment{
			Parameters: parameters,
			Content:    item.Content,
		})
	}
	body, err := xml.MarshalIndent(document, "", "")
	if err != nil {
		return ""
	}
	// Bilibili 官方文件在声明与根节点之间没有换行, 第三方工具据此识别格式.
	return `<?xml version="1.0" encoding="UTF-8"?>` + string(body)
}

// IDString 返回弹幕 ID 的字符串形式, 优先使用服务端下发的 idStr.
func (item DanmakuItem) IDString() string {
	if strings.TrimSpace(item.IDStr) != "" {
		return item.IDStr
	}
	if item.ID == 0 {
		return ""
	}
	return strconv.FormatInt(item.ID, 10)
}

// danmakuASSComment 是 ASS 排版过程中的中间表示.
type danmakuASSComment struct {
	start    float64
	position int
	color    int
	size     float64
	height   float64
	width    float64
	content  string
}

// 排版位置, 对应 danmaku2ass 的 pos 取值.
const (
	assPositionScroll  = 0
	assPositionTop     = 1
	assPositionBottom  = 2
	assPositionReverse = 3
)

// FormatDanmakuASS 把弹幕渲染成 ASS 字幕, 排版算法移植自 danmaku2ass.
func FormatDanmakuASS(items []DanmakuItem, options DanmakuASSOptions) (string, []string) {
	options = options.withDefaults()
	warnings := make([]string, 0)
	comments := make([]danmakuASSComment, 0, len(items))
	advancedCount := 0
	scriptCount := 0
	for _, item := range items {
		position, ok := danmakuASSPosition(item.Mode)
		if !ok {
			if item.Mode == DanmakuModeAdvanced {
				advancedCount++
				// 高级弹幕缺少定位参数解析, 降级为顶部弹幕而不是丢弃.
				position = assPositionTop
			} else {
				scriptCount++
				continue
			}
		}
		size := float64(item.FontSize) * options.FontSize / 25
		if item.FontSize <= 0 {
			size = options.FontSize
		}
		width := float64(runewidth.StringWidth(item.Content)) * size
		height := float64(strings.Count(item.Content, "\n")+1) * size
		comments = append(comments, danmakuASSComment{
			start:    float64(item.Progress) / 1000,
			position: position,
			color:    item.Color,
			size:     size,
			height:   height,
			width:    width,
			content:  item.Content,
		})
	}
	if advancedCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d 条高级弹幕缺少定位参数, 已按顶部弹幕导出", advancedCount))
	}
	if scriptCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d 条代码弹幕不支持导出, 已跳过", scriptCount))
	}
	renderer := newDanmakuASSRenderer(comments, options)
	return renderer.render(), warnings
}

func danmakuASSPosition(mode int) (int, bool) {
	switch mode {
	case DanmakuModeScroll:
		return assPositionScroll, true
	case DanmakuModeTop:
		return assPositionTop, true
	case DanmakuModeBottom:
		return assPositionBottom, true
	case DanmakuModeReverse:
		return assPositionReverse, true
	default:
		return 0, false
	}
}

type danmakuASSRenderer struct {
	comments []danmakuASSComment
	options  DanmakuASSOptions
	rows     [4][]*danmakuASSComment
	styleID  string
}

func newDanmakuASSRenderer(comments []danmakuASSComment, options DanmakuASSOptions) *danmakuASSRenderer {
	renderer := &danmakuASSRenderer{
		comments: comments,
		options:  options,
		styleID:  "Danmaku",
	}
	for index := range renderer.rows {
		renderer.rows[index] = make([]*danmakuASSComment, danmakuASSStageHeight+1)
	}
	return renderer
}

func (renderer *danmakuASSRenderer) render() string {
	var builder strings.Builder
	renderer.writeHeader(&builder)
	for index := range renderer.comments {
		comment := &renderer.comments[index]
		row := 0
		rowMax := danmakuASSStageHeight - int(comment.height)
		placed := false
		for row <= rowMax {
			free := renderer.testFreeRows(comment, row)
			if free >= int(comment.height) {
				renderer.markRow(comment, row)
				renderer.writeComment(&builder, comment, row)
				placed = true
				break
			}
			if free <= 0 {
				free = 1
			}
			row += free
		}
		if !placed {
			// 画面已满, 按 danmaku2ass 的做法让这条弹幕与最空闲的一行重叠.
			row = renderer.findAlternativeRow(comment)
			renderer.markRow(comment, row)
			renderer.writeComment(&builder, comment, row)
		}
	}
	return builder.String()
}

func (renderer *danmakuASSRenderer) writeHeader(builder *strings.Builder) {
	alpha := 255 - int(math.Round(renderer.options.Alpha*255))
	outline := math.Max(renderer.options.FontSize/25, 1)
	title := strings.TrimSpace(renderer.options.Title)
	if title != "" {
		fmt.Fprintf(builder, "[Script Info]\nTitle: %s\n", strings.ReplaceAll(title, "\n", " "))
	} else {
		builder.WriteString("[Script Info]\n")
	}
	fmt.Fprintf(builder, `; Script generated by bili-cli
ScriptType: v4.00+
PlayResX: %d
PlayResY: %d
Aspect Ratio: %d:%d
Collisions: Normal
WrapStyle: 2
ScaledBorderAndShadow: yes
YCbCr Matrix: TV.709

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: %s, %s, %.0f, &H%02XFFFFFF, &H%02XFFFFFF, &H%02X000000, &H%02X000000, 0, 0, 0, 0, 100, 100, 0.00, 0.00, 1, %.0f, 0, 7, 0, 0, 0, 0

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`,
		danmakuASSStageWidth, danmakuASSStageHeight, danmakuASSStageWidth, danmakuASSStageHeight,
		renderer.styleID, renderer.options.FontFace, renderer.options.FontSize,
		alpha, alpha, alpha, alpha, outline)
}

// testFreeRows 移植 danmaku2ass: 返回从该行开始可以连续使用的像素行数.
func (renderer *danmakuASSRenderer) testFreeRows(comment *danmakuASSComment, row int) int {
	result := 0
	target := (*danmakuASSComment)(nil)
	lane := renderer.rows[comment.position]
	for row < danmakuASSStageHeight && result < int(comment.height) {
		current := lane[row]
		if current != target {
			target = current
			if target != nil && renderer.conflicts(comment, target) {
				break
			}
		}
		row++
		result++
	}
	return result
}

// conflicts 判断已占位弹幕是否会与当前弹幕重叠.
func (renderer *danmakuASSRenderer) conflicts(comment, occupied *danmakuASSComment) bool {
	if comment.position == assPositionTop || comment.position == assPositionBottom {
		return occupied.start+renderer.options.StillDuration > comment.start
	}
	threshold := comment.start - renderer.options.ScrollDuration
	if denominator := comment.width + danmakuASSStageWidth; denominator != 0 {
		threshold = comment.start - renderer.options.ScrollDuration*(1-danmakuASSStageWidth/denominator)
	}
	if occupied.start > threshold {
		return true
	}
	if denominator := occupied.width + danmakuASSStageWidth; denominator != 0 {
		return occupied.start+occupied.width*renderer.options.ScrollDuration/denominator > comment.start
	}
	return false
}

func (renderer *danmakuASSRenderer) markRow(comment *danmakuASSComment, row int) {
	lane := renderer.rows[comment.position]
	for index := row; index < row+int(math.Ceil(comment.height)); index++ {
		if index < 0 || index >= len(lane) {
			continue
		}
		lane[index] = comment
	}
}

func (renderer *danmakuASSRenderer) findAlternativeRow(comment *danmakuASSComment) int {
	result := 0
	resultStart := math.Inf(1)
	lane := renderer.rows[comment.position]
	limit := danmakuASSStageHeight - int(math.Ceil(comment.height))
	if limit > len(lane) {
		limit = len(lane)
	}
	for row := 0; row < limit; row++ {
		if lane[row] == nil {
			return row
		}
		if lane[row].start < resultStart {
			resultStart = lane[row].start
			result = row
		}
	}
	if result < 0 {
		result = 0
	}
	return result
}

func (renderer *danmakuASSRenderer) writeComment(builder *strings.Builder, comment *danmakuASSComment, row int) {
	styles := make([]string, 0, 3)
	duration := renderer.options.ScrollDuration
	switch comment.position {
	case assPositionTop:
		styles = append(styles, fmt.Sprintf(`\an8\pos(%d, %d)`, danmakuASSStageWidth/2, row))
		duration = renderer.options.StillDuration
	case assPositionBottom:
		styles = append(styles, fmt.Sprintf(`\an2\pos(%d, %d)`, danmakuASSStageWidth/2, danmakuASSStageHeight-row))
		duration = renderer.options.StillDuration
	case assPositionReverse:
		styles = append(styles, fmt.Sprintf(`\move(%d, %d, %d, %d)`, -int(math.Ceil(comment.width)), row, danmakuASSStageWidth, row))
	default:
		styles = append(styles, fmt.Sprintf(`\move(%d, %d, %d, %d)`, danmakuASSStageWidth, row, -int(math.Ceil(comment.width)), row))
	}
	if math.Abs(comment.size-renderer.options.FontSize) >= 1 {
		styles = append(styles, fmt.Sprintf(`\fs%.0f`, comment.size))
	}
	if comment.color != 0xffffff {
		styles = append(styles, fmt.Sprintf(`\c&H%s&`, danmakuASSColor(comment.color)))
		if comment.color == 0x000000 {
			styles = append(styles, `\3c&HFFFFFF&`)
		}
	}
	fmt.Fprintf(builder, "Dialogue: 2,%s,%s,%s,,0000,0000,0000,,{%s}%s\n",
		danmakuASSTimestamp(comment.start),
		danmakuASSTimestamp(comment.start+duration),
		renderer.styleID,
		strings.Join(styles, ""),
		danmakuASSEscape(comment.content))
}

// danmakuASSColor 把 0xRRGGBB 转成 ASS 使用的 BBGGRR 顺序.
func danmakuASSColor(color int) string {
	red := (color >> 16) & 0xff
	green := (color >> 8) & 0xff
	blue := color & 0xff
	return fmt.Sprintf("%02X%02X%02X", blue, green, red)
}

// danmakuASSTimestamp 输出 ASS 的 时:分:秒.厘秒 时间格式.
func danmakuASSTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(math.Round(seconds * 100))
	hour := total / 360000
	total -= hour * 360000
	minute := total / 6000
	total -= minute * 6000
	second := total / 100
	centisecond := total - second*100
	return fmt.Sprintf("%d:%02d:%02d.%02d", hour, minute, second, centisecond)
}

// danmakuASSEscape 移植 danmaku2ass 的文本转义, 保证 ASS 覆盖标签不会被弹幕内容破坏.
func danmakuASSEscape(content string) string {
	replaced := strings.NewReplacer(
		`\`, "\\\u200b",
		`{`, `\{`,
		`}`, `\}`,
	).Replace(content)
	lines := strings.Split(replaced, "\n")
	for index, line := range lines {
		if line == "" {
			lines[index] = " "
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			line = "\u200b" + line
		}
		if last := line[len(line)-1]; last == ' ' || last == '\t' {
			line += "\u200b"
		}
		lines[index] = line
	}
	return strings.Join(lines, `\N`)
}
