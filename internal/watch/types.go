package watch

import (
	"fmt"
	"time"
)

const CurrentVersion = 1
const maxSeenIDs = 50

type Kind string

const (
	KindUpVideo       Kind = "up.video"
	KindUpLive        Kind = "up.live"
	KindVideoStat     Kind = "video.stat"
	KindUpDynamic     Kind = "up.dynamic"
	KindSearchKeyword Kind = "search.keyword"
	KindListUpdate    Kind = "list.update"
)

func (k Kind) Label() string {
	switch k {
	case KindUpVideo:
		return "新视频"
	case KindUpLive:
		return "直播"
	case KindVideoStat:
		return "视频数据"
	case KindUpDynamic:
		return "动态"
	case KindSearchKeyword:
		return "搜索"
	case KindListUpdate:
		return "合集"
	default:
		return string(k)
	}
}

type Rule struct {
	ID        int       `json:"id"`
	Kind      Kind      `json:"kind"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	Label     string    `json:"label"`
	Target    Target    `json:"target"`
	Options   Options   `json:"options"`
	State     State     `json:"state"`
}

type Target struct {
	UID      int64  `json:"uid,omitempty"`
	Name     string `json:"name,omitempty"`
	BVID     string `json:"bvid,omitempty"`
	RoomID   string `json:"room_id,omitempty"`
	ListID   int64  `json:"list_id,omitempty"`
	ListKind string `json:"list_kind,omitempty"`
	ListName string `json:"list_name,omitempty"`
	Query    string `json:"query,omitempty"`
}

func (t Target) Display() string {
	if t.Name != "" && t.UID > 0 {
		return t.Name
	}
	if t.BVID != "" {
		return t.BVID
	}
	if t.ListName != "" {
		return t.ListName
	}
	if t.Query != "" {
		return t.Query
	}
	if t.RoomID != "" {
		return "房间 " + t.RoomID
	}
	if t.UID > 0 {
		return fmt.Sprintf("UID %d", t.UID)
	}
	return "-"
}

type Options struct {
	TitleContains string `json:"title_contains,omitempty"`
	LiveEnd       bool   `json:"live_end,omitempty"`
	TitleChange   bool   `json:"title_change,omitempty"`
	View          int64  `json:"view,omitempty"`
	Like          int64  `json:"like,omitempty"`
	Coin          int64  `json:"coin,omitempty"`
	Reply         int64  `json:"reply,omitempty"`
	Favorite      int64  `json:"favorite,omitempty"`
	Danmaku       int64  `json:"danmaku,omitempty"`
	Share         int64  `json:"share,omitempty"`
}

type State struct {
	Initialized   bool      `json:"initialized"`
	LastCheckedAt time.Time `json:"last_checked_at,omitempty"`
	SeenIDs       []string  `json:"seen_ids,omitempty"`
	LastID        string    `json:"last_id,omitempty"`
	LastStatus    int       `json:"last_status,omitempty"`
	LastTitle     string    `json:"last_title,omitempty"`
	Fired         []string  `json:"fired,omitempty"`
}

type Event struct {
	RuleID     int            `json:"rule_id"`
	Kind       Kind           `json:"kind"`
	HappenedAt time.Time      `json:"happened_at"`
	Title      string         `json:"title"`
	URL        string         `json:"url"`
	Summary    string         `json:"summary"`
	Target     Target         `json:"target"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type Warning struct {
	RuleID  int    `json:"rule_id"`
	Kind    Kind   `json:"kind"`
	Message string `json:"message"`
}

type Report struct {
	CheckedAt time.Time `json:"checked_at"`
	Rules     int       `json:"rules"`
	Events    []Event   `json:"events"`
	Warnings  []Warning `json:"warnings,omitempty"`
}

type File struct {
	Version int    `json:"version"`
	NextID  int    `json:"next_id"`
	Rules   []Rule `json:"rules"`
}

type checkResult struct {
	Events []Event
	State  State
}
