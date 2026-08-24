package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	var outputDir, format, maxDurationValue, maxSizeValue string
	var quality string
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
			qualityCode, qualityErr := parseLiveQuality(quality)
			if qualityErr != nil {
				return app.invalidInput(cmd, qualityErr.Error(), mode)
			}
			maxDuration, durationErr := parseLiveDuration(maxDurationValue)
			if durationErr != nil {
				return app.invalidInput(cmd, durationErr.Error(), mode)
			}
			maxSize, sizeErr := parseLiveSize(maxSizeValue)
			if sizeErr != nil {
				return app.invalidInput(cmd, sizeErr.Error(), mode)
			}
			ctx := contextOrBackground(cmd.Context())
			stream, fetchErr := app.API.GetLiveStream(ctx, roomID, qualityCode, format, app.OptionalCredential(ctx))
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取直播地址", mode)
			}
			recordingStarted := time.Now()
			recordingCtx, cancelRecording := context.WithCancel(ctx)
			defer cancelRecording()
			var stopTimer *time.Timer
			if maxDuration > 0 {
				stopTimer = time.AfterFunc(maxDuration, cancelRecording)
				defer stopTimer.Stop()
			}
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
			progress := newDownloadProgressBarWithLimits(app.Out.Stdout, "直播流", maxDuration, maxSize)
			updateProgress := func(current media.DownloadProgress) {
				progress.Update(current)
				if maxSize > 0 && current.Written >= maxSize {
					cancelRecording()
				}
			}
			bytes, downloadErr := media.DownloadLiveStreamWithProgress(recordingCtx, stream.URL, outputPath, app.Logger, updateProgress)
			progress.Finish()
			if downloadErr != nil {
				return app.Fail(downloadErr, "录制直播流", mode)
			}
			if finalizeErr := media.FinalizeLiveRecording(outputPath, app.Logger); finalizeErr != nil {
				fmt.Fprintf(app.Out.Stdout, "文件时长元数据修复失败, 已保留原始文件: %v\n", finalizeErr)
			}
			fmt.Fprintf(app.Out.Stdout, "直播录制完成: %s (%.1f MB)\n", outputPath, float64(bytes)/(1024*1024))
			return nil
		},
	}
	command.Flags().StringVarP(&outputDir, "output", "o", "", "输出目录, 默认当前文件夹")
	command.Flags().StringVarP(&quality, "quality", "q", "原画", "画质: 流畅, 高清, 蓝光, 原画, 4K, 杜比, 或画质编号")
	command.Flags().StringVarP(&format, "format", "f", "flv", "流格式: flv 或 hls")
	command.Flags().StringVar(&maxDurationValue, "max-duration", "", "最大录制时长, 例如 30m 或 2h")
	command.Flags().StringVar(&maxSizeValue, "max-size", "", "最大录制大小, 例如 512MB 或 2GB")
	return command
}

func parseLiveDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("--max-duration 必须是正时长, 例如 30m 或 2h")
	}
	return duration, nil
}

func parseLiveSize(value string) (int64, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return 0, nil
	}
	multiplier := float64(1)
	for _, item := range []struct {
		suffix string
		factor  float64
	}{
		{"TB", 1 << 40},
		{"T", 1 << 40},
		{"GB", 1 << 30},
		{"G", 1 << 30},
		{"MB", 1 << 20},
		{"M", 1 << 20},
		{"KB", 1 << 10},
		{"K", 1 << 10},
		{"B", 1},
	} {
		if strings.HasSuffix(value, item.suffix) {
			value = strings.TrimSpace(strings.TrimSuffix(value, item.suffix))
			multiplier = item.factor
			break
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number <= 0 || number*multiplier > float64(^uint64(0)>>1) {
		return 0, fmt.Errorf("--max-size 必须是正大小, 例如 512MB 或 2GB")
	}
	return int64(number * multiplier), nil
}

func parseLiveQuality(value string) (int, error) {
	value = strings.TrimSpace(value)
	aliases := map[string]int{
		"流畅": 80,
		"高清": 150,
		"蓝光": 400,
		"原画": 10000,
		"4k": 20000,
		"杜比": 30000,
	}
	if quality, ok := aliases[strings.ToLower(value)]; ok {
		return quality, nil
	}
	quality, err := strconv.Atoi(value)
	if err != nil || quality < 1 {
		return 0, fmt.Errorf("--quality 仅支持流畅, 高清, 蓝光, 原画, 4K, 杜比或正整数画质编号")
	}
	return quality, nil
}
