package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

func checkVideoStat(ctx context.Context, source Source, rule Rule, cred *api.Credential, now time.Time) (checkResult, error) {
	if rule.Target.BVID == "" {
		return checkResult{}, api.NewError(api.CodeInvalidInput, "检查视频数据", "缺少 BV 号")
	}
	info, err := source.GetVideoInfo(ctx, rule.Target.BVID, cred)
	if err != nil {
		return checkResult{}, err
	}
	stat := model.Map(info["stat"])
	values := map[string]int64{
		"view":     model.ToInt64(stat["view"], 0),
		"like":     model.ToInt64(stat["like"], 0),
		"coin":     model.ToInt64(stat["coin"], 0),
		"reply":    model.ToInt64(stat["reply"], 0),
		"favorite": model.ToInt64(stat["favorite"], 0),
		"danmaku":  model.ToInt64(stat["danmaku"], 0),
		"share":    model.ToInt64(stat["share"], 0),
	}
	thresholds := map[string]int64{
		"view":     rule.Options.View,
		"like":     rule.Options.Like,
		"coin":     rule.Options.Coin,
		"reply":    rule.Options.Reply,
		"favorite": rule.Options.Favorite,
		"danmaku":  rule.Options.Danmaku,
		"share":    rule.Options.Share,
	}
	labels := map[string]string{
		"view":     "播放",
		"like":     "点赞",
		"coin":     "投币",
		"reply":    "评论",
		"favorite": "收藏",
		"danmaku":  "弹幕",
		"share":    "分享",
	}
	title := model.String(info["title"])
	if title == "" {
		title = rule.Target.BVID
	}
	state := rule.State
	fired := append([]string{}, state.Fired...)
	events := make([]Event, 0)
	for _, metric := range []string{"view", "like", "coin", "reply", "favorite", "danmaku", "share"} {
		threshold := thresholds[metric]
		if threshold <= 0 {
			continue
		}
		value := values[metric]
		key := fmt.Sprintf("%s:%d", metric, threshold)
		if value < threshold || hasFired(fired, key) {
			continue
		}
		fired = append(fired, key)
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindVideoStat,
			HappenedAt: now,
			Title:      title,
			URL:        videoURL(rule.Target.BVID),
			Summary:    fmt.Sprintf("视频 %s %s达到 %s", title, labels[metric], model.FormatCount(threshold)),
			Target:     rule.Target,
			Payload: map[string]any{
				"bvid":      rule.Target.BVID,
				"metric":    metric,
				"threshold": threshold,
				"value":     value,
			},
		})
	}
	state.Fired = fired
	state.LastID = rule.Target.BVID
	return checkResult{Events: events, State: state}, nil
}
