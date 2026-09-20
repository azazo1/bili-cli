package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

func checkKeyword(ctx context.Context, source Source, rule Rule, cred *api.Credential, now time.Time) (checkResult, error) {
	_ = cred
	query := rule.Target.Query
	if query == "" {
		return checkResult{}, api.NewError(api.CodeInvalidInput, "检查搜索", "缺少关键词")
	}
	results, err := source.Search(ctx, query, api.SearchOptions{Type: api.SearchTypeVideo, Order: api.SearchOrderLatest, Page: 1})
	if err != nil {
		return checkResult{}, err
	}
	ids := make([]string, 0, len(results))
	byID := make(map[string]map[string]any, len(results))
	for _, item := range results {
		normalized := model.NormalizeSearchVideo(item)
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
	events := make([]Event, 0)
	for _, id := range newIDs(state.SeenIDs, ids) {
		item := byID[id]
		title := model.String(item["title"])
		if !titleMatches(title, rule.Options.TitleContains) {
			continue
		}
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindSearchKeyword,
			HappenedAt: now,
			Title:      title,
			URL:        videoURL(id),
			Summary:    fmt.Sprintf("搜索 %s 出现新视频: %s", query, title),
			Target:     rule.Target,
			Payload:    map[string]any{"bvid": id, "title": title, "query": query},
		})
	}
	state.SeenIDs = mergeSeen(state.SeenIDs, ids)
	if len(ids) > 0 {
		state.LastID = ids[0]
	}
	return checkResult{Events: events, State: state}, nil
}
