package model

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func ToInt(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func ToInt64(value any, fallback int64) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func ToFloat(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func String(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func Map(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func List(value any) []any {
	if result, ok := value.([]any); ok {
		return result
	}
	return []any{}
}

func Maps(value any) []map[string]any {
	if direct, ok := value.([]map[string]any); ok {
		return direct
	}
	items := List(value)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			result = append(result, mapped)
		}
	}
	return result
}

func StripHTML(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(htmlTagPattern.ReplaceAllString(text, "")))
}

func FormatDuration(value any) string {
	total := ToInt(value, 0)
	if total < 0 {
		total = 0
	}
	if total >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", total/3600, (total%3600)/60, total%60)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func FormatCount(value any) string {
	count := ToInt(value, 0)
	if count >= 10000 {
		return fmt.Sprintf("%.1f万", float64(count)/10000)
	}
	return strconv.Itoa(count)
}

func PublishedAt(item map[string]any) string {
	for _, key := range []string{"published_at", "pubdate", "pub_time", "publish_time", "pubtime", "created", "ctime", "senddate", "live_time", "publish_time_text"} {
		value := item[key]
		if value == nil {
			continue
		}
		if timestamp := ToInt64(value, 0); timestamp > 0 {
			return timestampISO(timestamp)
		}
		if text := strings.TrimSpace(String(value)); text != "" && text != "0" && text != "-1" {
			return text
		}
	}
	return ""
}

func NormalizeUser(info map[string]any) map[string]any {
	levelInfo := Map(info["level_info"])
	wallet := Map(info["wallet"])
	level := ToInt(levelInfo["current_level"], 0)
	if level == 0 {
		level = ToInt(info["level"], 0)
	}
	coins := ToInt(info["money"], 0)
	if coins == 0 {
		coins = ToInt(info["coins"], 0)
	}
	bcoins := ToInt(wallet["bcoin_balance"], ToInt(info["bcoin_balance"], 0))
	return map[string]any{
		"id":       String(info["mid"]),
		"name":     firstString(info["name"], info["uname"]),
		"username": firstString(info["name"], info["uname"]),
		"level":    level,
		"coins":    coins,
		"bcoins":   bcoins,
		"sign":     String(info["sign"]),
		"vip":      Map(info["vip"]),
	}
}

func NormalizeRelation(info map[string]any) map[string]any {
	return map[string]any{
		"following": ToInt(info["following"], 0),
		"follower":  ToInt(info["follower"], 0),
	}
}

func NormalizeVideoSummary(value map[string]any) map[string]any {
	owner := Map(value["owner"])
	stat := Map(value["stat"])
	duration := ToInt(value["duration"], ToInt(value["length"], 0))
	if duration == 0 {
		if raw := String(value["length"]); strings.Contains(raw, ":") {
			duration = parseClock(raw)
		}
	}
	bvid := String(value["bvid"])
	identifier := bvid
	if identifier == "" {
		identifier = String(value["aid"])
	}
	url := ""
	if bvid != "" {
		url = "https://www.bilibili.com/video/" + bvid
	}
	return map[string]any{
		"id":                identifier,
		"bvid":              bvid,
		"aid":               ToInt(value["aid"], 0),
		"title":             StripHTML(value["title"]),
		"description":       firstString(value["desc"], value["description"]),
		"published_at":      PublishedAt(value),
		"duration_seconds":  duration,
		"duration":          FormatDuration(duration),
		"url":               url,
		"owner":             map[string]any{"id": firstString(owner["mid"], owner["id"], value["mid"], value["author_mid"]), "name": firstString(owner["name"], owner["uname"], value["author"])},
		"stats": map[string]any{
			"view":     ToInt(firstValue(stat["view"], value["play"]), 0),
			"danmaku":  ToInt(stat["danmaku"], 0),
			"like":     ToInt(stat["like"], 0),
			"coin":     ToInt(stat["coin"], 0),
			"favorite": ToInt(stat["favorite"], 0),
			"share":    ToInt(stat["share"], 0),
		},
	}
}

func NormalizeComment(item map[string]any) map[string]any {
	member := Map(item["member"])
	content := Map(item["content"])
	identifier := firstString(item["rpid_str"], item["rpid"])
	return map[string]any{
		"id": identifier,
		"author": map[string]any{
			"id":   String(member["mid"]),
			"name": String(member["uname"]),
		},
		"message":     String(content["message"]),
		"like":        ToInt(item["like"], 0),
		"reply_count": ToInt(item["rcount"], 0),
	}
}

func NormalizeFavoriteFolder(item map[string]any) map[string]any {
	return map[string]any{
		"id":          ToInt(item["id"], 0),
		"title":       String(item["title"]),
		"media_count": ToInt(item["media_count"], 0),
	}
}

func NormalizeFavoriteMedia(item map[string]any) map[string]any {
	upper := Map(item["upper"])
	duration := ToInt(item["duration"], 0)
	return map[string]any{
		"id":               firstString(item["bvid"], item["id"]),
		"bvid":             String(item["bvid"]),
		"title":            String(item["title"]),
		"published_at":     PublishedAt(item),
		"duration_seconds": duration,
		"duration":         FormatDuration(duration),
		"upper":            map[string]any{"name": String(upper["name"])},
	}
}

func NormalizeFollowingUser(item map[string]any) map[string]any {
	return map[string]any{"id": String(item["mid"]), "name": String(item["uname"]), "sign": String(item["sign"])}
}

func NormalizeHistoryItem(item map[string]any) map[string]any {
	history := Map(item["history"])
	owner := Map(item["owner"])
	viewedAt := ToInt64(firstValue(history["view_at"], item["view_at"]), 0)
	identifier := firstString(history["bvid"], item["bvid"], history["oid"])
	author := firstString(owner["name"], item["author_name"], item["author"])
	return map[string]any{
		"id":           identifier,
		"bvid":         firstString(history["bvid"], item["bvid"]),
		"title":        firstString(item["title"], item["name"]),
		"author":       author,
		"published_at": PublishedAt(item),
		"viewed_at":    timestampISO(viewedAt),
	}
}

func NormalizeWatchLaterItem(item map[string]any) map[string]any {
	owner := Map(item["owner"])
	duration := ToInt(item["duration"], 0)
	return map[string]any{
		"id":               String(item["bvid"]),
		"bvid":             String(item["bvid"]),
		"title":            String(item["title"]),
		"author":           String(owner["name"]),
		"published_at":     PublishedAt(item),
		"duration_seconds": duration,
		"duration":         FormatDuration(duration),
	}
}

func NormalizeDynamicItem(item map[string]any) map[string]any {
	modules := Map(item["modules"])
	author := Map(modules["module_author"])
	dynamic := Map(modules["module_dynamic"])
	stat := Map(modules["module_stat"])
	desc := Map(dynamic["desc"])
	major := Map(dynamic["major"])
	archive := Map(major["archive"])
	article := Map(major["article"])
	opus := Map(major["opus"])
	summary := Map(opus["summary"])
	card := DecodeJSON(item["card"])
	descInfo := Map(item["desc"])
	identifier := firstString(descInfo["dynamic_id_str"], descInfo["dynamic_id"], item["id_str"], item["id"])
	text := String(desc["text"])
	if text == "" {
		text = String(summary["text"])
	}
	if text == "" {
		for _, key := range []string{"dynamic", "description", "summary", "title"} {
			if value := String(card[key]); value != "" {
				text = value
				break
			}
		}
		itemInfo := Map(card["item"])
		if text == "" {
			text = firstString(itemInfo["content"], itemInfo["description"], itemInfo["title"])
		}
	}
	commentInfo := Map(stat["comment"])
	likeInfo := Map(stat["like"])
	ts := ToInt64(descInfo["timestamp"], ToInt64(author["pub_ts"], 0))
	return map[string]any{
		"id": identifier,
		"author": map[string]any{"name": String(author["name"])},
		"published_at":    timestampISO(ts),
		"published_label": String(author["pub_time"]),
		"title":           firstString(archive["title"], article["title"], opus["title"]),
		"text":            text,
		"stats": map[string]any{
			"comment": ToInt(commentInfo["count"], 0),
			"like":    ToInt(likeInfo["count"], 0),
		},
	}
}

func NormalizeVideoCommandPayload(info map[string]any, aiSummary string, comments, related []map[string]any, warnings []map[string]string) map[string]any {
	normalizedComments := make([]map[string]any, 0, len(comments))
	for _, item := range comments {
		normalizedComments = append(normalizedComments, NormalizeComment(item))
	}
	normalizedRelated := make([]map[string]any, 0, len(related))
	for _, item := range related {
		normalizedRelated = append(normalizedRelated, NormalizeVideoSummary(item))
	}
	return map[string]any{
		"video":      NormalizeVideoSummary(info),
		"ai_summary": aiSummary,
		"comments":   normalizedComments,
		"related":    normalizedRelated,
		"warnings":   warnings,
	}
}

func ActionResult(action string, fields map[string]any) map[string]any {
	result := map[string]any{"success": true, "action": action}
	for key, value := range fields {
		result[key] = value
	}
	return result
}

func DecodeJSON(value any) map[string]any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return map[string]any{}
	}
	return result
}

func TimestampISO(value int64) string { return timestampISO(value) }

func timestampISO(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).Local().Format(time.RFC3339)
}

func firstValue(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if text, ok := value.(string); ok && text == "" {
			continue
		}
		return value
	}
	return nil
}

func firstString(values ...any) string { return String(firstValue(values...)) }

func NormalizeSession(item map[string]any) map[string]any {
	last := Map(item["last_msg"])
	msgType := ToInt(last["msg_type"], 0)
	revoked := ToInt(last["msg_status"], 0) == 1 || msgType == 5
	content := decodeMessageContent(last["content"])
	kind, text := MessageSummary(msgType, content, revoked)
	timestamp := normalizeUnix(ToInt64(firstValue(last["timestamp"], item["session_ts"]), 0))
	result := map[string]any{
		"talker_id": ToInt64(item["talker_id"], 0),
		"name":      firstString(item["name"], item["uname"]),
		"unread":    ToInt(item["unread_count"], 0),
		"is_follow": ToInt(item["is_follow"], 0) != 0,
		"timestamp": timestampISO(timestamp),
		"last_text": text,
		"last_type": kind,
		"content":   content,
	}
	if detail := messageDetail(msgType, content); len(detail) > 0 {
		result["detail"] = detail
	}
	return result
}

func NormalizeMessage(item map[string]any) map[string]any {
	msgType := ToInt(item["msg_type"], 0)
	revoked := ToInt(item["msg_status"], 0) == 1 || msgType == 5
	content := decodeMessageContent(item["content"])
	kind, text := MessageSummary(msgType, content, revoked)
	result := map[string]any{
		"seqno":       ToInt64(item["msg_seqno"], 0),
		"key":         String(item["msg_key"]),
		"sender_id":   ToInt64(item["sender_uid"], 0),
		"receiver_id": ToInt64(item["receiver_id"], 0),
		"msg_type":    msgType,
		"type":        kind,
		"text":        text,
		"timestamp":   timestampISO(normalizeUnix(ToInt64(item["timestamp"], 0))),
		"revoked":     revoked,
		"content":     content,
	}
	if detail := messageDetail(msgType, content); len(detail) > 0 {
		result["detail"] = detail
	}
	return result
}

func NormalizeUnread(item map[string]any) map[string]any {
	follow := ToInt(item["follow_unread"], 0)
	unfollow := ToInt(item["unfollow_unread"], 0)
	return map[string]any{
		"follow_unread":   follow,
		"unfollow_unread": unfollow,
		"total":           follow + unfollow,
	}
}

func MessageSummary(msgType int, content any, revoked bool) (string, string) {
	if revoked || msgType == 5 {
		return "revoke", "[已撤回]"
	}
	decoded := Map(decodeMessageContent(content))
	if len(decoded) == 0 {
		decoded = DecodeJSON(content)
	}
	switch msgType {
	case 1:
		return "text", strings.TrimSpace(firstString(decoded["content"], content))
	case 2, 6:
		imageURL := firstString(decoded["url"], decoded["original"], decoded["imageUrl"])
		if imageURL == "" {
			return "image", "[图片]"
		}
		return "image", "[图片] " + imageURL
	case 7, 14:
		title := firstString(decoded["title"], decoded["source"])
		if title == "" {
			return "other", "[分享]"
		}
		return "other", "[分享] " + title
	case 10:
		text := strings.TrimSpace(firstString(decoded["text"], decoded["title"], decoded["content"]))
		if text == "" {
			return "tip", "[通知]"
		}
		return "tip", text
	case 11:
		title := firstString(decoded["title"], decoded["bvid"])
		if title == "" {
			return "video", "[视频]"
		}
		return "video", "[视频] " + title
	case 12:
		title := firstString(decoded["title"], String(decoded["rid"]))
		if title == "" {
			return "article", "[专栏]"
		}
		return "article", "[专栏] " + title
	case 13:
		cover := firstString(decoded["pic_url"], decoded["url"])
		if cover == "" {
			return "image", "[图片卡片]"
		}
		return "image", "[图片卡片] " + cover
	case 18:
		parts := make([]string, 0)
		for _, item := range List(decoded["content"]) {
			if mapped, ok := item.(map[string]any); ok {
				if text := strings.TrimSpace(String(mapped["text"])); text != "" {
					parts = append(parts, text)
				}
				continue
			}
			if text := strings.TrimSpace(String(item)); text != "" {
				parts = append(parts, text)
			}
		}
		if text := strings.Join(parts, ""); text != "" {
			return "tip", text
		}
		if text := strings.TrimSpace(String(decoded["content"])); text != "" {
			return "tip", text
		}
		return "tip", "[提示]"
	default:
		raw := strings.TrimSpace(String(content))
		if runes := []rune(raw); len(runes) > 80 {
			raw = string(runes[:80]) + "..."
		}
		if raw == "" {
			return "other", fmt.Sprintf("[类型%d]", msgType)
		}
		return "other", fmt.Sprintf("[类型%d] %s", msgType, raw)
	}
}

func decodeMessageContent(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		expandNestedJSON(typed)
		return typed
	case []any:
		return typed
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return map[string]any{}
		}
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return text
		}
		if mapped, ok := decoded.(map[string]any); ok {
			expandNestedJSON(mapped)
			return mapped
		}
		return decoded
	default:
		return map[string]any{}
	}
}

func expandNestedJSON(value map[string]any) {
	raw, ok := value["content"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return
	}
	value["content"] = decoded
}

func messageDetail(msgType int, content any) map[string]any {
	decoded := Map(content)
	detail := map[string]any{}
	switch msgType {
	case 2, 6:
		setIfPresent(detail, "url", firstValue(decoded["url"], decoded["original"], decoded["imageUrl"]))
		setIfPresent(detail, "width", decoded["width"])
		setIfPresent(detail, "height", decoded["height"])
		setIfPresent(detail, "size", decoded["size"])
	case 7:
		setIfPresent(detail, "title", decoded["title"])
		setIfPresent(detail, "author", decoded["author"])
		setIfPresent(detail, "cover", decoded["thumb"])
		setIfPresent(detail, "headline", decoded["headline"])
		setIfPresent(detail, "bvid", decoded["bvid"])
		setIfPresent(detail, "id", decoded["id"])
		setIfPresent(detail, "source", decoded["source"])
		setIfPresent(detail, "url", shareURL(decoded))
	case 10:
		setIfPresent(detail, "title", decoded["title"])
		setIfPresent(detail, "text", decoded["text"])
		setIfPresent(detail, "url", firstValue(decoded["jump_uri"], decoded["jump_uri_2"], decoded["jump_uri_3"]))
		if modules := List(decoded["modules"]); len(modules) > 0 {
			detail["modules"] = modules
		}
	case 11:
		setIfPresent(detail, "title", decoded["title"])
		setIfPresent(detail, "bvid", decoded["bvid"])
		setIfPresent(detail, "cover", decoded["cover"])
		setIfPresent(detail, "duration", decoded["times"])
		if bvid := strings.TrimSpace(String(decoded["bvid"])); bvid != "" {
			detail["url"] = "https://www.bilibili.com/video/" + bvid
		}
		attach := Map(decoded["attach_msg"])
		setIfPresent(detail, "attach", firstValue(attach["content"], decoded["attach_msg"]))
	case 12:
		setIfPresent(detail, "title", decoded["title"])
		setIfPresent(detail, "summary", decoded["summary"])
		setIfPresent(detail, "rid", decoded["rid"])
		if images := List(decoded["image_urls"]); len(images) > 0 {
			detail["covers"] = images
			setIfPresent(detail, "cover", images[0])
		}
		if rid := strings.TrimSpace(String(decoded["rid"])); rid != "" && rid != "0" {
			detail["url"] = "https://www.bilibili.com/read/cv" + rid
		}
	case 13:
		setIfPresent(detail, "url", decoded["jump_url"])
		setIfPresent(detail, "cover", decoded["pic_url"])
	case 14:
		setIfPresent(detail, "title", decoded["title"])
		setIfPresent(detail, "author", decoded["author"])
		setIfPresent(detail, "cover", decoded["cover"])
		setIfPresent(detail, "source", decoded["source"])
		setIfPresent(detail, "id", decoded["sourceID"])
		if strings.TrimSpace(String(decoded["source"])) == "直播" {
			if room := strings.TrimSpace(String(decoded["sourceID"])); room != "" {
				detail["url"] = "https://live.bilibili.com/" + room
			}
		}
	case 16:
		setIfPresent(detail, "title", decoded["main_title"])
		if cards := List(decoded["sub_cards"]); len(cards) > 0 {
			detail["cards"] = cards
		}
	}
	return detail
}

func shareURL(decoded map[string]any) string {
	source := ToInt(decoded["source"], 0)
	id := strings.TrimSpace(String(decoded["id"]))
	bvid := strings.TrimSpace(String(decoded["bvid"]))
	switch source {
	case 5:
		if bvid != "" {
			return "https://www.bilibili.com/video/" + bvid
		}
		if id != "" && id != "0" {
			return "https://www.bilibili.com/video/av" + id
		}
	case 6:
		if id != "" && id != "0" {
			return "https://www.bilibili.com/read/cv" + id
		}
	}
	return ""
}

func setIfPresent(dst map[string]any, key string, value any) {
	if value == nil {
		return
	}
	switch typed := value.(type) {
	case string:
		if text := strings.TrimSpace(typed); text != "" {
			dst[key] = text
		}
	case []any:
		if len(typed) > 0 {
			dst[key] = typed
		}
	case map[string]any:
		if len(typed) > 0 {
			dst[key] = typed
		}
	default:
		if number := ToInt64(value, 0); number != 0 {
			dst[key] = number
			return
		}
		if text := strings.TrimSpace(String(value)); text != "" && text != "0" {
			dst[key] = text
		}
	}
}

func normalizeUnix(value int64) int64 {
	if value <= 0 {
		return 0
	}
	for value > 9999999999 {
		value /= 1000
	}
	return value
}

func parseClock(value string) int {
	parts := strings.Split(value, ":")
	if len(parts) == 0 {
		return 0
	}
	result := 0
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return 0
		}
		result = result*60 + number
	}
	return result
}
