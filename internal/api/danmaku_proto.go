package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"
)

// danmakuSegmentLengthMs 是服务端弹幕分段长度, 每个分段覆盖 6 分钟.
const danmakuSegmentLengthMs = 360000

// grpcFrameHeaderSize 是 gRPC 长度前缀帧的固定头部长度: 1 字节压缩标志加 4 字节大端长度.
const grpcFrameHeaderSize = 5

// encodeDanmakuSegmentRequest 构造 DmSegMobileReq, 只填充服务端必需字段.
// tag 2 是 oid (cid), tag 3 是 type (1 表示视频弹幕), tag 4 是 segmentIndex.
func encodeDanmakuSegmentRequest(cid int64, segmentIndex int) []byte {
	payload := make([]byte, 0, 16)
	payload = protowire.AppendTag(payload, 2, protowire.VarintType)
	payload = protowire.AppendVarint(payload, uint64(cid))
	payload = protowire.AppendTag(payload, 3, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 1)
	payload = protowire.AppendTag(payload, 4, protowire.VarintType)
	payload = protowire.AppendVarint(payload, uint64(segmentIndex))
	return payload
}

// wrapGRPCFrame 给 protobuf 负载加上 gRPC 长度前缀.
func wrapGRPCFrame(payload []byte) []byte {
	frame := make([]byte, grpcFrameHeaderSize+len(payload))
	frame[0] = 0
	length := uint32(len(payload))
	frame[1] = byte(length >> 24)
	frame[2] = byte(length >> 16)
	frame[3] = byte(length >> 8)
	frame[4] = byte(length)
	copy(frame[grpcFrameHeaderSize:], payload)
	return frame
}

// decodeGRPCFrames 解析 gRPC 响应体, 返回第一个消息帧的 protobuf 负载.
// 服务端可能把一个响应拆成多帧, 这里只取第一帧, 与 piliplus 的处理方式一致.
func decodeGRPCFrames(body []byte) ([]byte, error) {
	if len(body) < grpcFrameHeaderSize {
		return nil, NewError(CodeUpstream, "解析弹幕响应", fmt.Sprintf("gRPC 响应长度不足: %d", len(body)))
	}
	compressed := body[0]
	length := int(uint32(body[1])<<24 | uint32(body[2])<<16 | uint32(body[3])<<8 | uint32(body[4]))
	if length < 0 || grpcFrameHeaderSize+length > len(body) {
		return nil, NewError(CodeUpstream, "解析弹幕响应", fmt.Sprintf("gRPC 帧长度非法: %d", length))
	}
	payload := body[grpcFrameHeaderSize : grpcFrameHeaderSize+length]
	switch compressed {
	case 0:
		return payload, nil
	case 1:
		reader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, &Error{Code: CodeUpstream, Action: "解析弹幕响应", Message: "gRPC 帧解压失败", Err: err}
		}
		defer reader.Close()
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return nil, &Error{Code: CodeUpstream, Action: "解析弹幕响应", Message: "gRPC 帧解压失败", Err: err}
		}
		return decoded, nil
	default:
		return nil, NewError(CodeUpstream, "解析弹幕响应", fmt.Sprintf("未知的 gRPC 压缩标志: %d", compressed))
	}
}

// decodeDanmakuSegment 解析 DmSegMobileReply, 只取 elems 字段.
func decodeDanmakuSegment(payload []byte) ([]DanmakuItem, error) {
	items := make([]DanmakuItem, 0, 64)
	for len(payload) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(payload)
		if tagLength < 0 {
			return nil, NewError(CodeUpstream, "解析弹幕响应", "弹幕响应包含非法字段标签")
		}
		payload = payload[tagLength:]
		if wireType != protowire.BytesType {
			skip := protowire.ConsumeFieldValue(number, wireType, payload)
			if skip < 0 {
				return nil, NewError(CodeUpstream, "解析弹幕响应", "弹幕响应包含非法字段值")
			}
			payload = payload[skip:]
			continue
		}
		element, valueLength := protowire.ConsumeBytes(payload)
		if valueLength < 0 {
			return nil, NewError(CodeUpstream, "解析弹幕响应", "弹幕响应包含非法长度前缀")
		}
		payload = payload[valueLength:]
		if number != 1 {
			continue
		}
		item, err := decodeDanmakuElement(element)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// decodeDanmakuElement 解析单条 DanmakuElem, 未识别字段按 wire type 跳过.
func decodeDanmakuElement(payload []byte) (DanmakuItem, error) {
	var item DanmakuItem
	for len(payload) > 0 {
		number, wireType, tagLength := protowire.ConsumeTag(payload)
		if tagLength < 0 {
			return DanmakuItem{}, NewError(CodeUpstream, "解析弹幕响应", "弹幕条目包含非法字段标签")
		}
		payload = payload[tagLength:]
		switch wireType {
		case protowire.VarintType:
			value, valueLength := protowire.ConsumeVarint(payload)
			if valueLength < 0 {
				return DanmakuItem{}, NewError(CodeUpstream, "解析弹幕响应", "弹幕条目包含非法整数值")
			}
			payload = payload[valueLength:]
			applyDanmakuVarint(&item, number, value)
		case protowire.BytesType:
			value, valueLength := protowire.ConsumeBytes(payload)
			if valueLength < 0 {
				return DanmakuItem{}, NewError(CodeUpstream, "解析弹幕响应", "弹幕条目包含非法字符串")
			}
			payload = payload[valueLength:]
			switch number {
			case 6:
				item.MidHash = string(value)
			case 7:
				item.Content = string(value)
			case 12:
				item.IDStr = string(value)
			}
		default:
			skip := protowire.ConsumeFieldValue(number, wireType, payload)
			if skip < 0 {
				return DanmakuItem{}, NewError(CodeUpstream, "解析弹幕响应", "弹幕条目包含非法字段值")
			}
			payload = payload[skip:]
		}
	}
	if item.IDStr == "" && item.ID != 0 {
		item.IDStr = strconv.FormatInt(item.ID, 10)
	}
	return item, nil
}

func applyDanmakuVarint(item *DanmakuItem, number protowire.Number, value uint64) {
	switch number {
	case 1:
		item.ID = int64(value)
	case 2:
		item.Progress = int(value)
	case 3:
		item.Mode = int(value)
	case 4:
		item.FontSize = int(value)
	case 5:
		item.Color = int(value)
	case 8:
		item.CTime = int64(value)
	case 9:
		item.Weight = int(value)
	case 11:
		item.Pool = int(value)
	case 13:
		item.Attr = int(value)
	case 15:
		item.LikeCount = int64(value)
	case 24:
		item.Colorful = int(value)
	case 27:
		item.DmFrom = int(value)
	}
}

// grpcDanmakuPath 是 App 端的弹幕分段 gRPC 方法路径.
const grpcDanmakuPath = "/bilibili.community.service.dm.v1.DM/DmSegMobile"

// grpcOutOfRangeStatus 是服务端对越界分段返回的业务状态码.
const grpcOutOfRangeStatus = "-304"

// errDanmakuSegmentOutOfRange 表示请求的分段超出视频时长, 调用方按空分段处理.
var errDanmakuSegmentOutOfRange = &Error{Code: CodeNotFound, Action: "获取弹幕", Message: "弹幕分段超出范围"}

// postGRPC 向 App 端的 gRPC 方法发送长度前缀帧, 返回第一个消息帧的负载.
func (c *Client) postGRPC(ctx context.Context, path string, payload []byte, cred *Credential) ([]byte, error) {
	requestURL := c.AppURL(path)
	body := wrapGRPCFrame(payload)
	attempts := 3
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		decoded, err := c.postGRPCOnce(ctx, requestURL, body, cred)
		if err == nil || attempt == attempts || !isRetryableReadError(err) {
			return decoded, err
		}
		lastErr = err
		if c.Logger != nil {
			c.Logger.Warn("弹幕请求失败, 准备重试", "url", requestURL, "attempt", attempt, "error", err)
		}
	}
	return nil, lastErr
}

func (c *Client) postGRPCOnce(ctx context.Context, requestURL string, body []byte, cred *Credential) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Code: CodeInvalidInput, Action: "获取弹幕", Message: err.Error(), Err: err}
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("User-Agent", appUserAgent)
	req.Header.Set("Accept", "application/grpc")
	req.Header.Set("grpc-accept-encoding", "identity")
	req.Header.Set("Referer", "https://www.bilibili.com/")
	if cred != nil && cred.Buvid3 != "" {
		req.Header.Set("buvid", cred.Buvid3)
	}
	if cred != nil && cred.DedeUserID != "" {
		req.Header.Set("x-bili-mid", cred.DedeUserID)
	}
	if cred != nil && cred.AccessKey != "" {
		req.Header.Set("authorization", "identify_v1 "+cred.AccessKey)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &Error{Code: CodeNetwork, Action: "获取弹幕", Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, &Error{Code: CodeNetwork, HTTPStatus: resp.StatusCode, Action: "获取弹幕", Message: readErr.Error(), Err: readErr}
	}
	status := grpcStatus(resp)
	if status != "" && status != "0" {
		if grpcOutOfRange(status, grpcBusinessStatus(resp)) {
			return nil, errDanmakuSegmentOutOfRange
		}
		return nil, grpcStatusError(resp, status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &Error{Code: CodeNetwork, HTTPStatus: resp.StatusCode, Action: "获取弹幕", Message: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	if len(responseBody) == 0 {
		return nil, nil
	}
	decoded, decodeErr := decodeGRPCFrames(responseBody)
	if decodeErr != nil {
		return nil, withAction("获取弹幕", decodeErr)
	}
	return decoded, nil
}

func grpcStatus(resp *http.Response) string {
	if value := resp.Header.Get("Grpc-Status"); value != "" {
		return value
	}
	if resp.Trailer != nil {
		if value := resp.Trailer.Get("Grpc-Status"); value != "" {
			return value
		}
	}
	return ""
}

// grpcStatusError 把 gRPC 失败状态映射成本项目统一的错误类型.
// 越界分段由调用方单独识别, 这里只处理其余失败.
func grpcStatusError(resp *http.Response, status string) error {
	detail := ""
	if resp != nil {
		detail = resp.Header.Get("Bili-Status-Code")
		if detail == "" && resp.Trailer != nil {
			detail = resp.Trailer.Get("Bili-Status-Code")
		}
	}
	message := fmt.Sprintf("gRPC 状态 %s", status)
	if detail != "" {
		message = fmt.Sprintf("gRPC 状态 %s (业务码 %s)", status, detail)
	}
	return &Error{Code: CodeUpstream, Action: "获取弹幕", Message: message}
}

// grpcOutOfRange 判断响应是否表示分段越界, 越界视为空分段.
func grpcOutOfRange(status string, detail string) bool {
	return status == "2" && detail == grpcOutOfRangeStatus
}

func grpcBusinessStatus(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	if value := resp.Header.Get("Bili-Status-Code"); value != "" {
		return value
	}
	if resp.Trailer != nil {
		return resp.Trailer.Get("Bili-Status-Code")
	}
	return ""
}
