package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

func checkUpVideo(ctx context.Context, source Source, rule Rule, cred *api.Credential, now time.Time) (checkResult, error) {
	if rule.Target.UID <= 0 {
		return checkResult{}, api.NewError(api.CodeInvalidInput, "检查新视频", "缺少 UID")
	}
	videos, err := source.GetUserVideos(ctx, rule.Target.UID, 20, cred)
	if err != nil {
		return checkResult{}, err
	}
	ids := make([]string, 0, len(videos))
	byID := make(map[string]map[string]any, len(videos))
	for _, video := range videos {
		normalized := model.NormalizeVideoSummary(video)
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
		name := displayName(rule.Target)
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindUpVideo,
			HappenedAt: now,
			Title:      title,
			URL:        videoURL(id),
			Summary:    fmt.Sprintf("%s 发布了新视频: %s", name, title),
			Target:     rule.Target,
			Payload:    map[string]any{"bvid": id, "title": title, "published_at": item["published_at"]},
		})
	}
	state.SeenIDs = mergeSeen(state.SeenIDs, ids)
	if len(ids) > 0 {
		state.LastID = ids[0]
	}
	return checkResult{Events: events, State: state}, nil
}

func displayName(target Target) string {
	if target.Name != "" {
		return target.Name
	}
	if target.UID > 0 {
		return fmt.Sprintf("UID %d", target.UID)
	}
	return "UP 主"
}
