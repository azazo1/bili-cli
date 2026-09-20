package watch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/azazo1/bilibili-cli/internal/api"
)

type fakeSource struct {
	videos   map[int64][]map[string]any
	users    map[int64]map[string]any
	rooms    map[string]api.LiveRoomInfo
	video    map[string]map[string]any
	dynamics map[int64]map[string]any
	search   map[string][]map[string]any
	lists    map[int64]api.UserList
	errs     map[string]error
}

func (f *fakeSource) GetUserInfo(_ context.Context, uid int64, _ *api.Credential) (map[string]any, error) {
	if err := f.errs["user"]; err != nil {
		return nil, err
	}
	return f.users[uid], nil
}

func (f *fakeSource) GetUserVideos(_ context.Context, uid int64, _ int, _ *api.Credential) ([]map[string]any, error) {
	if err := f.errs["videos"]; err != nil {
		return nil, err
	}
	return f.videos[uid], nil
}

func (f *fakeSource) GetVideoInfo(_ context.Context, bvid string, _ *api.Credential) (map[string]any, error) {
	if err := f.errs["video"]; err != nil {
		return nil, err
	}
	return f.video[bvid], nil
}

func (f *fakeSource) GetLiveRoomInfo(_ context.Context, roomID string, _ *api.Credential) (api.LiveRoomInfo, error) {
	if err := f.errs["live"]; err != nil {
		return api.LiveRoomInfo{}, err
	}
	return f.rooms[roomID], nil
}

func (f *fakeSource) GetUserDynamics(_ context.Context, uid int64, _ int64, _ *api.Credential) (map[string]any, error) {
	if err := f.errs["dynamic"]; err != nil {
		return nil, err
	}
	return f.dynamics[uid], nil
}

func (f *fakeSource) Search(_ context.Context, keyword string, _ api.SearchOptions) ([]map[string]any, error) {
	if err := f.errs["search"]; err != nil {
		return nil, err
	}
	return f.search[keyword], nil
}

func (f *fakeSource) GetUserList(_ context.Context, reference api.UserListReference, _ int, _ *api.Credential) (api.UserList, error) {
	if err := f.errs["list"]; err != nil {
		return api.UserList{}, err
	}
	return f.lists[reference.ListID], nil
}

func TestEngineSnapshotsThenEmitsNewVideo(t *testing.T) {
	store := newTestStore(t)
	source := &fakeSource{videos: map[int64][]map[string]any{
		42: {{"bvid": "BV1oldxxxxxx", "title": "old"}},
	}}
	engine := newTestEngine(store, source)
	if _, err := store.Add(Rule{Kind: KindUpVideo, Target: Target{UID: 42, Name: "up"}}); err != nil {
		t.Fatal(err)
	}
	first, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(first.Events) != 0 {
		t.Fatalf("first check = %#v, %v", first, err)
	}
	source.videos[42] = []map[string]any{
		{"bvid": "BV1newxxxxxx", "title": "new"},
		{"bvid": "BV1oldxxxxxx", "title": "old"},
	}
	second, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(second.Events) != 1 || second.Events[0].Payload["bvid"] != "BV1newxxxxxx" {
		t.Fatalf("second check = %#v, %v", second, err)
	}
	third, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(third.Events) != 0 {
		t.Fatalf("third check = %#v, %v", third, err)
	}
}

func TestEngineLiveStartAndEnd(t *testing.T) {
	store := newTestStore(t)
	source := &fakeSource{rooms: map[string]api.LiveRoomInfo{
		"5440": {RoomID: "5440", Title: "first", LiveStatus: 0, UName: "up"},
	}}
	engine := newTestEngine(store, source)
	if _, err := store.Add(Rule{Kind: KindUpLive, Target: Target{UID: 1, Name: "up", RoomID: "5440"}, Options: Options{LiveEnd: true, TitleChange: true}}); err != nil {
		t.Fatal(err)
	}
	if report, err := engine.Check(context.Background(), nil, nil); err != nil || len(report.Events) != 0 {
		t.Fatalf("snapshot live = %#v, %v", report, err)
	}
	source.rooms["5440"] = api.LiveRoomInfo{RoomID: "5440", Title: "on", LiveStatus: 1, UName: "up"}
	start, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(start.Events) != 1 || start.Events[0].Summary == "" {
		t.Fatalf("live start = %#v, %v", start, err)
	}
	source.rooms["5440"] = api.LiveRoomInfo{RoomID: "5440", Title: "changed", LiveStatus: 1, UName: "up"}
	title, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(title.Events) != 1 {
		t.Fatalf("title change = %#v, %v", title, err)
	}
	source.rooms["5440"] = api.LiveRoomInfo{RoomID: "5440", Title: "changed", LiveStatus: 0, UName: "up"}
	end, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(end.Events) != 1 {
		t.Fatalf("live end = %#v, %v", end, err)
	}
}

func TestEngineVideoStatFiresOnce(t *testing.T) {
	store := newTestStore(t)
	source := &fakeSource{video: map[string]map[string]any{
		"BV1ABcsztEcY": {"title": "demo", "stat": map[string]any{"like": float64(12000), "view": float64(10)}},
	}}
	engine := newTestEngine(store, source)
	if _, err := store.Add(Rule{Kind: KindVideoStat, Target: Target{BVID: "BV1ABcsztEcY"}, Options: Options{Like: 10000}}); err != nil {
		t.Fatal(err)
	}
	first, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(first.Events) != 1 {
		t.Fatalf("first stat = %#v, %v", first, err)
	}
	second, err := engine.Check(context.Background(), nil, nil)
	if err != nil || len(second.Events) != 0 {
		t.Fatalf("second stat = %#v, %v", second, err)
	}
}

func TestEngineRateLimitedSkipsRemaining(t *testing.T) {
	store := newTestStore(t)
	source := &fakeSource{
		videos: map[int64][]map[string]any{1: {{"bvid": "BV1aaaaaaaaa", "title": "a"}}},
		errs:   map[string]error{},
	}
	engine := newTestEngine(store, source)
	if _, err := store.Add(
		Rule{Kind: KindUpVideo, Target: Target{UID: 1}},
		Rule{Kind: KindVideoStat, Target: Target{BVID: "BV1ABcsztEcY"}, Options: Options{Like: 1}},
	); err != nil {
		t.Fatal(err)
	}
	source.errs["video"] = api.NewError(api.CodeRateLimited, "检查视频数据", "请求过于频繁")
	report, err := engine.Check(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rules != 1 {
		t.Fatalf("rate limited rules = %d, want 1, warnings=%#v", report.Rules, report.Warnings)
	}
	if len(report.Warnings) < 2 {
		t.Fatalf("expected skip warning: %#v", report.Warnings)
	}
}

func TestEngineSkipsDisabledUnlessRequested(t *testing.T) {
	store := newTestStore(t)
	source := &fakeSource{search: map[string][]map[string]any{"go": {{"bvid": "BV1aaaaaaaaa", "title": "go"}}}}
	engine := newTestEngine(store, source)
	if _, err := store.Add(Rule{Kind: KindSearchKeyword, Target: Target{Query: "go"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetEnabled(1, false); err != nil {
		t.Fatal(err)
	}
	skipped, err := engine.Check(context.Background(), nil, nil)
	if err != nil || skipped.Rules != 0 {
		t.Fatalf("disabled check = %#v, %v", skipped, err)
	}
	forced, err := engine.Check(context.Background(), nil, []int{1})
	if err != nil || forced.Rules != 1 {
		t.Fatalf("forced check = %#v, %v", forced, err)
	}
}

func newTestEngine(store *Store, source Source) *Engine {
	return &Engine{
		Store:  store,
		Source: source,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC) },
	}
}
