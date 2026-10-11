package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

// danmakuExportFormats 是支持的导出格式.
var danmakuExportFormats = []string{"ass", "xml"}

// danmakuPreviewLimit 是 rich 模式下默认预览的弹幕条数.
const danmakuPreviewLimit = 20

type danmakuCommandOptions struct {
	outputDir   string
	formats     []string
	limit       int
	assFontSize float64
	assDuration time.Duration
	assFontFace string
	asJSON      bool
	asYAML      bool
}

// danmakuExportResult 记录一个导出文件.
type danmakuExportResult struct {
	Format string
	Path   string
	Bytes  int
}

func newDanmakuCommand(app *App) *cobra.Command {
	options := &danmakuCommandOptions{}
	command := &cobra.Command{
		Use:     "danmaku BV_OR_URL",
		Aliases: []string{"danmu"},
		Short:   "获取并导出视频弹幕",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDanmakuCommand(app, cmd, args[0], options)
		},
	}
	command.Flags().StringVarP(&options.outputDir, "output", "o", "", "弹幕导出目录, 留空则只输出到终端")
	command.Flags().StringSliceVar(&options.formats, "format", []string{"ass"}, "导出格式, 支持 ass 和 xml")
	command.Flags().IntVar(&options.limit, "limit", 0, "终端与结构化输出的弹幕条数上限, 0 表示全部")
	command.Flags().Float64Var(&options.assFontSize, "ass-fontsize", 56, "ASS 基准字号")
	command.Flags().DurationVar(&options.assDuration, "ass-duration", 8*time.Second, "ASS 滚动弹幕显示时长")
	command.Flags().StringVar(&options.assFontFace, "ass-font", "sans-serif", "ASS 字体名")
	addStructuredFlags(command, &options.asJSON, &options.asYAML)
	command.AddCommand(newDanmakuSendCommand(app))
	return command
}

func runDanmakuCommand(app *App, cmd *cobra.Command, input string, options *danmakuCommandOptions) error {
	mode, err := app.mode(cmd, options.asJSON, options.asYAML)
	if err != nil {
		return err
	}
	if options.limit < 0 {
		return app.invalidInput(cmd, "--limit 不能为负数", mode)
	}
	formats, err := normalizeDanmakuFormats(options.formats)
	if err != nil {
		return app.invalidInput(cmd, err.Error(), mode)
	}
	reference, err := app.extractVideoReference(cmd, input, mode)
	if err != nil {
		return err
	}
	ctx := contextOrBackground(cmd.Context())
	credential := app.OptionalCredential(ctx)
	pages, err := app.API.GetVideoPages(ctx, reference.BVID, credential)
	if err != nil {
		return app.apiFailure(err, "获取视频分P", mode)
	}
	if !reference.PageSpecified && len(pages) > 1 {
		return reportVideoPages(app, reference.BVID, pages, "bili video danmaku", mode)
	}
	if reference.Page > len(pages) {
		return app.Fail(api.NewError(api.CodeNotFound, "", "分P序号超出范围"), "获取弹幕", mode)
	}
	selectedPage := pages[reference.Page-1]
	durationMs := selectedPage.DurationSeconds * 1000
	result, err := app.API.GetVideoDanmaku(ctx, selectedPage.CID, durationMs, credential)
	if err != nil {
		return app.apiFailure(err, "获取弹幕", mode)
	}
	videoTitle := reference.BVID
	if info, infoErr := app.API.GetVideoInfo(ctx, reference.BVID, credential); infoErr == nil {
		if title := stringValue(info["title"]); title != "" {
			videoTitle = title
		}
	} else {
		app.Logger.Warn("获取视频标题失败, 使用 BV 号作为弹幕文件名", "bvid", reference.BVID, "error", infoErr)
	}
	fileTitle := videoDownloadFileTitle(videoTitle, reference.Page, len(pages), selectedPage.Title)
	warnings := make([]string, 0, 2)
	exports := make([]danmakuExportResult, 0, len(formats))
	if options.outputDir != "" && len(result.Items) > 0 {
		outputDir := expandHome(options.outputDir)
		exported, exportWarnings, exportErr := exportDanmakuFiles(outputDir, fileTitle, result.Items, result.CID, formats, options)
		if exportErr != nil {
			return app.Fail(exportErr, "导出弹幕文件失败", mode)
		}
		exports = exported
		warnings = append(warnings, exportWarnings...)
	}
	app.Logger.Info("导出弹幕",
		"bvid", reference.BVID, "page", reference.Page, "cid", selectedPage.CID,
		"count", len(result.Items), "segments", result.Segments, "formats", formats)

	payload := danmakuCommandPayload(reference.BVID, reference.Page, selectedPage, result, options.limit, exports, warnings)
	return app.CompleteTable(payload, mode, options.asJSON, options.asYAML, func(w io.Writer) {
		renderDanmakuResult(app, w, reference.BVID, reference.Page, result, options.limit, exports, warnings)
	})
}

func normalizeDanmakuFormats(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"ass"}, nil
	}
	seen := make(map[string]struct{}, len(values))
	formats := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.ToLower(strings.TrimSpace(part))
			if normalized == "" {
				continue
			}
			if !isDanmakuExportFormat(normalized) {
				return nil, fmt.Errorf("--format 仅支持 %s", strings.Join(danmakuExportFormats, ", "))
			}
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			formats = append(formats, normalized)
		}
	}
	if len(formats) == 0 {
		return []string{"ass"}, nil
	}
	return formats, nil
}

func isDanmakuExportFormat(value string) bool {
	for _, format := range danmakuExportFormats {
		if format == value {
			return true
		}
	}
	return false
}

// exportDanmakuFiles 按指定格式写出弹幕文件, 文件名与视频下载和字幕共享基名.
func exportDanmakuFiles(outputDir, fileTitle string, items []api.DanmakuItem, cid int64, formats []string, options *danmakuCommandOptions) ([]danmakuExportResult, []string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, nil, err
	}
	safeTitle := sanitizeFileName(fileTitle)
	exports := make([]danmakuExportResult, 0, len(formats))
	warnings := make([]string, 0)
	for _, format := range formats {
		var content string
		switch format {
		case "ass":
			assOptions := api.DefaultDanmakuASSOptions()
			assOptions.Title = safeTitle
			if options != nil {
				if options.assFontFace != "" {
					assOptions.FontFace = options.assFontFace
				}
				if options.assFontSize > 0 {
					assOptions.FontSize = options.assFontSize
				}
				if options.assDuration > 0 {
					assOptions.ScrollDuration = options.assDuration.Seconds()
				}
			}
			rendered, assWarnings := api.FormatDanmakuASS(items, assOptions)
			content = rendered
			warnings = append(warnings, assWarnings...)
		case "xml":
			content = api.FormatDanmakuXML(cid, items)
		}
		if content == "" {
			return nil, nil, fmt.Errorf("生成 %s 弹幕内容失败", format)
		}
		path := filepath.Join(outputDir, safeTitle+"."+format)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, nil, err
		}
		exports = append(exports, danmakuExportResult{Format: format, Path: path, Bytes: len(content)})
	}
	return exports, warnings, nil
}

func danmakuCommandPayload(bvid string, page int, selectedPage api.VideoPage, result api.DanmakuResult, limit int, exports []danmakuExportResult, warnings []string) map[string]any {
	items := result.Items
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	entries := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entries = append(entries, danmakuItemPayload(item))
	}
	outputs := make([]map[string]any, 0, len(exports))
	for _, item := range exports {
		outputs = append(outputs, map[string]any{
			"format": item.Format,
			"path":   item.Path,
			"bytes":  item.Bytes,
		})
	}
	modeCounts := danmakuModeCounts(result.Items)
	return map[string]any{
		"bvid":       bvid,
		"page":       page,
		"cid":        selectedPage.CID,
		"part_title": selectedPage.Title,
		"duration":   selectedPage.DurationSeconds,
		"segments":   result.Segments,
		"total":      len(result.Items),
		"mode_counts": map[string]any{
			"scroll": modeCounts[1],
			"top":    modeCounts[5],
			"bottom": modeCounts[4],
			"other":  len(result.Items) - modeCounts[1] - modeCounts[5] - modeCounts[4],
		},
		"items":    entries,
		"outputs":  outputs,
		"warnings": warnings,
	}
}

func danmakuItemPayload(item api.DanmakuItem) map[string]any {
	entry := map[string]any{
		"id":          item.IDString(),
		"progress_ms": item.Progress,
		"time":        danmakuTimeLabel(item.Progress),
		"mode":        item.Mode,
		"mode_name":   item.ModeName(),
		"font_size":   item.FontSize,
		"color":       item.Color,
		"color_hex":   fmt.Sprintf("#%06X", item.Color),
		"mid_hash":    item.MidHash,
		"content":     item.Content,
	}
	if item.ID != 0 {
		entry["id_number"] = item.ID
	}
	if item.CTime > 0 {
		entry["posted_at"] = model.TimestampISO(item.CTime)
	}
	if item.Pool != 0 {
		entry["pool"] = item.Pool
	}
	if item.Weight != 0 {
		entry["weight"] = item.Weight
	}
	if item.LikeCount != 0 {
		entry["like_count"] = item.LikeCount
	}
	if item.Colorful != 0 {
		entry["colorful"] = item.Colorful
	}
	if item.DmFrom != 0 {
		entry["dm_from"] = item.DmFrom
	}
	return entry
}

// danmakuModeCounts 统计各模式弹幕数量.
func danmakuModeCounts(items []api.DanmakuItem) map[int]int {
	counts := make(map[int]int, 8)
	for _, item := range items {
		counts[item.Mode]++
	}
	return counts
}

func danmakuTimeLabel(progressMs int) string {
	if progressMs < 0 {
		progressMs = 0
	}
	total := progressMs / 100
	hour := total / 3600
	total -= hour * 3600
	minute := total / 60
	second := total - minute*60
	tenth := (progressMs / 100) % 10
	if hour > 0 {
		return fmt.Sprintf("%d:%02d:%02d.%d", hour, minute, second, tenth)
	}
	return fmt.Sprintf("%02d:%02d.%d", minute, second, tenth)
}

func renderDanmakuResult(app *App, w io.Writer, bvid string, page int, result api.DanmakuResult, limit int, exports []danmakuExportResult, warnings []string) {
	if len(result.Items) == 0 {
		app.renderTable(w, "弹幕", []string{"字段", "内容"}, [][]string{
			{"BV号", bvid},
			{"分P", fmt.Sprintf("P%d", page)},
			{"CID", fmt.Sprintf("%d", result.CID)},
			{"弹幕数", "0"},
		})
		fmt.Fprintln(w, "该视频暂无弹幕")
		return
	}
	modeCounts := danmakuModeCounts(result.Items)
	rows := [][]string{
		{"BV号", bvid},
		{"分P", fmt.Sprintf("P%d", page)},
		{"CID", fmt.Sprintf("%d", result.CID)},
		{"弹幕数", fmt.Sprintf("%d", len(result.Items))},
		{"分段", fmt.Sprintf("%d", result.Segments)},
		{"滚动", fmt.Sprintf("%d", modeCounts[1])},
		{"顶部", fmt.Sprintf("%d", modeCounts[5])},
		{"底部", fmt.Sprintf("%d", modeCounts[4])},
	}
	if other := len(result.Items) - modeCounts[1] - modeCounts[5] - modeCounts[4]; other > 0 {
		rows = append(rows, []string{"其他", fmt.Sprintf("%d", other)})
	}
	for _, item := range exports {
		rows = append(rows, []string{"导出 " + strings.ToUpper(item.Format), item.Path})
	}
	app.renderTable(w, "弹幕", []string{"字段", "内容"}, rows)
	// --limit 大于 0 时预览同样使用该上限, 与结构化输出保持一致.
	previewLimit := danmakuPreviewLimit
	if limit > 0 {
		previewLimit = limit
	}
	preview := result.Items
	if len(preview) > previewLimit {
		preview = preview[:previewLimit]
	}
	previewRows := make([][]string, 0, len(preview))
	for _, item := range preview {
		previewRows = append(previewRows, []string{
			danmakuTimeLabel(item.Progress),
			item.ModeName(),
			truncate(item.Content, 60),
		})
	}
	app.renderTable(w, "\n弹幕预览", []string{"时间", "类型", "内容"}, previewRows)
	if len(result.Items) > len(preview) {
		fmt.Fprintf(w, "共 %d 条弹幕, 已预览 %d 条. 用 --limit 调整预览条数, 或用 --json 输出完整条目\n", len(result.Items), len(preview))
	}
	for _, warning := range warnings {
		fmt.Fprintf(w, "警告: %s\n", warning)
	}
}

// downloadDanmakuForVideo 为 video download 导出弹幕, 供 --with-danmaku 使用.
func (a *App) downloadDanmakuForVideo(ctx context.Context, bvid string, page int, pages []api.VideoPage, outputDir, fileTitle string, options *danmakuCommandOptions) ([]danmakuExportResult, error) {
	if page < 1 || page > len(pages) {
		return nil, api.NewError(api.CodeNotFound, "下载弹幕", "分P序号超出范围")
	}
	selectedPage := pages[page-1]
	credential := a.OptionalCredential(ctx)
	formats, err := normalizeDanmakuFormats([]string{"ass"})
	if err != nil {
		return nil, err
	}
	result, err := a.API.GetVideoDanmaku(ctx, selectedPage.CID, selectedPage.DurationSeconds*1000, credential)
	if err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return nil, api.NewError(api.CodeNotFound, "下载弹幕", "视频没有弹幕")
	}
	exports, _, exportErr := exportDanmakuFiles(outputDir, fileTitle, result.Items, selectedPage.CID, formats, options)
	if exportErr != nil {
		return nil, exportErr
	}
	return exports, nil
}
