package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

func checkUpDynamic(ctx context.Context, source Source, rule Rule, cred *api.Credential, now time.Time) (checkResult, error) {
	if rule.Target.UID <= 0 {
		return checkResult{}, api.NewError(api.CodeInvalidInput, "检查动态", "缺少 UID")
	}
	data, err := source.GetUserDynamics(ctx, rule.Target.UID, 0, cred)
	if err != nil {
		return checkResult{}, err
	}
	items := model.Maps(data["items"])
	ids := make([]string, 0, len(items))
	byID := make(map[string]map[string]any, len(items))
	for _, item := range items {
		if isPinnedDynamic(item) {
			continue
		}
		normalized := model.NormalizeDynamicItem(item)
		id := model.String(normalized["id"])
		if id == "" {
			continue
		}
		ids = append(ids, id)
		byID[id] = normalized
	}
	state := rule.State
	if !state.Initialized {
		state.SeenIDs = mergeSeen(nil, ids)
		if len(ids) > 0 {
			state.LastID = ids[0]
		}
		return checkResult{State: state}, nil
	}
	name := displayName(rule.Target)
	events := make([]Event, 0)
	for _, id := range newIDs(state.SeenIDs, ids) {
		item := byID[id]
		title := model.String(item["title"])
		text := model.String(item["text"])
		summaryText := title
		if summaryText == "" {
			summaryText = text
		}
		if summaryText == "" {
			summaryText = id
		}
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindUpDynamic,
			HappenedAt: now,
			Title:      summaryText,
			URL:        dynamicURL(id),
			Summary:    fmt.Sprintf("%s 发布了新动态: %s", name, summaryText),
			Target:     rule.Target,
			Payload:    map[string]any{"dynamic_id": id, "title": title, "text": text},
		})
	}
	state.SeenIDs = mergeSeen(state.SeenIDs, ids)
	if len(ids) > 0 {
		state.LastID = ids[0]
	}
	return checkResult{Events: events, State: state}, nil
}

func isPinnedDynamic(item map[string]any) bool {
	modules := model.Map(item["modules"])
	author := model.Map(modules["module_author"])
	switch value := author["is_top"].(type) {
	case bool:
		return value
	default:
		return model.ToInt(author["is_top"], 0) == 1
	}
}
