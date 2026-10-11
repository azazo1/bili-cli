package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

// danmakuModeAliases 把命令行上的模式名映射到服务端 mode 取值.
var danmakuModeAliases = map[string]int{
	"scroll": api.DanmakuModeScroll,
	"1":      api.DanmakuModeScroll,
	"top":    api.DanmakuModeTop,
	"5":      api.DanmakuModeTop,
	"bottom": api.DanmakuModeBottom,
	"4":      api.DanmakuModeBottom,
}

func newDanmakuSendCommand(app *App) *cobra.Command {
	var progress string
	var modeValue string
	var colorValue string
	var fontSize int
	var pool int
	var fromFile string
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "send BV_OR_URL [TEXT]",
		Short: "发送一条视频弹幕",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessWrite, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			reference, err := app.extractVideoReference(cmd, args[0], mode)
			if err != nil {
				return err
			}
			text := ""
			if len(args) > 1 {
				text = args[1]
			}
			if fromFile != "" {
				data, readErr := os.ReadFile(expandHome(fromFile))
				if readErr != nil {
					return app.Fail(readErr, "读取弹幕文本失败", mode)
				}
				text = string(data)
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return app.invalidInput(cmd, "请提供弹幕文本. 可用 TEXT 或 --from-file FILE", mode)
			}
			progressMs, err := parseDanmakuProgress(progress)
			if err != nil {
				return app.invalidInput(cmd, err.Error(), mode)
			}
			danmakuMode, err := parseDanmakuMode(modeValue)
			if err != nil {
				return app.invalidInput(cmd, err.Error(), mode)
			}
			color, err := parseDanmakuColor(colorValue)
			if err != nil {
				return app.invalidInput(cmd, err.Error(), mode)
			}
			if fontSize <= 0 || fontSize > 64 {
				return app.invalidInput(cmd, "--fontsize 必须在 1 到 64 之间", mode)
			}
			if pool < 0 || pool > 2 {
				return app.invalidInput(cmd, "--pool 必须在 0 到 2 之间", mode)
			}
			ctx := contextOrBackground(cmd.Context())
			pages, err := app.API.GetVideoPages(ctx, reference.BVID, credential)
			if err != nil {
				return app.apiFailure(err, "获取视频分P", mode)
			}
			if !reference.PageSpecified && len(pages) > 1 {
				return reportVideoPages(app, reference.BVID, pages, "bili video danmaku send", mode)
			}
			if reference.Page > len(pages) {
				return app.Fail(api.NewError(api.CodeNotFound, "", "分P序号超出范围"), "发送弹幕", mode)
			}
			selectedPage := pages[reference.Page-1]
			result, err := app.API.PostVideoDanmaku(ctx, api.DanmakuPostRequest{
				BVID:     reference.BVID,
				OID:      selectedPage.CID,
				Content:  text,
				Progress: progressMs,
				Mode:     danmakuMode,
				Color:    color,
				FontSize: fontSize,
				Pool:     pool,
			}, credential)
			if err != nil {
				return app.apiFailure(err, "发送弹幕失败", mode)
			}
			app.Logger.Info("发送弹幕",
				"bvid", reference.BVID, "page", reference.Page, "cid", selectedPage.CID,
				"mode", danmakuMode, "progress_ms", progressMs, "dmid", result.DMID)
			payload := model.ActionResult("danmaku_post", map[string]any{
				"bvid":        reference.BVID,
				"page":        reference.Page,
				"cid":         selectedPage.CID,
				"dmid":        result.DMID,
				"progress_ms": progressMs,
				"mode":        danmakuMode,
				"color":       color,
				"font_size":   fontSize,
				"text":        text,
			})
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "已发送弹幕 (P%d, %s, %s)\n",
					reference.Page, danmakuTimeLabel(progressMs), truncate(text, 60))
				if result.DMID != "" {
					fmt.Fprintf(w, "弹幕 ID: %s\n", result.DMID)
				}
			})
		},
	}
	command.Flags().StringVar(&progress, "progress", "0", "弹幕出现时间, 支持 12.5, 12.5s, 1m20s, 00:12.5, 1:20:00")
	command.Flags().StringVar(&modeValue, "mode", "scroll", "弹幕类型: scroll, top, bottom")
	command.Flags().StringVar(&colorValue, "color", "#FFFFFF", "弹幕颜色, 支持 #RRGGBB 或十进制")
	command.Flags().IntVar(&fontSize, "fontsize", 25, "弹幕字号")
	command.Flags().IntVar(&pool, "pool", 0, "弹幕池: 0 普通, 1 字幕, 2 特殊")
	command.Flags().StringVar(&fromFile, "from-file", "", "从文件读取弹幕文本")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

// parseDanmakuTime 解析一个时间值, 返回毫秒; 空字符串返回 -1 表示未设置该侧限制.
// flagName 只用于错误信息.
func parseDanmakuTime(value, flagName string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return -1, nil
	}
	if strings.Contains(trimmed, ":") {
		parsed, err := parseDanmakuClock(trimmed)
		if err != nil {
			return 0, fmt.Errorf("无法解析 --%s: %s", flagName, value)
		}
		return parsed, nil
	}
	if seconds, err := strconv.ParseFloat(strings.TrimSuffix(strings.ToLower(trimmed), "s"), 64); err == nil {
		if seconds < 0 {
			return 0, fmt.Errorf("--%s 不能为负数", flagName)
		}
		return int(seconds * 1000), nil
	}
	duration, err := parseDanmakuDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("无法解析 --%s: %s", flagName, value)
	}
	return duration, nil
}

// parseDanmakuRange 解析 --from 与 --to, 未设置的一侧返回 -1.
func parseDanmakuRange(from, to string) (int, int, error) {
	fromMs, err := parseDanmakuTime(from, "from")
	if err != nil {
		return 0, 0, err
	}
	toMs, err := parseDanmakuTime(to, "to")
	if err != nil {
		return 0, 0, err
	}
	if fromMs >= 0 && toMs >= 0 && fromMs > toMs {
		return 0, 0, fmt.Errorf("--from 不能晚于 --to")
	}
	return fromMs, toMs, nil
}

// parseDanmakuProgress 解析弹幕出现时间, 返回毫秒; 空值表示 0.
func parseDanmakuProgress(value string) (int, error) {
	parsed, err := parseDanmakuTime(value, "progress")
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, nil
	}
	return parsed, nil
}

// parseDanmakuClock 解析 mm:ss.d 或 hh:mm:ss 形式.
func parseDanmakuClock(value string) (int, error) {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("无法解析 --progress: %s", value)
	}
	total := 0.0
	for _, part := range parts {
		number, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || number < 0 {
			return 0, fmt.Errorf("无法解析 --progress: %s", value)
		}
		total = total*60 + number
	}
	return int(total * 1000), nil
}

// parseDanmakuDuration 解析 1m20s 这类时长写法, 返回毫秒.
func parseDanmakuDuration(value string) (int, error) {
	lowered := strings.ToLower(strings.TrimSpace(value))
	total := 0.0
	current := strings.Builder{}
	hasUnit := false
	for _, char := range lowered {
		switch {
		case char >= '0' && char <= '9' || char == '.':
			current.WriteRune(char)
		case char == 'h' || char == 'm' || char == 's':
			if current.Len() == 0 {
				return 0, fmt.Errorf("无法解析时长: %s", value)
			}
			number, err := strconv.ParseFloat(current.String(), 64)
			if err != nil {
				return 0, fmt.Errorf("无法解析时长: %s", value)
			}
			switch char {
			case 'h':
				total += number * 3600
			case 'm':
				total += number * 60
			case 's':
				total += number
			}
			current.Reset()
			hasUnit = true
		default:
			return 0, fmt.Errorf("无法解析时长: %s", value)
		}
	}
	if !hasUnit || current.Len() > 0 {
		return 0, fmt.Errorf("无法解析时长: %s", value)
	}
	if total < 0 {
		return 0, fmt.Errorf("时长不能为负数")
	}
	return int(total * 1000), nil
}

func parseDanmakuMode(value string) (int, error) {
	mode, ok := danmakuModeAliases[strings.ToLower(strings.TrimSpace(value))]
	if !ok {
		return 0, fmt.Errorf("--mode 仅支持 scroll, top 或 bottom")
	}
	return mode, nil
}

func parseDanmakuColor(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0xffffff, nil
	}
	if strings.HasPrefix(trimmed, "#") {
		trimmed = strings.TrimPrefix(trimmed, "#")
	}
	if len(trimmed) == 6 {
		parsed, err := strconv.ParseInt(trimmed, 16, 32)
		if err != nil {
			return 0, fmt.Errorf("无法解析 --color: %s", value)
		}
		return int(parsed) & 0xffffff, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 || parsed > 0xffffff {
		return 0, fmt.Errorf("无法解析 --color: %s", value)
	}
	return parsed, nil
}
