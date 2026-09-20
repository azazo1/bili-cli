package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/output"
	"github.com/azazo1/bilibili-cli/internal/watch"
)

func newWatchCommand(app *App) *cobra.Command {
	command := &cobra.Command{
		Use:   "watch",
		Short: "订阅 UP 主, 视频数据和搜索变化",
	}
	command.AddCommand(
		newWatchAddCommand(app),
		newWatchListCommand(app),
		newWatchRemoveCommand(app),
		newWatchEnableCommand(app, true),
		newWatchEnableCommand(app, false),
		newWatchCheckCommand(app),
		newWatchRunCommand(app),
	)
	return command
}

func (a *App) watchStore() *watch.Store {
	return watch.NewStore()
}

func (a *App) watchEngine() *watch.Engine {
	return &watch.Engine{
		Store:  a.watchStore(),
		Source: a.API,
		Logger: a.Logger,
		Gap:    time.Duration(a.Config.Watch.RequestGapMs) * time.Millisecond,
	}
}

func (a *App) watchNotifier() watch.Notifier {
	return watch.Notifier{
		Webhook: a.Config.Watch.Notify.Webhook,
		Exec:    a.Config.Watch.Notify.Exec,
		Logger:  a.Logger,
	}
}

func newWatchListCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "list",
		Short: "列出本地订阅规则",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			file, loadErr := app.watchStore().Load()
			if loadErr != nil {
				return app.Fail(loadErr, "读取订阅失败", mode)
			}
			payload := map[string]any{"rules": file.Rules, "count": len(file.Rules)}
			return app.CompleteTable(payload, mode, asJSON, asYAML, func(w io.Writer) {
				if len(file.Rules) == 0 {
					fmt.Fprintln(w, "暂无订阅规则")
					return
				}
				rows := make([][]string, 0, len(file.Rules))
				for _, rule := range file.Rules {
					rows = append(rows, []string{
						strconv.Itoa(rule.ID),
						rule.Kind.Label(),
						rule.Target.Display(),
						enabledLabel(rule.Enabled),
						rule.Label,
					})
				}
				app.renderTable(w, fmt.Sprintf("订阅规则 (%d)", len(file.Rules)), []string{"ID", "类型", "对象", "状态", "说明"}, rows)
			})
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWatchRemoveCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:     "rm ID",
		Aliases: []string{"remove", "delete"},
		Short:   "删除订阅规则",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			id, parseErr := parseWatchRuleID(args[0])
			if parseErr != nil {
				return app.invalidInput(cmd, parseErr.Error(), mode)
			}
			rule, removeErr := app.watchStore().Remove(id)
			if removeErr != nil {
				return app.Fail(removeErr, "删除订阅失败", mode)
			}
			app.Logger.Info("已删除订阅规则", "rule_id", rule.ID, "kind", string(rule.Kind))
			payload := map[string]any{"removed": rule}
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "已删除订阅 %d (%s)\n", rule.ID, rule.Kind.Label())
			})
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWatchEnableCommand(app *App, enabled bool) *cobra.Command {
	var asJSON, asYAML bool
	use := "enable ID"
	short := "启用订阅规则"
	if !enabled {
		use = "disable ID"
		short = "停用订阅规则"
	}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			id, parseErr := parseWatchRuleID(args[0])
			if parseErr != nil {
				return app.invalidInput(cmd, parseErr.Error(), mode)
			}
			rule, setErr := app.watchStore().SetEnabled(id, enabled)
			if setErr != nil {
				return app.Fail(setErr, "更新订阅失败", mode)
			}
			payload := map[string]any{"rule": rule}
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "已%s订阅 %d (%s)\n", enabledLabel(enabled), rule.ID, rule.Kind.Label())
			})
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWatchCheckCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	var ruleID int
	command := &cobra.Command{
		Use:   "check [ID]",
		Short: "立即检查订阅并输出新事件",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			ids, parseErr := watchIDsFromInput(cmd, args, ruleID)
			if parseErr != nil {
				return app.invalidInput(cmd, parseErr.Error(), mode)
			}
			return app.runWatchCheck(cmd, mode, asJSON, asYAML, ids, true)
		},
	}
	command.Flags().IntVar(&ruleID, "id", 0, "只检查指定规则")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWatchRunCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	var ruleID int
	var intervalValue string
	command := &cobra.Command{
		Use:   "run",
		Short: "按间隔持续检查订阅",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			ids, parseErr := watchIDsFromInput(cmd, nil, ruleID)
			if parseErr != nil {
				return app.invalidInput(cmd, parseErr.Error(), mode)
			}
			interval := time.Duration(app.Config.Watch.IntervalSeconds) * time.Second
			if strings.TrimSpace(intervalValue) != "" {
				parsed, durationErr := time.ParseDuration(intervalValue)
				if durationErr != nil || parsed < 10*time.Second {
					return app.invalidInput(cmd, "--interval 必须是至少 10s 的时长, 例如 5m 或 300s", mode)
				}
				interval = parsed
			}
			if interval < 10*time.Second {
				return app.invalidInput(cmd, "watch.interval_seconds 必须至少 10 秒", mode)
			}
			ctx := contextOrBackground(cmd.Context())
			round := 0
			app.Logger.Info("开始持续监视", "interval", interval.String())
			for {
				round++
				app.Logger.Info("开始一轮订阅检查", "round", round)
				if runErr := app.runWatchCheck(cmd, mode, asJSON, asYAML, ids, false); runErr != nil {
					if ctx.Err() != nil {
						app.Logger.Info("订阅监视已停止")
						return nil
					}
					app.Logger.Error("订阅检查失败", "round", round, "error", runErr)
				}
				timer := time.NewTimer(interval)
				select {
				case <-ctx.Done():
					timer.Stop()
					app.Logger.Info("订阅监视已停止")
					return nil
				case <-timer.C:
				}
			}
		},
	}
	command.Flags().IntVar(&ruleID, "id", 0, "只检查指定规则")
	command.Flags().StringVar(&intervalValue, "interval", "", "检查间隔, 例如 5m 或 300s")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func (a *App) runWatchCheck(cmd *cobra.Command, mode output.Mode, asJSON, asYAML bool, ids []int, showEmpty bool) error {
	ctx := contextOrBackground(cmd.Context())
	credential := a.OptionalCredential(ctx)
	report, err := a.watchEngine().Check(ctx, credential, ids)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return a.Fail(err, "检查订阅失败", mode)
	}
	if notifyErr := a.watchNotifier().Send(ctx, report.Events); notifyErr != nil {
		a.Logger.Warn("订阅通知失败", "error", notifyErr)
		report.Warnings = append(report.Warnings, watch.Warning{Message: "通知失败: " + notifyErr.Error()})
	}
	payload := map[string]any{
		"checked_at": report.CheckedAt,
		"rules":      report.Rules,
		"events":     report.Events,
		"warnings":   report.Warnings,
	}
	return a.CompleteTable(payload, mode, asJSON, asYAML, func(w io.Writer) {
		if len(report.Events) == 0 && showEmpty {
			fmt.Fprintln(w, "无新事件")
		}
		if len(report.Events) > 0 {
			rows := make([][]string, 0, len(report.Events))
			for _, event := range report.Events {
				rows = append(rows, []string{
					strconv.Itoa(event.RuleID),
					event.Kind.Label(),
					event.Summary,
					event.URL,
				})
			}
			a.renderTable(w, fmt.Sprintf("新事件 (%d)", len(report.Events)), []string{"规则", "类型", "摘要", "链接"}, rows)
		}
		for _, warning := range report.Warnings {
			if warning.RuleID > 0 {
				fmt.Fprintf(w, "警告: 规则 %d %s\n", warning.RuleID, warning.Message)
				continue
			}
			fmt.Fprintf(w, "警告: %s\n", warning.Message)
		}
	})
}

func parseWatchRuleID(value string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || id < 1 {
		return 0, fmt.Errorf("规则 ID 必须是正整数")
	}
	return id, nil
}

func watchIDsFromInput(cmd *cobra.Command, args []string, flagID int) ([]int, error) {
	if len(args) == 1 {
		id, err := parseWatchRuleID(args[0])
		if err != nil {
			return nil, err
		}
		return []int{id}, nil
	}
	if flagID != 0 {
		if flagID < 1 {
			return nil, fmt.Errorf("规则 ID 必须是正整数")
		}
		return []int{flagID}, nil
	}
	return nil, nil
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "启用"
	}
	return "停用"
}
