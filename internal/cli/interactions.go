package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/model"
)

func newVideoInteractionCommands(app *App) []*cobra.Command {
	return []*cobra.Command{
		newLikeCommand(app),
		newCoinCommand(app),
		newTripleCommand(app),
	}
}

func newUserInteractionCommands(app *App) []*cobra.Command {
	return []*cobra.Command{
		newFollowCommand(app),
		newUnfollowCommand(app),
	}
}

func newFollowCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "follow UID",
		Short: "关注用户",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			uid, parseErr := strconv.ParseInt(args[0], 10, 64)
			if parseErr != nil || uid <= 0 {
				return app.invalidInput(cmd, "UID 必须是正整数", mode)
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessWrite, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			if fetchErr := app.API.FollowUser(contextOrBackground(cmd.Context()), uid, credential); fetchErr != nil {
				return app.apiFailure(fetchErr, "关注用户失败", mode)
			}
			payload := model.ActionResult("follow", map[string]any{"uid": uid})
			return app.Complete(payload, mode, func(w io.Writer) { fmt.Fprintf(w, "已关注 UID=%d\n", uid) })
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newLikeCommand(app *App) *cobra.Command {
	var undo, asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "like BV_OR_URL",
		Short: "点赞视频",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessWrite, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			bvid, err := app.extractBVID(cmd, args[0], mode)
			if err != nil {
				return err
			}
			if fetchErr := app.API.LikeVideo(contextOrBackground(cmd.Context()), bvid, undo, credential); fetchErr != nil {
				return app.apiFailure(fetchErr, "操作失败", mode)
			}
			action := "like"
			if undo {
				action = "unlike"
			}
			payload := model.ActionResult(action, map[string]any{"bvid": bvid, "undo": undo})
			return app.Complete(payload, mode, func(w io.Writer) {
				if undo {
					fmt.Fprintf(w, "已取消点赞: %s\n", bvid)
				} else {
					fmt.Fprintf(w, "已点赞: %s\n", bvid)
				}
			})
		},
	}
	command.Flags().BoolVar(&undo, "undo", false, "取消点赞")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newCoinCommand(app *App) *cobra.Command {
	var coins int
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "coin BV_OR_URL",
		Short: "给视频投币",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			if coins < 1 || coins > 2 {
				return app.invalidInput(cmd, "--num 必须是 1 或 2", mode)
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessWrite, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			bvid, err := app.extractBVID(cmd, args[0], mode)
			if err != nil {
				return err
			}
			if fetchErr := app.API.CoinVideo(contextOrBackground(cmd.Context()), bvid, coins, credential); fetchErr != nil {
				return app.apiFailure(fetchErr, "投币失败", mode)
			}
			payload := model.ActionResult("coin", map[string]any{"bvid": bvid, "coins": coins})
			return app.Complete(payload, mode, func(w io.Writer) { fmt.Fprintf(w, "已投 %d 枚硬币: %s\n", coins, bvid) })
		},
	}
	command.Flags().IntVarP(&coins, "num", "n", 1, "投币数量: 1 或 2")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newTripleCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "triple BV_OR_URL",
		Short: "一键三连",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessWrite, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			bvid, err := app.extractBVID(cmd, args[0], mode)
			if err != nil {
				return err
			}
			result, fetchErr := app.API.TripleVideo(contextOrBackground(cmd.Context()), bvid, credential)
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "三连失败", mode)
			}
			payload := model.ActionResult("triple", map[string]any{"bvid": bvid, "result": map[string]any{"like": boolValue(result["like"]), "coin": boolValue(result["coin"]), "favorite": boolValue(firstNonNil(result["multiply"], result["fav"]))}})
			return app.Complete(payload, mode, func(w io.Writer) { fmt.Fprintf(w, "一键三连成功: %s\n", bvid) })
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newUnfollowCommand(app *App) *cobra.Command {
	var yes bool
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "unfollow UID",
		Short: "取消关注用户",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			uid, parseErr := strconv.ParseInt(args[0], 10, 64)
			if parseErr != nil || uid <= 0 {
				return app.invalidInput(cmd, "UID 必须是正整数", mode)
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessDestructive, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			if !yes && app.ShouldConfirmDangerousAction() && !confirm(app.Out.Stdout, app.In, fmt.Sprintf("确认取消关注 UID=%d 吗?", uid)) {
				return nil
			}
			if fetchErr := app.API.UnfollowUser(contextOrBackground(cmd.Context()), uid, credential); fetchErr != nil {
				return app.apiFailure(fetchErr, "取消关注失败", mode)
			}
			payload := model.ActionResult("unfollow", map[string]any{"uid": uid})
			return app.Complete(payload, mode, func(w io.Writer) { fmt.Fprintf(w, "已取消关注 UID=%d\n", uid) })
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "跳过确认")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}
