package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
	"github.com/azazo1/bilibili-cli/internal/watch"
)

func newWatchAddCommand(app *App) *cobra.Command {
	command := &cobra.Command{
		Use:   "add",
		Short: "添加订阅规则",
	}
	command.AddCommand(
		newWatchAddUpCommand(app),
		newWatchAddVideoCommand(app),
		newWatchAddSearchCommand(app),
		newWatchAddListCommand(app),
	)
	return command
}

func newWatchAddUpCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	var watchVideo, watchLive, watchDynamic bool
	var liveEnd, titleChange bool
	var titleContains string
	command := &cobra.Command{
		Use:   "up UID_OR_NAME_OR_URL",
		Short: "订阅 UP 主新视频, 开播或动态",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			defaulted := !watchVideo && !watchLive && !watchDynamic
			if defaulted {
				watchVideo, watchLive = true, true
			}
			uid, err := resolveUID(cmd, app, args[0], mode)
			if err != nil {
				return err
			}
			ctx := contextOrBackground(cmd.Context())
			credential := app.OptionalCredential(ctx)
			info, fetchErr := app.API.GetUserInfo(ctx, uid, credential)
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取用户信息失败", mode)
			}
			name := strings.TrimSpace(stringValue(info["name"]))
			target := watch.Target{UID: uid, Name: name}
			room, hasRoom := liveRoomFromUser(info)
			if hasRoom {
				target.RoomID = room.RoomID
			}
			rules := make([]watch.Rule, 0, 3)
			notes := make([]string, 0, 2)
			if watchVideo {
				rules = append(rules, watch.Rule{
					Kind:    watch.KindUpVideo,
					Label:   watchLabel(name, "新视频"),
					Target:  target,
					Options: watch.Options{TitleContains: strings.TrimSpace(titleContains)},
				})
			}
			if watchLive {
				if !hasRoom {
					if defaulted {
						notes = append(notes, "该用户没有直播间, 已跳过开播订阅")
					} else {
						return app.Fail(api.NewError(api.CodeNotFound, "", "该用户没有直播间"), "", mode)
					}
				} else {
					rules = append(rules, watch.Rule{
						Kind:    watch.KindUpLive,
						Label:   watchLabel(name, "直播"),
						Target:  target,
						Options: watch.Options{LiveEnd: liveEnd, TitleChange: titleChange},
					})
				}
			}
			if watchDynamic {
				rules = append(rules, watch.Rule{
					Kind:   watch.KindUpDynamic,
					Label:  watchLabel(name, "动态"),
					Target: target,
				})
			}
			if len(rules) == 0 {
				return app.invalidInput(cmd, "没有可添加的订阅规则", mode)
			}
			added, addErr := app.watchStore().Add(rules...)
			if addErr != nil {
				return app.Fail(addErr, "保存订阅失败", mode)
			}
			for _, rule := range added {
				app.Logger.Info("已添加订阅规则", "rule_id", rule.ID, "kind", string(rule.Kind), "uid", uid)
			}
			payload := map[string]any{"rules": added, "notes": notes}
			return app.Complete(payload, mode, func(w io.Writer) {
				renderAddedRules(w, added)
				for _, note := range notes {
					fmt.Fprintln(w, note)
				}
			})
		},
	}
	command.Flags().BoolVar(&watchVideo, "video", false, "监视新视频")
	command.Flags().BoolVar(&watchLive, "live", false, "监视开播")
	command.Flags().BoolVar(&watchDynamic, "dynamic", false, "监视动态")
	command.Flags().BoolVar(&liveEnd, "live-end", false, "同时监视下播")
	command.Flags().BoolVar(&titleChange, "live-title-change", false, "同时监视直播标题变化")
	command.Flags().StringVar(&titleContains, "title-contains", "", "仅匹配标题包含该文本的视频")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWatchAddVideoCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	var view, like, coin, reply, favorite, danmaku, share int64
	command := &cobra.Command{
		Use:   "video BV_OR_URL",
		Short: "订阅视频播放, 点赞, 评论等数据阈值",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			options := watch.Options{
				View:     view,
				Like:     like,
				Coin:     coin,
				Reply:    reply,
				Favorite: favorite,
				Danmaku:  danmaku,
				Share:    share,
			}
			if !hasWatchThreshold(options) {
				return app.invalidInput(cmd, "至少指定一个阈值, 例如 --like 10000", mode)
			}
			bvid, err := app.extractBVID(cmd, args[0], mode)
			if err != nil {
				return err
			}
			ctx := contextOrBackground(cmd.Context())
			info, fetchErr := app.API.GetVideoInfo(ctx, bvid, app.OptionalCredential(ctx))
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取视频信息失败", mode)
			}
			title := strings.TrimSpace(stringValue(info["title"]))
			rule := watch.Rule{
				Kind:    watch.KindVideoStat,
				Label:   watchLabel(title, "数据"),
				Target:  watch.Target{BVID: bvid, Name: title},
				Options: options,
			}
			added, addErr := app.watchStore().Add(rule)
			if addErr != nil {
				return app.Fail(addErr, "保存订阅失败", mode)
			}
			app.Logger.Info("已添加订阅规则", "rule_id", added[0].ID, "kind", string(rule.Kind), "bvid", bvid)
			return app.Complete(map[string]any{"rules": added}, mode, func(w io.Writer) {
				renderAddedRules(w, added)
			})
		},
	}
	command.Flags().Int64Var(&view, "view", 0, "播放量阈值")
	command.Flags().Int64Var(&like, "like", 0, "点赞阈值")
	command.Flags().Int64Var(&coin, "coin", 0, "投币阈值")
	command.Flags().Int64Var(&reply, "reply", 0, "评论阈值")
	command.Flags().Int64Var(&favorite, "favorite", 0, "收藏阈值")
	command.Flags().Int64Var(&danmaku, "danmaku", 0, "弹幕阈值")
	command.Flags().Int64Var(&share, "share", 0, "分享阈值")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWatchAddSearchCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	var titleContains string
	command := &cobra.Command{
		Use:   "search QUERY",
		Short: "订阅关键词最新视频",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			query := strings.TrimSpace(args[0])
			if query == "" {
				return app.invalidInput(cmd, "搜索关键词不能为空", mode)
			}
			rule := watch.Rule{
				Kind:    watch.KindSearchKeyword,
				Label:   watchLabel(query, "搜索"),
				Target:  watch.Target{Query: query},
				Options: watch.Options{TitleContains: strings.TrimSpace(titleContains)},
			}
			added, addErr := app.watchStore().Add(rule)
			if addErr != nil {
				return app.Fail(addErr, "保存订阅失败", mode)
			}
			app.Logger.Info("已添加订阅规则", "rule_id", added[0].ID, "kind", string(rule.Kind), "query", query)
			return app.Complete(map[string]any{"rules": added}, mode, func(w io.Writer) {
				renderAddedRules(w, added)
			})
		},
	}
	command.Flags().StringVar(&titleContains, "title-contains", "", "仅匹配标题包含该文本的视频")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWatchAddListCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "list UID/SID_OR_URL",
		Short: "订阅合集或系列更新",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			reference, err := resolveUserListsReference(cmd, app, args[0], mode)
			if err != nil {
				return err
			}
			if !reference.HasList() {
				return app.invalidInput(cmd, "请指定合集或系列, 例如 UID/SID 或空间列表链接", mode)
			}
			ctx := contextOrBackground(cmd.Context())
			list, fetchErr := app.API.GetUserList(ctx, reference, 1, app.OptionalCredential(ctx))
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取用户列表失败", mode)
			}
			rule := watch.Rule{
				Kind:  watch.KindListUpdate,
				Label: watchLabel(list.Metadata.Title, "合集"),
				Target: watch.Target{
					UID:      list.Metadata.OwnerID,
					ListID:   list.Metadata.ID,
					ListKind: string(reference.KindHint),
					ListName: list.Metadata.Title,
					Name:     list.Metadata.Title,
				},
			}
			added, addErr := app.watchStore().Add(rule)
			if addErr != nil {
				return app.Fail(addErr, "保存订阅失败", mode)
			}
			app.Logger.Info("已添加订阅规则", "rule_id", added[0].ID, "kind", string(rule.Kind), "uid", list.Metadata.OwnerID, "list_id", list.Metadata.ID)
			return app.Complete(map[string]any{"rules": added}, mode, func(w io.Writer) {
				renderAddedRules(w, added)
			})
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func liveRoomFromUser(info map[string]any) (api.LiveRoomInfo, bool) {
	room := model.Map(info["live_room"])
	if len(room) == 0 {
		return api.LiveRoomInfo{}, false
	}
	roomID := int64Value(room["roomid"], int64Value(room["room_id"], 0))
	if roomID <= 0 {
		return api.LiveRoomInfo{}, false
	}
	return api.LiveRoomInfo{
		RoomID:     fmt.Sprintf("%d", roomID),
		UID:        int64Value(info["mid"], 0),
		Title:      stringValue(room["title"]),
		LiveStatus: intValue(room["liveStatus"], intValue(room["live_status"], 0)),
		UName:      stringValue(info["name"]),
	}, true
}

func hasWatchThreshold(options watch.Options) bool {
	return options.View > 0 || options.Like > 0 || options.Coin > 0 || options.Reply > 0 || options.Favorite > 0 || options.Danmaku > 0 || options.Share > 0
}

func watchLabel(name, kind string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return kind
	}
	return name + " " + kind
}

func renderAddedRules(w io.Writer, rules []watch.Rule) {
	for _, rule := range rules {
		fmt.Fprintf(w, "已添加订阅 %d: %s (%s)\n", rule.ID, rule.Kind.Label(), rule.Label)
	}
}
