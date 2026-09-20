package watch

import (
	"context"

	"github.com/azazo1/bilibili-cli/internal/api"
)

var _ Source = (*api.Client)(nil)

type Source interface {
	GetUserInfo(ctx context.Context, uid int64, cred *api.Credential) (map[string]any, error)
	GetUserVideos(ctx context.Context, uid int64, count int, cred *api.Credential) ([]map[string]any, error)
	GetVideoInfo(ctx context.Context, bvid string, cred *api.Credential) (map[string]any, error)
	GetLiveRoomInfo(ctx context.Context, roomID string, cred *api.Credential) (api.LiveRoomInfo, error)
	GetUserDynamics(ctx context.Context, uid int64, offset int64, cred *api.Credential) (map[string]any, error)
	Search(ctx context.Context, keyword string, options api.SearchOptions) ([]map[string]any, error)
	GetUserList(ctx context.Context, reference api.UserListReference, page int, cred *api.Credential) (api.UserList, error)
}
