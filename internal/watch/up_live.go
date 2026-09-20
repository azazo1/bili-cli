package watch

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

func checkUpLive(ctx context.Context, source Source, rule Rule, cred *api.Credential, now time.Time) (checkResult, error) {
	info, err := resolveLiveInfo(ctx, source, rule, cred)
	if err != nil {
		return checkResult{}, err
	}
	state := rule.State
	if !state.Initialized {
		state.LastStatus = info.LiveStatus
		state.LastTitle = info.Title
		if info.RoomID != "" {
			state.LastID = info.RoomID
		}
		return checkResult{State: state}, nil
	}
	events := make([]Event, 0)
	name := displayName(rule.Target)
	if info.UName != "" && rule.Target.Name == "" {
		name = info.UName
	}
	url := liveURL(info.RoomID)
	if state.LastStatus != 1 && info.LiveStatus == 1 {
		title := info.Title
		if title == "" {
			title = "直播"
		}
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindUpLive,
			HappenedAt: now,
			Title:      title,
			URL:        url,
			Summary:    fmt.Sprintf("%s 开播: %s", name, title),
			Target:     rule.Target,
			Payload:    map[string]any{"room_id": info.RoomID, "title": title, "live_status": info.LiveStatus},
		})
	}
	if rule.Options.LiveEnd && state.LastStatus == 1 && info.LiveStatus != 1 {
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindUpLive,
			HappenedAt: now,
			Title:      info.Title,
			URL:        url,
			Summary:    fmt.Sprintf("%s 下播", name),
			Target:     rule.Target,
			Payload:    map[string]any{"room_id": info.RoomID, "title": info.Title, "live_status": info.LiveStatus},
		})
	}
	if rule.Options.TitleChange && state.LastStatus == 1 && info.LiveStatus == 1 && state.LastTitle != "" && info.Title != "" && info.Title != state.LastTitle {
		events = append(events, Event{
			RuleID:     rule.ID,
			Kind:       KindUpLive,
			HappenedAt: now,
			Title:      info.Title,
			URL:        url,
			Summary:    fmt.Sprintf("%s 修改直播标题: %s -> %s", name, state.LastTitle, info.Title),
			Target:     rule.Target,
			Payload:    map[string]any{"room_id": info.RoomID, "title": info.Title, "previous_title": state.LastTitle},
		})
	}
	state.LastStatus = info.LiveStatus
	state.LastTitle = info.Title
	if info.RoomID != "" {
		state.LastID = info.RoomID
	}
	return checkResult{Events: events, State: state}, nil
}

func resolveLiveInfo(ctx context.Context, source Source, rule Rule, cred *api.Credential) (api.LiveRoomInfo, error) {
	if rule.Target.RoomID != "" {
		return source.GetLiveRoomInfo(ctx, rule.Target.RoomID, cred)
	}
	if rule.Target.UID <= 0 {
		return api.LiveRoomInfo{}, api.NewError(api.CodeInvalidInput, "检查直播", "缺少 UID 或房间号")
	}
	user, err := source.GetUserInfo(ctx, rule.Target.UID, cred)
	if err != nil {
		return api.LiveRoomInfo{}, err
	}
	info, ok := liveRoomFromUserInfo(user)
	if !ok {
		return api.LiveRoomInfo{}, api.NewError(api.CodeNotFound, "检查直播", "该用户没有直播间")
	}
	return info, nil
}

func liveRoomFromUserInfo(user map[string]any) (api.LiveRoomInfo, bool) {
	room := model.Map(user["live_room"])
	if len(room) == 0 {
		return api.LiveRoomInfo{}, false
	}
	roomID := model.ToInt64(room["roomid"], model.ToInt64(room["room_id"], 0))
	if roomID <= 0 {
		return api.LiveRoomInfo{}, false
	}
	status := model.ToInt(room["liveStatus"], model.ToInt(room["live_status"], 0))
	return api.LiveRoomInfo{
		RoomID:     strconv.FormatInt(roomID, 10),
		UID:        model.ToInt64(user["mid"], 0),
		Title:      model.String(room["title"]),
		Cover:      model.String(room["cover"]),
		LiveStatus: status,
		UName:      model.String(user["name"]),
	}, true
}
