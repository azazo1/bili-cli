package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
	"github.com/azazo1/bilibili-cli/internal/output"
)

func newVideoCommand(app *App) *cobra.Command {
	var comments, showAI, related bool
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "video BV_OR_URL",
		Short: "查看视频详情",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			bvid, err := app.extractBVID(cmd, args[0], mode)
			if err != nil {
				return err
			}
			ctx := contextOrBackground(cmd.Context())
			credential := app.OptionalCredential(ctx)
			info, err := app.API.GetVideoInfo(ctx, bvid, credential)
			if err != nil {
				return app.apiFailure(err, "获取视频信息失败", mode)
			}
			pages, pagesErr := app.API.GetVideoPages(ctx, bvid, credential)
			aiSummary := ""
			commentItems := []map[string]any{}
			relatedItems := []map[string]any{}
			warnings := []map[string]string{}
			if pagesErr != nil {
				warnings = append(warnings, map[string]string{"code": "pages_unavailable", "message": "获取视频分P信息失败"})
				app.Logger.Warn("获取视频分P信息失败", "error", pagesErr)
			}
			if showAI {
				result, fetchErr := app.API.GetVideoAIConclusion(ctx, bvid, credential)
				if fetchErr != nil {
					warnings = append(warnings, map[string]string{"code": "ai_summary_unavailable", "message": "获取 AI 总结失败"})
					app.Logger.Warn("获取 AI 总结失败", "error", fetchErr)
				} else {
					aiSummary = stringValue(mapValue(result["model_result"])["summary"])
				}
			}
			if comments {
				result, fetchErr := app.API.GetVideoComments(ctx, bvid, 1, credential)
				if fetchErr != nil {
					warnings = append(warnings, map[string]string{"code": "comments_unavailable", "message": "获取评论失败"})
					app.Logger.Warn("获取评论失败", "error", fetchErr)
				} else {
					commentItems = firstMapList(result["replies"])
				}
			}
			if related {
				result, fetchErr := app.API.GetRelatedVideos(ctx, bvid, credential)
				if fetchErr != nil {
					warnings = append(warnings, map[string]string{"code": "related_unavailable", "message": "获取相关推荐失败"})
					app.Logger.Warn("获取相关推荐失败", "error", fetchErr)
				} else {
					relatedItems = result
				}
			}
			payload := model.NormalizeVideoCommandPayload(info, aiSummary, commentItems, relatedItems, warnings)
			payload["pages"] = videoPagePayload(pages)
			return app.CompleteTable(payload, mode, asJSON, asYAML, func(w io.Writer) {
				renderVideo(app, w, info, bvid)
				renderVideoPages(app, w, pages)
				if showAI {
					fmt.Fprintln(w, "\nAI 总结:")
					if aiSummary == "" {
						fmt.Fprintln(w, "该视频暂无 AI 总结")
					} else {
						fmt.Fprintln(w, aiSummary)
					}
				}
				if comments {
					fmt.Fprintln(w, "\n热门评论:")
					for _, item := range commentItems[:min(len(commentItems), 10)] {
						member := mapValue(item["member"])
						content := mapValue(item["content"])
						fmt.Fprintf(w, "%s (赞 %s)\n%s\n", stringValue(member["uname"]), formatCount(item["like"]), truncate(stringValue(content["message"]), 120))
					}
				}
				normalizedRelated := firstMapList(payload["related"])
				if related && len(normalizedRelated) > 0 {
					visible := normalizedRelated[:min(len(normalizedRelated), 10)]
					includePublishedAt := hasPublishedAt(visible)
					rows := make([][]string, 0, len(visible))
					for index, item := range visible {
						owner := mapValue(item["owner"])
						stats := mapValue(item["stats"])
						row := []string{fmt.Sprintf("%d", index+1), stringValue(item["bvid"]), stringValue(item["title"]), stringValue(owner["name"])}
						if includePublishedAt {
							row = append(row, publishedTime(item))
						}
						rows = append(rows, append(row, formatCount(stats["view"])))
					}
					headers := []string{"#", "BV号", "标题", "UP主"}
					if includePublishedAt {
						headers = append(headers, "发布时间")
					}
					headers = append(headers, "播放")
					app.renderTable(w, "\n相关推荐", headers, rows)
				}
			})
		},
	}
	command.Flags().BoolVarP(&comments, "comments", "c", false, "显示评论")
	command.Flags().BoolVar(&showAI, "ai", false, "显示 AI 总结")
	command.Flags().BoolVarP(&related, "related", "r", false, "显示相关推荐")
	addStructuredFlags(command, &asJSON, &asYAML)
	command.AddCommand(
		newDanmakuCommand(app),
		newSubtitleCommand(app),
		newVideoDownloadCommand(app),
		newCoinCommand(app),
		newHotCommand(app),
		newLikeCommand(app),
		newRankCommand(app),
		newTripleCommand(app),
		newWatchLaterCommand(app),
	)
	return command
}

func renderVideo(app *App, w io.Writer, info map[string]any, bvid string) {
	owner := mapValue(info["owner"])
	stat := mapValue(info["stat"])
	rows := [][]string{
		{"BV号", bvid},
		{"标题", stringValue(info["title"])},
		{"UP主", fmt.Sprintf("%s (UID: %s)", stringValue(owner["name"]), stringValue(owner["mid"]))},
		{"时长", formatDuration(info["duration"])},
		{"播放", formatCount(stat["view"])},
		{"弹幕", formatCount(stat["danmaku"])},
		{"点赞", formatCount(stat["like"])},
		{"投币", formatCount(stat["coin"])},
		{"收藏", formatCount(stat["favorite"])},
		{"分享", formatCount(stat["share"])},
		{"链接", "https://www.bilibili.com/video/" + bvid},
	}
	if description := stringValue(info["desc"]); description != "" {
		rows = append(rows, []string{"简介", description})
	}
	app.renderTable(w, "视频详情", []string{"字段", "内容"}, rows)
}

func renderVideoPages(app *App, w io.Writer, pages []api.VideoPage) {
	if len(pages) <= 1 {
		return
	}
	rows := make([][]string, 0, len(pages))
	for _, page := range pages {
		title := strings.TrimSpace(page.Title)
		if title == "" {
			title = "未命名"
		}
		rows = append(rows, []string{fmt.Sprintf("P%d", page.Page), title})
	}
	app.renderTable(w, "\n分P", []string{"分P", "标题"}, rows)
}

func videoPagePayload(pages []api.VideoPage) []map[string]any {
	items := make([]map[string]any, 0, len(pages))
	for _, page := range pages {
		items = append(items, map[string]any{
			"page":  page.Page,
			"cid":   page.CID,
			"title": page.Title,
		})
	}
	return items
}

func reportVideoPages(app *App, bvid string, pages []api.VideoPage, exampleCommand string, mode output.Mode) error {
	err := api.NewError(api.CodeInvalidInput, "", "多分P视频必须通过 URL 的 p 参数指定分P")
	if mode != output.ModeRich {
		return app.fail(err, "选择分P", mode, map[string]any{
			"bvid":    bvid,
			"pages":   videoPagePayload(pages),
			"example": fmt.Sprintf("%s %s?p=2", exampleCommand, bvid),
		})
	}
	fmt.Fprintln(app.Out.Stdout, "该视频包含多个分P, 请先选择要下载的分P:")
	for _, page := range pages {
		title := strings.TrimSpace(page.Title)
		if title == "" {
			title = "未命名"
		}
		fmt.Fprintf(app.Out.Stdout, "  P%d: %s\n", page.Page, title)
	}
	fmt.Fprintf(app.Out.Stdout, "示例: %s %s?p=2\n", exampleCommand, bvid)
	return app.Fail(err, "选择分P", mode)
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
