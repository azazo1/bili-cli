package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

func checkListUpdate(ctx context.Context, source Source, rule Rule, cred *api.Credential, now time.Time) (checkResult, error) {
	if rule.Target.UID <= 0 || rule.Target.ListID <= 0 {
		return checkResult{}, api.NewError(api.CodeInvalidInput, "检查合集", "缺少 UID 或列表 ID")
	}
	reference := api.UserListReference{
		OwnerID:  rule.Target.UID,
		ListID:   rule.Target.ListID,
		KindHint: api.UserListKind(rule.Target.ListKind),
	}
	list, err := source.GetUserList(ctx, reference, 1, cred)
	if err != nil {
		return checkResult{}, err
	}
	ids := make([]string, 0, len(list.Archives))
	byID := make(map[string]map[string]any, len(list.Archives))
	for _, archive := range list.Archives {
		normalized := model.NormalizeVideoSummary(archive)
		id := model.String(normalized["bvid"])
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
	listName := rule.Target.ListName
	if listName == "" {
		listName = list.Metadata.Title
	}
	if listName == "" {
		listName = fmt.Sprintf("列表 %d", rule.Target.ListID)
	}
	events := make([]Event, 0)
	for _, id := range newIDs(state.SeenIDs, ids) {
		item := byID[id]
		title := model.String(item["title"])
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindListUpdate,
			HappenedAt: now,
			Title:      title,
			URL:        videoURL(id),
			Summary:    fmt.Sprintf("合集 %s 更新: %s", listName, title),
			Target:     rule.Target,
			Payload:    map[string]any{"bvid": id, "title": title, "list_id": rule.Target.ListID},
		})
	}
	state.SeenIDs = mergeSeen(state.SeenIDs, ids)
	if len(ids) > 0 {
		state.LastID = ids[0]
	}
	return checkResult{Events: events, State: state}, nil
}
