package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DanmakuSegmentLengthMs 是服务端弹幕分段长度, 每个分段覆盖 6 分钟.
const DanmakuSegmentLengthMs = danmakuSegmentLengthMs

// danmakuMaxSegments 限制单个视频请求的分段数, 避免异常时长导致大量请求.
const danmakuMaxSegments = 100

// danmakuSegmentConcurrency 是并发拉取弹幕分段的并发度.
const danmakuSegmentConcurrency = 8

// danmakuMaxContentRunes 是发送弹幕时允许的最大字符数.
const danmakuMaxContentRunes = 100

// DanmakuItem 是一条视频弹幕.
type DanmakuItem struct {
	ID        int64
	IDStr     string
	Progress  int
	Mode      int
	FontSize  int
	Color     int
	MidHash   string
	Content   string
	CTime     int64
	Pool      int
	Weight    int
	LikeCount int64
	Colorful  int
	DmFrom    int
	Attr      int
}

// ModeName 返回弹幕模式的可读名称.
func (item DanmakuItem) ModeName() string {
	switch item.Mode {
	case 1:
		return "滚动"
	case 4:
		return "底部"
	case 5:
		return "顶部"
	case 6:
		return "逆向"
	case 7:
		return "高级"
	case 8:
		return "代码"
	case 9:
		return "BAS"
	default:
		return "未知"
	}
}

// DanmakuResult 是一个视频分P的弹幕集合.
type DanmakuResult struct {
	CID int64
	// TotalSegments 是按视频时长算出的分段总数.
	TotalSegments int
	// Segment 大于 0 时表示本次只拉取了该分段, 其余分段未请求.
	Segment int
	Items   []DanmakuItem
}

// DanmakuPostRequest 描述一条待发送的弹幕.
type DanmakuPostRequest struct {
	BVID     string
	OID      int64
	Content  string
	Progress int
	Mode     int
	Color    int
	FontSize int
	Pool     int
}

// DanmakuPostResult 是发送弹幕的结果.
type DanmakuPostResult struct {
	DMID     string
	Progress int
}

// DanmakuSegmentCount 按视频时长计算弹幕分段总数, 至少为 1.
func DanmakuSegmentCount(durationMs int) int {
	if durationMs <= 0 {
		return 1
	}
	segments := (durationMs + danmakuSegmentLengthMs - 1) / danmakuSegmentLengthMs
	if segments < 1 {
		return 1
	}
	return segments
}

// GetVideoDanmaku 拉取指定 cid 的全部弹幕分段并合并.
func (c *Client) GetVideoDanmaku(ctx context.Context, cid int64, durationMs int, cred *Credential) (DanmakuResult, error) {
	return c.getVideoDanmaku(ctx, cid, durationMs, 0, cred)
}

// GetVideoDanmakuSegment 只拉取指定分段, 用于长视频按时间定位查看.
// segment 为 0 时与 GetVideoDanmaku 等价.
func (c *Client) GetVideoDanmakuSegment(ctx context.Context, cid int64, durationMs int, segment int, cred *Credential) (DanmakuResult, error) {
	if segment < 0 {
		return DanmakuResult{}, NewError(CodeInvalidInput, "获取弹幕", "弹幕分段序号不能为负数")
	}
	return c.getVideoDanmaku(ctx, cid, durationMs, segment, cred)
}

// getVideoDanmaku 是 GetVideoDanmaku 与 GetVideoDanmakuSegment 的共同实现.
// segment 为 0 时拉取全部分段, 大于 0 时只拉取该分段.
func (c *Client) getVideoDanmaku(ctx context.Context, cid int64, durationMs int, segment int, cred *Credential) (DanmakuResult, error) {
	if cid <= 0 {
		return DanmakuResult{}, NewError(CodeInvalidInput, "获取弹幕", "缺少有效的 cid")
	}
	total := DanmakuSegmentCount(durationMs)
	if total > danmakuMaxSegments {
		return DanmakuResult{}, NewError(CodeInvalidInput, "获取弹幕",
			fmt.Sprintf("视频时长对应 %d 个弹幕分段, 超过上限 %d", total, danmakuMaxSegments))
	}
	result := DanmakuResult{CID: cid, TotalSegments: total, Segment: segment}
	if segment > 0 {
		if segment > total {
			return DanmakuResult{}, NewError(CodeNotFound, "获取弹幕",
				fmt.Sprintf("分段序号 %d 超出范围, 该视频共 %d 个分段", segment, total))
		}
		if c.Logger != nil {
			c.Logger.Info("开始获取弹幕分段", "cid", cid, "segment", segment, "total_segments", total)
		}
		items, err := c.getDanmakuSegment(ctx, cid, segment, cred)
		if err != nil {
			return DanmakuResult{}, err
		}
		result.Items = mergeDanmakuItems(items)
		if c.Logger != nil {
			c.Logger.Info("弹幕分段获取完成", "cid", cid, "segment", segment, "count", len(result.Items))
		}
		return result, nil
	}
	if c.Logger != nil {
		c.Logger.Info("开始获取弹幕", "cid", cid, "segments", total, "duration_ms", durationMs)
	}
	items, err := c.getDanmakuSegments(ctx, cid, total, cred)
	if err != nil {
		return DanmakuResult{}, err
	}
	result.Items = mergeDanmakuItems(items)
	if c.Logger != nil {
		c.Logger.Info("弹幕获取完成", "cid", cid, "segments", total, "count", len(result.Items))
	}
	return result, nil
}

// GetDanmakuSegment 拉取单个分段. 分段越界时返回空切片且不报错.
func (c *Client) GetDanmakuSegment(ctx context.Context, cid int64, segmentIndex int, cred *Credential) ([]DanmakuItem, error) {
	if cid <= 0 {
		return nil, NewError(CodeInvalidInput, "获取弹幕", "缺少有效的 cid")
	}
	if segmentIndex < 1 {
		return nil, NewError(CodeInvalidInput, "获取弹幕", "弹幕分段序号必须大于 0")
	}
	return c.getDanmakuSegment(ctx, cid, segmentIndex, cred)
}

func (c *Client) getDanmakuSegment(ctx context.Context, cid int64, segmentIndex int, cred *Credential) ([]DanmakuItem, error) {
	requestCredential := c.credentialWithDevice(ctx, cred)
	payload, err := c.postGRPC(ctx, grpcDanmakuPath, encodeDanmakuSegmentRequest(cid, segmentIndex), requestCredential)
	if err != nil {
		if errors.Is(err, errDanmakuSegmentOutOfRange) {
			return nil, nil
		}
		return nil, withAction("获取弹幕", err)
	}
	if len(payload) == 0 {
		return nil, nil
	}
	items, decodeErr := decodeDanmakuSegment(payload)
	if decodeErr != nil {
		return nil, withAction("获取弹幕", decodeErr)
	}
	return items, nil
}

func (c *Client) getDanmakuSegments(ctx context.Context, cid int64, segments int, cred *Credential) ([]DanmakuItem, error) {
	concurrency := danmakuSegmentConcurrency
	if concurrency > segments {
		concurrency = segments
	}
	if concurrency < 1 {
		concurrency = 1
	}
	type segmentResult struct {
		index int
		items []DanmakuItem
	}
	results := make([]segmentResult, segments)
	indexes := make(chan int)
	var waitGroup sync.WaitGroup
	var failureMu sync.Mutex
	var failure error
	worker := func() {
		defer waitGroup.Done()
		for index := range indexes {
			items, err := c.getDanmakuSegment(ctx, cid, index+1, cred)
			if err != nil {
				failureMu.Lock()
				if failure == nil {
					failure = err
				}
				failureMu.Unlock()
				continue
			}
			results[index] = segmentResult{index: index, items: items}
		}
	}
	waitGroup.Add(concurrency)
	for started := 0; started < concurrency; started++ {
		go worker()
	}
	for index := 0; index < segments; index++ {
		select {
		case <-ctx.Done():
			close(indexes)
			waitGroup.Wait()
			return nil, ctx.Err()
		case indexes <- index:
		}
	}
	close(indexes)
	waitGroup.Wait()
	if failure != nil {
		return nil, failure
	}
	items := make([]DanmakuItem, 0, 256)
	for _, result := range results {
		items = append(items, result.items...)
	}
	return items, nil
}

// mergeDanmakuItems 按弹幕 ID 去重, 并按出现时间稳定排序.
func mergeDanmakuItems(items []DanmakuItem) []DanmakuItem {
	if len(items) == 0 {
		return []DanmakuItem{}
	}
	seen := make(map[int64]struct{}, len(items))
	merged := make([]DanmakuItem, 0, len(items))
	for _, item := range items {
		if item.ID != 0 {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
		}
		merged = append(merged, item)
	}
	sort.SliceStable(merged, func(left, right int) bool {
		if merged[left].Progress != merged[right].Progress {
			return merged[left].Progress < merged[right].Progress
		}
		return merged[left].ID < merged[right].ID
	})
	return merged
}

// FilterDanmakuByProgress 只保留出现时间落在 [fromMs, toMs] 内的弹幕.
// fromMs 或 toMs 为负数时表示该侧不设限制, 必须已有 fromMs <= toMs.
func FilterDanmakuByProgress(items []DanmakuItem, fromMs, toMs int) ([]DanmakuItem, error) {
	if fromMs >= 0 && toMs >= 0 && fromMs > toMs {
		return nil, NewError(CodeInvalidInput, "筛选弹幕", "起始时间不能晚于结束时间")
	}
	filtered := make([]DanmakuItem, 0, len(items))
	for _, item := range items {
		if fromMs >= 0 && item.Progress < fromMs {
			continue
		}
		if toMs >= 0 && item.Progress > toMs {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

// DanmakuSegmentRange 返回指定分段覆盖的时间区间, 单位毫秒.
// 第二个返回值表示该区间是否被视频时长截断.
func DanmakuSegmentRange(segment int, durationMs int) (int, int) {
	if segment < 1 {
		segment = 1
	}
	start := (segment - 1) * danmakuSegmentLengthMs
	end := segment*danmakuSegmentLengthMs - 1
	if durationMs > 0 && end > durationMs {
		end = durationMs
	}
	if end < start {
		end = start
	}
	return start, end
}

// PostVideoDanmaku 发送一条视频弹幕.
func (c *Client) PostVideoDanmaku(ctx context.Context, request DanmakuPostRequest, cred *Credential) (DanmakuPostResult, error) {
	if err := requireCredential("发送弹幕", cred, true); err != nil {
		return DanmakuPostResult{}, err
	}
	content := strings.TrimSpace(request.Content)
	if content == "" {
		return DanmakuPostResult{}, NewError(CodeInvalidInput, "发送弹幕", "弹幕内容不能为空")
	}
	if runes := []rune(content); len(runes) > danmakuMaxContentRunes {
		return DanmakuPostResult{}, NewError(CodeInvalidInput, "发送弹幕",
			fmt.Sprintf("弹幕内容最多 %d 个字符, 当前 %d 个", danmakuMaxContentRunes, len(runes)))
	}
	if request.OID <= 0 {
		return DanmakuPostResult{}, NewError(CodeInvalidInput, "发送弹幕", "缺少有效的 cid")
	}
	if strings.TrimSpace(request.BVID) == "" {
		return DanmakuPostResult{}, NewError(CodeInvalidInput, "发送弹幕", "缺少 bvid")
	}
	if request.Progress < 0 {
		return DanmakuPostResult{}, NewError(CodeInvalidInput, "发送弹幕", "弹幕出现时间不能为负")
	}
	mode := request.Mode
	if mode == 0 {
		mode = 1
	}
	fontSize := request.FontSize
	if fontSize <= 0 {
		fontSize = 25
	}
	requestCredential := c.credentialWithDevice(ctx, cred)
	form := url.Values{
		"type":     []string{"1"},
		"oid":      []string{strconv.FormatInt(request.OID, 10)},
		"bvid":     []string{request.BVID},
		"msg":      []string{content},
		"mode":     []string{strconv.Itoa(mode)},
		"progress": []string{strconv.Itoa(request.Progress)},
		"color":    []string{strconv.Itoa(request.Color)},
		"fontsize": []string{strconv.Itoa(fontSize)},
		"pool":     []string{strconv.Itoa(request.Pool)},
		"rnd":      []string{strconv.FormatInt(time.Now().UnixMicro(), 10)},
		"csrf":     []string{cred.BiliJct},
	}
	var data map[string]any
	if err := c.request(ctx, http.MethodPost, "/x/v2/dm/post", nil, form, requestCredential, &data); err != nil {
		return DanmakuPostResult{}, withAction("发送弹幕", err)
	}
	result := DanmakuPostResult{Progress: request.Progress}
	result.DMID = stringValue(mapValue(data)["dmid"])
	if result.DMID == "" {
		result.DMID = stringValue(mapValue(data)["dmid_str"])
	}
	return result, nil
}
