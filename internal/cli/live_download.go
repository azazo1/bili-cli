package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/media"
	"github.com/azazo1/bilibili-cli/internal/output"
)

func newLiveCommand(app *App) *cobra.Command {
	command := &cobra.Command{
		Use:   "live",
		Short: "查看和下载直播内容",
	}
	command.AddCommand(newLiveDownloadCommand(app))
	return command
}

func newLiveDownloadCommand(app *App) *cobra.Command {
	var outputDir, format string
	var quality int
	command := &cobra.Command{
		Use:   "download ROOM_OR_URL",
		Short: "下载直播流并持续录制直到停止",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := output.ModeRich
			roomID, err := api.ExtractLiveRoomID(args[0])
			if err != nil {
				return app.failUsageWithMode(cmd, err, "", mode)
			}
			format = strings.ToLower(strings.TrimSpace(format))
			if format != "flv" && format != "hls" {
				return app.invalidInput(cmd, "--format 仅支持 flv 或 hls", mode)
			}
			ctx := contextOrBackground(cmd.Context())
			stream, fetchErr := app.API.GetLiveStream(ctx, roomID, quality, format, app.OptionalCredential(ctx))
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取直播地址", mode)
			}
			recordingStarted := time.Now()
			outDir := "."
			if outputDir != "" {
				outDir = expandHome(outputDir)
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return app.Fail(err, "创建输出目录失败", mode)
			}
			title := sanitizeFileName(stream.Title)
			if title == "audio" && strings.TrimSpace(stream.Title) == "" {
				title = "live_" + roomID
			}
			title += "_" + recordingStarted.Format("20060102_150405")
			extension := ".flv"
			if format == "hls" {
				extension = ".ts"
			}
			outputPath := filepath.Join(outDir, title+extension)
			fmt.Fprintf(app.Out.Stdout, "直播间 %s: %s\n", roomID, stream.Title)
			fmt.Fprintf(app.Out.Stdout, "流格式: %s, 编码: %s\n", stream.Format, stream.Codec)
			fmt.Fprintf(app.Out.Stdout, "开始录制, 停止请按 Ctrl-C. 输出文件: %s\n", outputPath)
			progress := newDownloadProgressBar(app.Out.Stdout, "直播流")
			bytes, downloadErr := media.DownloadLiveStreamWithProgress(ctx, stream.URL, outputPath, app.Logger, progress.Update)
			progress.Finish()
			if downloadErr != nil {
				return app.Fail(downloadErr, "录制直播流", mode)
			}
			fmt.Fprintf(app.Out.Stdout, "直播录制完成: %s (%.1f MB)\n", outputPath, float64(bytes)/(1024*1024))
			return nil
		},
	}
	command.Flags().StringVarP(&outputDir, "output", "o", "", "输出目录, 默认当前文件夹")
	command.Flags().IntVarP(&quality, "quality", "q", 10000, "画质编号, 默认 10000 原画")
	command.Flags().StringVarP(&format, "format", "f", "flv", "流格式: flv 或 hls")
	return command
}
