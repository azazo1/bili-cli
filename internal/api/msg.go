package api

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func messageRequestHeaders() http.Header {
	headers := make(http.Header)
	headers.Set("Origin", "https://message.bilibili.com")
	headers.Set("Referer", "https://message.bilibili.com/")
	return headers
}

func (c *Client) requestVC(ctx context.Context, method, path string, query url.Values, form url.Values, cred *Credential, out any) error {
	return c.requestAtBaseWithHeaders(ctx, method, c.VCBaseURL, defaultVCBaseURL, path, query, form, cred, messageRequestHeaders(), out)
}

func (c *Client) GetMsgUnread(ctx context.Context, cred *Credential) (map[string]any, error) {
	if err := requireCredential("获取私信未读", cred, false); err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("unread_type", "0")
	query.Set("build", "0")
	query.Set("mobi_app", "web")
	var data map[string]any
	if err := c.requestVC(ctx, http.MethodGet, "/session_svr/v1/session_svr/single_unread", query, nil, c.credentialWithDevice(ctx, cred), &data); err != nil {
		return nil, withAction("获取私信未读", err)
	}
	return mapValue(data), nil
}

func (c *Client) GetSessionList(ctx context.Context, endTS int64, cred *Credential) (map[string]any, error) {
	if err := requireCredential("获取私信会话", cred, false); err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("session_type", "1")
	query.Set("group_fold", "1")
	query.Set("unfollow_fold", "0")
	query.Set("sort_rule", "2")
	query.Set("build", "0")
	query.Set("mobi_app", "web")
	if endTS > 0 {
		query.Set("end_ts", strconv.FormatInt(endTS, 10))
	}
	signed, err := c.signWBI(ctx, query, cred)
	if err != nil {
		return nil, withAction("获取私信会话", err)
	}
	var data map[string]any
	if err := c.requestVC(ctx, http.MethodGet, "/session_svr/v1/session_svr/get_sessions", signed, nil, c.credentialWithDevice(ctx, cred), &data); err != nil {
		return nil, withAction("获取私信会话", err)
	}
	sessions := mapList(data["session_list"])
	uids := make([]string, 0, len(sessions))
	seen := map[int64]bool{}
	for _, session := range sessions {
		talkerID := int64Value(session["talker_id"], 0)
		if talkerID <= 0 || seen[talkerID] {
			continue
		}
		seen[talkerID] = true
		uids = append(uids, strconv.FormatInt(talkerID, 10))
	}
	cards := c.getUserCards(ctx, uids, cred)
	merged := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		item := copyMap(session)
		talkerID := int64Value(item["talker_id"], 0)
		if card := cards[talkerID]; card != nil {
			if name := stringValue(card["name"]); name != "" {
				item["name"] = name
			}
			if face := stringValue(card["face"]); face != "" {
				item["face"] = face
			}
		}
		merged = append(merged, item)
	}
	result := map[string]any{
		"sessions": merged,
		"has_more": intValue(data["has_more"], 0) != 0,
	}
	if len(sessions) > 0 {
		result["end_ts"] = int64Value(sessions[len(sessions)-1]["session_ts"], 0)
	}
	return result, nil
}

func (c *Client) GetSessionMessages(ctx context.Context, talkerID int64, size int, endSeqno int64, cred *Credential) (map[string]any, error) {
	if err := requireCredential("获取私信", cred, false); err != nil {
		return nil, err
	}
	if talkerID <= 0 {
		return nil, NewError(CodeInvalidInput, "获取私信", "talker_id 无效")
	}
	if size <= 0 {
		size = 20
	}
	if endSeqno < 0 {
		return nil, NewError(CodeInvalidInput, "获取私信", "end_seqno 无效")
	}
	query := url.Values{}
	query.Set("talker_id", strconv.FormatInt(talkerID, 10))
	query.Set("session_type", "1")
	query.Set("size", strconv.Itoa(size))
	query.Set("sender_device_id", "1")
	query.Set("build", "0")
	query.Set("mobi_app", "web")
	if endSeqno > 0 {
		query.Set("begin_seqno", "0")
		query.Set("end_seqno", strconv.FormatInt(endSeqno, 10))
	}
	signed, err := c.signWBI(ctx, query, cred)
	if err != nil {
		return nil, withAction("获取私信", err)
	}
	var data map[string]any
	if err := c.requestVC(ctx, http.MethodGet, "/svr_sync/v1/svr_sync/fetch_session_msgs", signed, nil, c.credentialWithDevice(ctx, cred), &data); err != nil {
		return nil, withAction("获取私信", err)
	}
	messages := mapList(data["messages"])
	if len(messages) == 0 {
		messages = mapList(data["msg_list"])
	}
	return map[string]any{
		"talker_id": talkerID,
		"messages":  messages,
		"has_more":  intValue(data["has_more"], 0) != 0,
		"min_seqno": int64Value(data["min_seqno"], 0),
		"max_seqno": int64Value(data["max_seqno"], 0),
	}, nil
}

func (c *Client) SendTextMessage(ctx context.Context, receiverID int64, text string, cred *Credential) (map[string]any, error) {
	if err := requireCredential("发送私信", cred, true); err != nil {
		return nil, err
	}
	if receiverID <= 0 {
		return nil, NewError(CodeInvalidInput, "发送私信", "receiver_id 无效")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, NewError(CodeInvalidInput, "发送私信", "消息内容为空")
	}
	senderID, err := c.senderUID(ctx, cred)
	if err != nil {
		return nil, err
	}
	devID := randomUUID()
	content, err := json.Marshal(map[string]string{"content": text})
	if err != nil {
		return nil, NewError(CodeInvalidInput, "发送私信", "编码消息内容失败")
	}
	form := url.Values{}
	form.Set("msg[sender_uid]", strconv.FormatInt(senderID, 10))
	form.Set("msg[receiver_id]", strconv.FormatInt(receiverID, 10))
	form.Set("msg[receiver_type]", "1")
	form.Set("msg[msg_type]", "1")
	form.Set("msg[msg_status]", "0")
	form.Set("msg[content]", string(content))
	form.Set("msg[timestamp]", strconv.FormatInt(time.Now().Unix(), 10))
	form.Set("msg[new_face_version]", "1")
	form.Set("msg[dev_id]", devID)
	form.Set("from_firework", "0")
	form.Set("build", "0")
	form.Set("mobi_app", "web")
	form.Set("csrf", cred.BiliJct)
	form.Set("csrf_token", cred.BiliJct)
	query := url.Values{}
	query.Set("w_sender_uid", strconv.FormatInt(senderID, 10))
	query.Set("w_receiver_id", strconv.FormatInt(receiverID, 10))
	query.Set("w_dev_id", devID)
	signed, err := c.signWBI(ctx, query, cred)
	if err != nil {
		return nil, withAction("发送私信", err)
	}
	var data map[string]any
	if err := c.requestVC(ctx, http.MethodPost, "/web_im/v1/web_im/send_msg", signed, form, c.credentialWithDevice(ctx, cred), &data); err != nil {
		return nil, withAction("发送私信", err)
	}
	return mapValue(data), nil
}

func (c *Client) AckSession(ctx context.Context, talkerID, ackSeqno int64, cred *Credential) error {
	if err := requireCredential("标记私信已读", cred, true); err != nil {
		return err
	}
	if talkerID <= 0 {
		return NewError(CodeInvalidInput, "标记私信已读", "talker_id 无效")
	}
	query := url.Values{}
	query.Set("talker_id", strconv.FormatInt(talkerID, 10))
	query.Set("session_type", "1")
	query.Set("ack_seqno", strconv.FormatInt(ackSeqno, 10))
	query.Set("build", "0")
	query.Set("mobi_app", "web")
	query.Set("csrf", cred.BiliJct)
	query.Set("csrf_token", cred.BiliJct)
	signed, err := c.signWBI(ctx, query, cred)
	if err != nil {
		return withAction("标记私信已读", err)
	}
	if err := c.requestVC(ctx, http.MethodGet, "/session_svr/v1/session_svr/update_ack", signed, nil, c.credentialWithDevice(ctx, cred), nil); err != nil {
		return withAction("标记私信已读", err)
	}
	return nil
}

func (c *Client) RemoveSession(ctx context.Context, talkerID int64, cred *Credential) error {
	if err := requireCredential("删除私信会话", cred, true); err != nil {
		return err
	}
	if talkerID <= 0 {
		return NewError(CodeInvalidInput, "删除私信会话", "talker_id 无效")
	}
	form := url.Values{}
	form.Set("talker_id", strconv.FormatInt(talkerID, 10))
	form.Set("session_type", "1")
	form.Set("build", "0")
	form.Set("mobi_app", "web")
	form.Set("csrf", cred.BiliJct)
	form.Set("csrf_token", cred.BiliJct)
	signed, err := c.signWBI(ctx, form, cred)
	if err != nil {
		return withAction("删除私信会话", err)
	}
	if err := c.requestVC(ctx, http.MethodPost, "/session_svr/v1/session_svr/remove_session", nil, signed, c.credentialWithDevice(ctx, cred), nil); err != nil {
		return withAction("删除私信会话", err)
	}
	return nil
}

func (c *Client) getUserCards(ctx context.Context, uids []string, cred *Credential) map[int64]map[string]any {
	result := map[int64]map[string]any{}
	if len(uids) == 0 {
		return result
	}
	query := url.Values{}
	query.Set("uids", strings.Join(uids, ","))
	query.Set("build", "0")
	query.Set("mobi_app", "web")
	if cred != nil && cred.BiliJct != "" {
		query.Set("csrf", cred.BiliJct)
	}
	var data any
	if err := c.requestVC(ctx, http.MethodGet, "/account/v1/user/cards", query, nil, c.credentialWithDevice(ctx, cred), &data); err != nil {
		if c.Logger != nil {
			c.Logger.Debug("获取私信用户卡片失败", "error", err)
		}
		return result
	}
	for _, item := range userCardList(data) {
		mid := int64Value(item["mid"], 0)
		if mid > 0 {
			result[mid] = item
		}
	}
	return result
}

func (c *Client) senderUID(ctx context.Context, cred *Credential) (int64, error) {
	if uid := int64Value(cred.DedeUserID, 0); uid > 0 {
		return uid, nil
	}
	me, err := c.GetSelfInfo(ctx, cred)
	if err != nil {
		return 0, err
	}
	uid := int64Value(me["mid"], 0)
	if uid <= 0 {
		return 0, NewError(CodeUpstream, "发送私信", "当前用户信息缺少 mid")
	}
	return uid, nil
}

func userCardList(value any) []map[string]any {
	if items := mapList(value); len(items) > 0 {
		return items
	}
	mapped := mapValue(value)
	for _, key := range []string{"cards", "list", "users"} {
		if items := mapList(mapped[key]); len(items) > 0 {
			return items
		}
	}
	return nil
}

func copyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func randomUUID() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		return infocUUID()
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:])
}
