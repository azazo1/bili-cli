package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/media"
	"github.com/azazo1/bilibili-cli/internal/output"
)

type videoDownloadItem struct {
	label string
	stream string
	url   string
	path  string
}

func newVideoDownloadCommand(app *App) *cobra.Command {
	var outputDir string
	var audioOnly, videoOnly, noMerge, withSRT, withDanmaku bool
	command := &cobra.Command{
		Use:   "download BV_OR_URL",
		Short: "下载视频音频和视频",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := output.ModeRich
			if audioOnly && videoOnly {
				return app.invalidInput(cmd, "--audio-only 和 --video-only 不能同时使用", mode)
			}
			reference, err := app.extractVideoReference(cmd, args[0], mode)
			if err != nil {
				return err
			}
			ctx := contextOrBackground(cmd.Context())
			credential := app.OptionalCredential(ctx)
			if !reference.PageSpecified {
				pages, pagesErr := app.API.GetVideoPages(ctx, reference.BVID, credential)
				if pagesErr != nil {
					return app.apiFailure(pagesErr, "获取视频分P", mode)
				}
				if len(pages) > 1 {
					return reportVideoPages(app, reference.BVID, pages, "bili video download", mode)
				}
			}
			info, fetchErr := app.API.GetVideoInfo(ctx, reference.BVID, credential)
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取视频信息", mode)
			}
			title := stringValue(info["title"])
			if title == "" {
				title = reference.BVID
			}
			fmt.Fprintf(app.Out.Stdout, "%s (%s)\n", title, formatDuration(info["duration"]))
			fmt.Fprintln(app.Out.Stdout, "获取下载地址...")
			urls, fetchErr := app.API.GetVideoDownloadURLsForPage(ctx, reference.BVID, reference.Page, credential)
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取下载地址", mode)
			}
			if strings.TrimSpace(urls.PartTitle) != "" {
				fmt.Fprintf(app.Out.Stdout, "分P P%d: %s\n", urls.Page, urls.PartTitle)
			}
			outDir := "."
			if outputDir != "" {
				outDir = expandHome(outputDir)
			}
			fileTitle := videoDownloadFileTitle(title, urls.Page, urls.PageCount, urls.PartTitle)
			items, planErr := makeVideoDownloadItems(urls, fileTitle, outDir, audioOnly, videoOnly)
			if planErr != nil {
				return app.Fail(planErr, "准备下载", mode)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return app.Fail(err, "创建输出目录失败", mode)
			}
			fmt.Fprintf(app.Out.Stdout, "输出目录: %s\n", outDir)
			downloadThreads := app.Config.Download.Threads
			if downloadThreads < 1 {
				downloadThreads = media.DefaultDownloadThreads
			}
			for _, item := range items {
				fmt.Fprintf(app.Out.Stdout, "下载%s中...\n", item.label)
				progress := newDownloadProgressBar(app.Out.Stdout, item.label)
				bytes, downloadErr := media.DownloadFileWithProgressAndThreads(ctx, item.url, item.path, app.Logger, progress.Update, downloadThreads)
				progress.Finish()
				if downloadErr != nil {
					return app.Fail(downloadErr, "下载"+item.label, mode)
				}
				fmt.Fprintf(app.Out.Stdout, "%s已保存: %s (%.1f MB)\n", item.label, item.path, float64(bytes)/(1024*1024))
			}
			if !noMerge {
				audioPath, videoPath := separateStreamPaths(items)
				if audioPath != "" && videoPath != "" {
					mergedPath := filepath.Join(outDir, sanitizeFileName(fileTitle)+".mp4")
					fmt.Fprintln(app.Out.Stdout, "合并音视频中...")
					if mergeErr := media.MergeStreams(ctx, audioPath, videoPath, mergedPath, app.Logger); mergeErr != nil {
						app.Logger.Warn("合并音视频失败, 保留原始流", "error", mergeErr)
						fmt.Fprintf(app.Out.Stdout, "合并失败, 已保留原始文件: %s, %s\n", audioPath, videoPath)
					} else {
						fmt.Fprintf(app.Out.Stdout, "合并完成: %s\n", mergedPath)
					}
				}
			}
			if withSRT {
				fmt.Fprintln(app.Out.Stdout, "尝试下载匹配字幕...")
				subtitlePath, subtitleErr := app.downloadPreferredSubtitle(ctx, reference.BVID, reference.Page, outDir, fileTitle, credential)
				if subtitleErr != nil {
					app.Logger.Warn("自动下载字幕失败, 继续视频下载", "bvid", reference.BVID, "page", reference.Page, "error", subtitleErr)
					fmt.Fprintf(app.Out.Stdout, "字幕下载失败, 继续完成视频下载: %v\n", subtitleErr)
				} else {
					fmt.Fprintf(app.Out.Stdout, "字幕已保存: %s\n", subtitlePath)
				}
			}
			if withDanmaku {
				fmt.Fprintln(app.Out.Stdout, "尝试下载弹幕...")
				exports, danmakuErr := app.downloadDanmakuForVideo(ctx, reference.BVID, reference.Page, urls.Pages, outDir, fileTitle, nil)
				if danmakuErr != nil {
					app.Logger.Warn("自动下载弹幕失败, 继续视频下载", "bvid", reference.BVID, "page", reference.Page, "error", danmakuErr)
					fmt.Fprintf(app.Out.Stdout, "弹幕下载失败, 继续完成视频下载: %v\n", danmakuErr)
				} else {
					for _, item := range exports {
						fmt.Fprintf(app.Out.Stdout, "弹幕已保存 (%s): %s\n", strings.ToUpper(item.Format), item.Path)
					}
				}
			}
			return nil
		},
	}
	command.Flags().StringVarP(&outputDir, "output", "o", "", "输出目录, 默认当前文件夹")
	command.Flags().BoolVar(&audioOnly, "audio-only", false, "仅下载音频")
	command.Flags().BoolVar(&videoOnly, "video-only", false, "仅下载视频")
	command.Flags().BoolVar(&noMerge, "no-merge", false, "不尝试合并音频和视频")
	command.Flags().BoolVar(&withSRT, "with-srt", false, "自动下载一条匹配字幕")
	command.Flags().BoolVar(&withDanmaku, "with-danmaku", false, "同时导出 ASS 弹幕")
	return command
}

func makeVideoDownloadItems(urls api.VideoDownloadURLs, title, outputDir string, audioOnly, videoOnly bool) ([]videoDownloadItem, error) {
	if audioOnly && videoOnly {
		return nil, api.NewError(api.CodeInvalidInput, "", "--audio-only 和 --video-only 不能同时使用")
	}
	safeTitle := sanitizeFileName(title)
	audioPath := filepath.Join(outputDir, safeTitle+"_audio.m4a")
	videoPath := filepath.Join(outputDir, safeTitle+"_video.m4a")
	combinedPath := filepath.Join(outputDir, safeTitle+".mp4")
	if audioOnly {
		if urls.AudioURL == "" {
			return nil, api.NewError(api.CodeNotFound, "", "无法获取音频流")
		}
		return []videoDownloadItem{{label: "音频", stream: "audio", url: urls.AudioURL, path: audioPath}}, nil
	}
	if videoOnly {
		videoURL := urls.VideoURL
		if videoURL == "" {
			videoURL = urls.CombinedURL
		}
		if videoURL == "" {
			return nil, api.NewError(api.CodeNotFound, "", "无法获取视频流")
		}
		path := videoPath
		stream := "video"
		if urls.VideoURL == "" {
			path = combinedPath
			stream = "combined"
		}
		return []videoDownloadItem{{label: "视频", stream: stream, url: videoURL, path: path}}, nil
	}
	if urls.AudioURL != "" && urls.VideoURL != "" {
		return []videoDownloadItem{
			{label: "音频", stream: "audio", url: urls.AudioURL, path: audioPath},
			{label: "视频", stream: "video", url: urls.VideoURL, path: videoPath},
		}, nil
	}
	if urls.CombinedURL != "" {
		return []videoDownloadItem{{label: "视频", stream: "combined", url: urls.CombinedURL, path: combinedPath}}, nil
	}
	if urls.AudioURL != "" {
		return []videoDownloadItem{{label: "音频", stream: "audio", url: urls.AudioURL, path: audioPath}}, nil
	}
	if urls.VideoURL != "" {
		return []videoDownloadItem{{label: "视频", stream: "video", url: urls.VideoURL, path: videoPath}}, nil
	}
	return nil, api.NewError(api.CodeNotFound, "", "无法获取音视频流")
}

func separateStreamPaths(items []videoDownloadItem) (audioPath, videoPath string) {
	for _, item := range items {
		switch item.stream {
		case "audio":
			audioPath = item.path
		case "video":
			videoPath = item.path
		}
	}
	return audioPath, videoPath
}

func videoDownloadFileTitle(title string, page, pageCount int, partTitle string) string {
	baseTitle := sanitizeFileName(title)
	parts := []string{}
	if pageCount > 1 && page > 0 {
		parts = append(parts, fmt.Sprintf("P%02d", page))
	}
	if strings.TrimSpace(partTitle) != "" && strings.TrimSpace(partTitle) != strings.TrimSpace(title) {
		parts = append(parts, sanitizeFileName(partTitle))
	}
	if len(parts) == 0 {
		return baseTitle
	}
	suffix := "_" + strings.Join(parts, "_")
	if len([]rune(suffix)) >= 120 {
		return truncate(strings.TrimPrefix(suffix, "_"), 120)
	}
	return truncate(baseTitle, 120-len([]rune(suffix))) + suffix
}

var unsafeFileName = regexp.MustCompile(`[<>:"/\\|?*]`)

func sanitizeFileName(title string) string {
	result := unsafeFileName.ReplaceAllString(title, "_")
	result = strings.Trim(result, ". ")
	runes := []rune(result)
	if len(runes) > 120 {
		result = string(runes[:120])
	}
	if result == "" {
		return "audio"
	}
	return result
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
