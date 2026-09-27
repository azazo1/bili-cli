package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/auth"
	"github.com/azazo1/bilibili-cli/internal/model"
	"github.com/azazo1/bilibili-cli/internal/output"
)

func newAccountCommands(app *App) []*cobra.Command {
	var phone, code string
	var countryCode int
	var sms, asJSON, asYAML bool
	login := &cobra.Command{
		Use:     "login [sms]",
		Aliases: []string{"sms-login"},
		Short:   "扫码或手机号短信登录 Bilibili",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			useSMS := sms || strings.TrimSpace(phone) != "" || strings.TrimSpace(code) != ""
			if len(args) == 1 {
				if !strings.EqualFold(args[0], "sms") && !strings.EqualFold(args[0], "phone") {
					return app.invalidInput(cmd, "登录方式仅支持 sms", output.ModeRich)
				}
				useSMS = true
			}
			if !useSMS {
				if _, err := app.Auth.QRLogin(contextOrBackground(cmd.Context()), app.Out.Stdout); err != nil {
					return app.Fail(err, "登录失败", output.ModeRich)
				}
				return nil
			}
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			interactionOut := app.Out.Stdout
			if mode != output.ModeRich {
				interactionOut = app.Out.Stderr
			}
			if interactionOut == nil {
				interactionOut = io.Discard
			}
			credential, loginErr := app.Auth.SMSLogin(contextOrBackground(cmd.Context()), auth.SMSLoginOptions{
				Phone:       phone,
				CountryCode: countryCode,
				Code:        code,
			}, app.In, interactionOut)
			if loginErr != nil {
				return app.Fail(loginErr, "短信登录失败", mode)
			}
			return app.Complete(map[string]any{"authenticated": true, "access_key": credential.AccessKey != ""}, mode, nil)
		},
	}
	login.Flags().StringVar(&phone, "phone", "", "手机号")
	login.Flags().IntVarP(&countryCode, "country-code", "c", 86, "国家区号")
	login.Flags().StringVar(&code, "code", "", "短信验证码")
	login.Flags().BoolVar(&sms, "sms", false, "使用手机号短信登录")
	addStructuredFlags(login, &asJSON, &asYAML)
	logout := &cobra.Command{
		Use:   "logout",
		Short: "注销并清除保存的凭证",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := app.Auth.Clear(); err != nil {
				return app.Fail(err, "注销失败", output.ModeRich)
			}
			fmt.Fprintln(app.Out.Stdout, "已注销, 凭证已清除")
			return nil
		},
	}
	status := newStatusCommand(app)
	return []*cobra.Command{login, logout, status}
}

func newStatusCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "status",
		Short: "检查登录状态",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.Auth.GetCredential(contextOrBackground(cmd.Context()), authModeRead())
			if err != nil {
				return app.Fail(err, "检查登录状态失败", mode)
			}
			if credential == nil {
				return app.Fail(api.NewError(api.CodeNotAuthenticated, "", "未登录. 使用 bili me login 登录"), "", mode)
			}
			info, err := app.API.GetSelfInfo(contextOrBackground(cmd.Context()), credential)
			if err != nil {
				return app.Fail(err, "检查登录状态失败", mode)
			}
			payload := map[string]any{"authenticated": true, "user": model.NormalizeUser(info)}
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "已登录: %s (UID: %s)\n", stringValue(info["uname"]), stringValue(info["mid"]))
			})
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newWhoamiCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "me",
		Short: "查看当前登录用户",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessRead, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			info, err := app.API.GetSelfInfo(contextOrBackground(cmd.Context()), credential)
			if err != nil {
				return app.Fail(err, "获取用户信息失败", mode)
			}
			uid := int64Value(info["mid"], 0)
			relation, err := app.API.GetUserRelationInfo(contextOrBackground(cmd.Context()), uid, credential)
			if err != nil {
				return app.Fail(err, "获取用户信息失败", mode)
			}
			user := model.NormalizeUser(info)
			payload := map[string]any{"user": user, "relation": model.NormalizeRelation(relation)}
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "个人信息\n用户: %s (UID: %d)\n等级: %d  硬币: %d  B币: %d\n粉丝: %s  关注: %s\n", stringValue(user["name"]), uid, intValue(user["level"], 0), intValue(user["coins"], 0), intValue(user["bcoins"], 0), formatCount(relation["follower"]), formatCount(relation["following"]))
			})
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newMeCommand(app *App) *cobra.Command {
	command := newWhoamiCommand(app)
	command.AddCommand(newAccountCommands(app)...)
	command.AddCommand(newCollectionCommands(app)...)
	return command
}

func authModeRead() auth.Mode { return auth.ModeRead }
