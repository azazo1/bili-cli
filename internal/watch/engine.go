package watch

import (
	"context"
	"log/slog"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
)

type Engine struct {
	Store  *Store
	Source Source
	Logger *slog.Logger
	Now    func() time.Time
	Gap    time.Duration
}

func (e *Engine) Check(ctx context.Context, cred *api.Credential, ids []int) (Report, error) {
	started := e.now()
	file, err := e.Store.Load()
	if err != nil {
		return Report{}, err
	}
	selected := selectRules(file.Rules, ids)
	report := Report{CheckedAt: started, Events: []Event{}, Warnings: []Warning{}}
	e.logger().Info("开始检查订阅", "rules", len(selected))
	for index, rule := range selected {
		if index > 0 && e.Gap > 0 {
			timer := time.NewTimer(e.Gap)
			select {
			case <-ctx.Done():
				timer.Stop()
				return report, ctx.Err()
			case <-timer.C:
			}
		}
		result, checkErr := e.checkRule(ctx, rule, cred)
		state := rule.State
		if checkErr == nil {
			state = result.State
			state.Initialized = true
			state.LastCheckedAt = e.now()
			report.Events = append(report.Events, result.Events...)
			e.logger().Debug("已检查订阅规则", "rule_id", rule.ID, "kind", string(rule.Kind), "events", len(result.Events))
		} else {
			e.logger().Warn("订阅规则检查失败", "rule_id", rule.ID, "kind", string(rule.Kind), "error", checkErr)
			report.Warnings = append(report.Warnings, Warning{RuleID: rule.ID, Kind: rule.Kind, Message: checkErr.Error()})
			if api.CodeOf(checkErr) == api.CodeRateLimited {
				report.Warnings = append(report.Warnings, Warning{Message: "请求被限流, 本轮剩余规则已跳过"})
				if saveErr := e.Store.UpdateState(rule.ID, state); saveErr != nil {
					return report, saveErr
				}
				break
			}
		}
		if saveErr := e.Store.UpdateState(rule.ID, state); saveErr != nil {
			return report, saveErr
		}
		report.Rules++
	}
	e.logger().Info("订阅检查完成", "rules", report.Rules, "events", len(report.Events), "warnings", len(report.Warnings), "duration", e.now().Sub(started))
	return report, nil
}

func (e *Engine) checkRule(ctx context.Context, rule Rule, cred *api.Credential) (checkResult, error) {
	switch rule.Kind {
	case KindUpVideo:
		return checkUpVideo(ctx, e.Source, rule, cred, e.now())
	case KindUpLive:
		return checkUpLive(ctx, e.Source, rule, cred, e.now())
	case KindVideoStat:
		return checkVideoStat(ctx, e.Source, rule, cred, e.now())
	case KindUpDynamic:
		return checkUpDynamic(ctx, e.Source, rule, cred, e.now())
	case KindSearchKeyword:
		return checkKeyword(ctx, e.Source, rule, cred, e.now())
	case KindListUpdate:
		return checkListUpdate(ctx, e.Source, rule, cred, e.now())
	default:
		return checkResult{}, api.NewError(api.CodeInvalidInput, "检查订阅", "不支持的订阅类型: "+string(rule.Kind))
	}
}

func selectRules(rules []Rule, ids []int) []Rule {
	if len(ids) == 0 {
		selected := make([]Rule, 0, len(rules))
		for _, rule := range rules {
			if rule.Enabled {
				selected = append(selected, rule)
			}
		}
		return selected
	}
	want := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	selected := make([]Rule, 0, len(ids))
	for _, rule := range rules {
		if _, ok := want[rule.ID]; ok {
			selected = append(selected, rule)
		}
	}
	return selected
}

func (e *Engine) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) logger() *slog.Logger {
	if e != nil && e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}
