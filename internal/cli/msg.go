package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/azazo1/bilibili-cli/internal/api"
	"github.com/azazo1/bilibili-cli/internal/model"
)

func newMsgCommand(app *App) *cobra.Command {
	var maxItems int
	var endTS int64
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:     "msg",
		Aliases: []string{"dm"},
		Short:   "查看和发送私信",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMsgList(cmd, app, maxItems, endTS, asJSON, asYAML)
		},
	}
	bindMsgListFlags(command, &maxItems, &endTS)
	addStructuredFlags(command, &asJSON, &asYAML)
	command.AddCommand(
		newMsgListCommand(app),
		newMsgUnreadCommand(app),
		newMsgShowCommand(app),
		newMsgSendCommand(app),
		newMsgAckCommand(app),
		newMsgRemoveCommand(app),
	)
	return command
}

func newMsgListCommand(app *App) *cobra.Command {
	var maxItems int
	var endTS int64
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "list",
		Short: "列出私信会话",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMsgList(cmd, app, maxItems, endTS, asJSON, asYAML)
		},
	}
	bindMsgListFlags(command, &maxItems, &endTS)
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func bindMsgListFlags(command *cobra.Command, maxItems *int, endTS *int64) {
	command.Flags().IntVar(maxItems, "max", 20, "最多显示会话数")
	command.Flags().Int64Var(endTS, "end-ts", 0, "会话分页游标")
}

func runMsgList(cmd *cobra.Command, app *App, maxItems int, endTS int64, asJSON, asYAML bool) error {
	mode, err := app.mode(cmd, asJSON, asYAML)
	if err != nil {
		return err
	}
	if maxItems < 1 {
		return app.invalidInput(cmd, "--max 必须大于 0", mode)
	}
	if endTS < 0 {
		return app.invalidInput(cmd, "--end-ts 不能为负数", mode)
	}
	credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessRead, mode, "需要登录才能查看私信. 使用 bili me login 登录")
	if err != nil {
		return err
	}
	data, fetchErr := app.API.GetSessionList(contextOrBackground(cmd.Context()), endTS, credential)
	if fetchErr != nil {
		return app.apiFailure(fetchErr, "获取私信会话失败", mode)
	}
	raw := firstMapList(data["sessions"])
	if len(raw) > maxItems {
		raw = raw[:maxItems]
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		items = append(items, model.NormalizeSession(item))
	}
	payload := map[string]any{
		"items":    items,
		"has_more": boolValue(data["has_more"]),
		"end_ts":   int64Value(data["end_ts"], 0),
	}
	return app.CompleteTable(payload, mode, asJSON, asYAML, func(w io.Writer) {
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			name := stringValue(item["name"])
			if name == "" {
				name = "-"
			}
			rows = append(rows, []string{
				strconv.Itoa(intValue(item["unread"], 0)),
				stringValue(item["talker_id"]),
				name,
				displayOrDash(stringValue(item["timestamp"])),
				displayOrDash(stringValue(item["last_text"])),
			})
		}
		app.renderTable(w, "私信会话", []string{"未读", "UID", "用户", "时间", "最后一条"}, rows)
		if boolValue(data["has_more"]) {
			next := int64Value(data["end_ts"], 0)
			if next > 0 {
				fmt.Fprintf(w, "还有更多会话, 使用 bili msg list --end-ts %d 查看下一页\n", next)
			}
		}
	})
}

func newMsgUnreadCommand(app *App) *cobra.Command {
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "unread",
		Short: "查看私信未读数",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessRead, mode, "需要登录才能查看私信. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			data, fetchErr := app.API.GetMsgUnread(contextOrBackground(cmd.Context()), credential)
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取私信未读失败", mode)
			}
			payload := model.NormalizeUnread(data)
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "关注未读: %d\n未关注未读: %d\n合计: %d\n", intValue(payload["follow_unread"], 0), intValue(payload["unfollow_unread"], 0), intValue(payload["total"], 0))
			})
		},
	}
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newMsgShowCommand(app *App) *cobra.Command {
	var maxItems int
	var before int64
	var ack bool
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "show UID_OR_NAME_OR_URL [SEQ]",
		Short: "查看与指定用户的私信",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			if maxItems < 1 {
				return app.invalidInput(cmd, "--max 必须大于 0", mode)
			}
			if before < 0 {
				return app.invalidInput(cmd, "--before 不能为负数", mode)
			}
			if before > 0 && ack {
				return app.invalidInput(cmd, "--ack 不能和 --before 一起使用", mode)
			}
			access := AccessRead
			if ack {
				access = AccessWrite
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), access, mode, "需要登录才能查看私信. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			uid, err := resolveUID(cmd, app, args[0], mode)
			if err != nil {
				return err
			}
			var seq int64
			if len(args) == 2 {
				parsed, parseErr := strconv.ParseInt(args[1], 10, 64)
				if parseErr != nil || parsed <= 0 {
					return app.invalidInput(cmd, "SEQ 必须是正整数", mode)
				}
				seq = parsed
			}
			ctx := contextOrBackground(cmd.Context())
			data, fetchErr := app.API.GetSessionMessages(ctx, uid, maxItems, before, credential)
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "获取私信失败", mode)
			}
			raw := firstMapList(data["messages"])
			items := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				items = append(items, model.NormalizeMessage(item))
			}
			sort.SliceStable(items, func(i, j int) bool {
				left := int64Value(items[i]["seqno"], 0)
				right := int64Value(items[j]["seqno"], 0)
				if left == right {
					return stringValue(items[i]["timestamp"]) < stringValue(items[j]["timestamp"])
				}
				return left < right
			})
			ackSeqno := int64Value(data["max_seqno"], 0)
			if ackSeqno == 0 {
				for _, item := range items {
					if seqno := int64Value(item["seqno"], 0); seqno > ackSeqno {
						ackSeqno = seqno
					}
				}
			}
			if ack {
				if ackErr := app.API.AckSession(ctx, uid, ackSeqno, credential); ackErr != nil {
					return app.apiFailure(ackErr, "标记私信已读失败", mode)
				}
			}
			selfID := int64Value(credential.DedeUserID, 0)
			if seq > 0 {
				var found map[string]any
				for _, item := range items {
					if int64Value(item["seqno"], 0) == seq {
						found = item
						break
					}
				}
				if found == nil {
					return app.Fail(api.NewError(api.CodeNotFound, "", fmt.Sprintf("未找到 seqno=%d. 可加大 --max 或使用 --before", seq)), "", mode)
				}
				payload := map[string]any{
					"talker_id": uid,
					"item":      found,
					"acked":     ack,
				}
				return app.Complete(payload, mode, func(w io.Writer) {
					renderExpandedMessage(w, found, selfID)
				})
			}
			minSeqno := int64Value(data["min_seqno"], 0)
			if minSeqno == 0 {
				for _, item := range items {
					if seqno := int64Value(item["seqno"], 0); seqno > 0 && (minSeqno == 0 || seqno < minSeqno) {
						minSeqno = seqno
					}
				}
			}
			payload := map[string]any{
				"talker_id": uid,
				"items":     items,
				"has_more":  boolValue(data["has_more"]),
				"min_seqno": minSeqno,
				"max_seqno": int64Value(data["max_seqno"], 0),
				"acked":     ack,
			}
			return app.Complete(payload, mode, func(w io.Writer) {
				if len(items) == 0 {
					fmt.Fprintf(w, "与 UID=%d 暂无私信\n", uid)
					return
				}
				for _, item := range items {
					fmt.Fprintf(w, "%s  %s  %s  %s\n", stringValue(item["seqno"]), displayOrDash(stringValue(item["timestamp"])), senderLabel(int64Value(item["sender_id"], 0), selfID), displayOrDash(stringValue(item["text"])))
				}
				if boolValue(data["has_more"]) && minSeqno > 0 {
					fmt.Fprintf(w, "还有更早的消息, 使用 bili msg show %d --before %d\n", uid, minSeqno)
				}
			})
		},
	}
	command.Flags().IntVar(&maxItems, "max", 20, "最多显示消息数")
	command.Flags().Int64Var(&before, "before", 0, "拉取该 seqno 之前的更早消息")
	command.Flags().BoolVar(&ack, "ack", false, "查看后标记已读")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newMsgSendCommand(app *App) *cobra.Command {
	var fromFile string
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "send UID_OR_NAME_OR_URL [TEXT]",
		Short: "发送一条纯文本私信",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessWrite, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			uid, err := resolveUID(cmd, app, args[0], mode)
			if err != nil {
				return err
			}
			text := ""
			if len(args) > 1 {
				text = args[1]
			}
			if fromFile != "" {
				data, readErr := os.ReadFile(fromFile)
				if readErr != nil {
					return app.Fail(readErr, "读取私信文本失败", mode)
				}
				text = string(data)
			}
			text = strings.TrimSpace(text)
			if text == "" {
				return app.invalidInput(cmd, "请提供私信文本. 可用 TEXT 或 --from-file FILE", mode)
			}
			result, fetchErr := app.API.SendTextMessage(contextOrBackground(cmd.Context()), uid, text, credential)
			if fetchErr != nil {
				return app.apiFailure(fetchErr, "发送私信失败", mode)
			}
			payload := model.ActionResult("msg_send", map[string]any{
				"talker_id": uid,
				"text":      text,
				"msg_key":   firstString(result["msg_key"], result["msgKey"]),
			})
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "已发送私信给 UID=%d\n", uid)
			})
		},
	}
	command.Flags().StringVar(&fromFile, "from-file", "", "从文件读取私信文本")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newMsgAckCommand(app *App) *cobra.Command {
	var seq int64
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "ack UID_OR_NAME_OR_URL",
		Short: "标记与指定用户的私信为已读",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			if seq < 0 {
				return app.invalidInput(cmd, "--seq 不能为负数", mode)
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessWrite, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			uid, err := resolveUID(cmd, app, args[0], mode)
			if err != nil {
				return err
			}
			ctx := contextOrBackground(cmd.Context())
			ackSeqno := seq
			if ackSeqno == 0 {
				data, fetchErr := app.API.GetSessionMessages(ctx, uid, 20, 0, credential)
				if fetchErr != nil {
					return app.apiFailure(fetchErr, "获取私信失败", mode)
				}
				ackSeqno = int64Value(data["max_seqno"], 0)
				if ackSeqno == 0 {
					for _, item := range firstMapList(data["messages"]) {
						if seqno := int64Value(item["msg_seqno"], 0); seqno > ackSeqno {
							ackSeqno = seqno
						}
					}
				}
			}
			if ackErr := app.API.AckSession(ctx, uid, ackSeqno, credential); ackErr != nil {
				return app.apiFailure(ackErr, "标记私信已读失败", mode)
			}
			payload := model.ActionResult("msg_ack", map[string]any{"talker_id": uid, "seqno": ackSeqno})
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "已标记与 UID=%d 的私信为已读\n", uid)
			})
		},
	}
	command.Flags().Int64Var(&seq, "seq", 0, "已读游标, 默认取最近一页最大 seqno")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func newMsgRemoveCommand(app *App) *cobra.Command {
	var yes bool
	var asJSON, asYAML bool
	command := &cobra.Command{
		Use:   "rm UID_OR_NAME_OR_URL",
		Short: "删除与指定用户的私信会话",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := app.mode(cmd, asJSON, asYAML)
			if err != nil {
				return err
			}
			credential, err := app.RequireCredential(contextOrBackground(cmd.Context()), AccessDestructive, mode, "未登录. 使用 bili me login 登录")
			if err != nil {
				return err
			}
			uid, err := resolveUID(cmd, app, args[0], mode)
			if err != nil {
				return err
			}
			if !yes && app.ShouldConfirmDangerousAction() && !confirm(app.Out.Stdout, app.In, fmt.Sprintf("确认删除与 UID=%d 的会话吗?", uid)) {
				return nil
			}
			if fetchErr := app.API.RemoveSession(contextOrBackground(cmd.Context()), uid, credential); fetchErr != nil {
				return app.apiFailure(fetchErr, "删除私信会话失败", mode)
			}
			payload := model.ActionResult("msg_rm", map[string]any{"talker_id": uid})
			return app.Complete(payload, mode, func(w io.Writer) {
				fmt.Fprintf(w, "已删除与 UID=%d 的私信会话\n", uid)
			})
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "跳过确认")
	addStructuredFlags(command, &asJSON, &asYAML)
	return command
}

func renderExpandedMessage(w io.Writer, item map[string]any, selfID int64) {
	fmt.Fprintf(w, "seqno: %s\n", stringValue(item["seqno"]))
	fmt.Fprintf(w, "时间: %s\n", displayOrDash(stringValue(item["timestamp"])))
	fmt.Fprintf(w, "发送者: %s\n", senderLabel(int64Value(item["sender_id"], 0), selfID))
	fmt.Fprintf(w, "类型: %s\n", displayOrDash(stringValue(item["type"])))
	fmt.Fprintf(w, "摘要: %s\n", displayOrDash(stringValue(item["text"])))
	detail := mapValue(item["detail"])
	for _, key := range []string{"title", "bvid", "url", "cover", "author", "summary", "duration", "width", "height", "id", "rid", "source"} {
		value := strings.TrimSpace(stringValue(detail[key]))
		if value == "" {
			continue
		}
		fmt.Fprintf(w, "%s: %s\n", key, value)
	}
}

func senderLabel(senderID, selfID int64) string {
	if selfID > 0 && senderID == selfID {
		return "我"
	}
	if senderID <= 0 {
		return "-"
	}
	return strconv.FormatInt(senderID, 10)
}

func displayOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}
