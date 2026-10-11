package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type VideoDownloadURLs struct {
	AudioURL    string
	VideoURL    string
	CombinedURL string
	Page        int
	PageCount   int
	PartTitle   string
	Pages       []VideoPage
}

type VideoPage struct {
	Page  int
	CID   int64
	Title string
	// DurationSeconds 是该分P的时长, 用于推导弹幕分段数量.
	DurationSeconds int
}

func (c *Client) GetVideoURL(ctx context.Context, bvid string, cred *Credential) (string, error) {
	urls, err := c.GetVideoDownloadURLs(ctx, bvid, cred)
	if err != nil {
		return "", err
	}
	if urls.VideoURL != "" {
		return urls.VideoURL, nil
	}
	if urls.CombinedURL != "" {
		return urls.CombinedURL, nil
	}
	return "", NewError(CodeNotFound, "获取下载地址", "无法获取视频流")
}

func (c *Client) GetVideoDownloadURLs(ctx context.Context, bvid string, cred *Credential) (VideoDownloadURLs, error) {
	return c.GetVideoDownloadURLsForPage(ctx, bvid, 1, cred)
}

func (c *Client) GetVideoPages(ctx context.Context, bvid string, cred *Credential) ([]VideoPage, error) {
	rawPages, err := c.getVideoPages(ctx, bvid, cred)
	if err != nil {
		return nil, err
	}
	pages := normalizeVideoPages(rawPages)
	if len(pages) == 0 {
		return nil, NewError(CodeNotFound, "获取下载地址", "视频没有可用分P")
	}
	return pages, nil
}

func (c *Client) GetVideoDownloadURLsForPage(ctx context.Context, bvid string, page int, cred *Credential) (VideoDownloadURLs, error) {
	if page < 1 {
		return VideoDownloadURLs{}, NewError(CodeInvalidInput, "获取下载地址", "分P序号必须大于 0")
	}
	pages, err := c.GetVideoPages(ctx, bvid, cred)
	if err != nil {
		return VideoDownloadURLs{}, err
	}
	if page > len(pages) {
		return VideoDownloadURLs{}, NewError(CodeNotFound, "获取下载地址", "分P序号超出范围")
	}
	selectedPage := pages[page-1]
	cid := selectedPage.CID
	if cid == 0 {
		return VideoDownloadURLs{}, NewError(CodeUpstream, "获取下载地址", "视频分P信息缺少 cid")
	}
	data, err := c.getPlayURLData(ctx, bvid, cid, cred)
	if err != nil {
		return VideoDownloadURLs{}, err
	}
	urls := videoDownloadURLsFromPlayURL(data)
	if urls.AudioURL == "" && urls.VideoURL == "" && urls.CombinedURL == "" {
		return VideoDownloadURLs{}, NewError(CodeNotFound, "获取下载地址", "无法获取音视频流")
	}
	urls.Page = page
	urls.PageCount = len(pages)
	urls.PartTitle = selectedPage.Title
	urls.Pages = pages
	return urls, nil
}

func normalizeVideoPages(items []map[string]any) []VideoPage {
	pages := make([]VideoPage, 0, len(items))
	for index, item := range items {
		page := intValue(item["page"], index+1)
		if page < 1 {
			page = index + 1
		}
		title := stringValue(item["part"])
		if title == "" {
			title = stringValue(item["title"])
		}
		pages = append(pages, VideoPage{
			Page:            page,
			CID:             int64Value(item["cid"], 0),
			Title:           title,
			DurationSeconds: intValue(item["duration"], 0),
		})
	}
	return pages
}

func (c *Client) getPlayURLData(ctx context.Context, bvid string, cid int64, cred *Credential) (map[string]any, error) {
	requestCredential := c.credentialWithDevice(ctx, cred)
	query := url.Values{
		"bvid":          []string{bvid},
		"cid":           []string{fmt.Sprintf("%d", cid)},
		"qn":            []string{"80"},
		"fnval":         []string{"4048"},
		"fnver":         []string{"0"},
		"fourk":         []string{"1"},
		"gaia_source":  []string{"pre-load"},
		"isGaiaAvoided": []string{"true"},
		"web_location":  []string{"1315873"},
	}
	var data map[string]any
	var requestErr error
	if signed, signErr := c.signWBI(ctx, query, requestCredential); signErr == nil {
		requestErr = c.request(ctx, http.MethodGet, "/x/player/wbi/playurl", signed, nil, requestCredential, &data)
	} else {
		requestErr = signErr
	}
	if requestErr != nil && CodeOf(requestErr) == CodeRateLimited {
		return nil, withAction("获取下载地址", requestErr)
	}
	if requestErr != nil {
		requestErr = c.request(ctx, http.MethodGet, "/x/player/playurl", query, nil, requestCredential, &data)
	}
	if requestErr != nil {
		return nil, withAction("获取下载地址", requestErr)
	}
	return mapValue(data), nil
}

func videoDownloadURLsFromPlayURL(data map[string]any) VideoDownloadURLs {
	data = mapValue(data)
	dash := mapValue(data["dash"])
	urls := VideoDownloadURLs{
		AudioURL: firstStreamURL(dash["audio"]),
		VideoURL: firstStreamURL(dash["video"]),
	}
	urls.CombinedURL = firstURL(data["durl"])
	if urls.CombinedURL == "" {
		urls.CombinedURL = firstURL(data["url"])
	}
	if urls.CombinedURL == "" {
		urls.CombinedURL = firstURL(data["dash"])
	}
	return urls
}

func audioURLFromPlayURL(data map[string]any) (string, error) {
	urls := videoDownloadURLsFromPlayURL(data)
	if urls.AudioURL != "" {
		return urls.AudioURL, nil
	}
	if urls.CombinedURL != "" {
		return urls.CombinedURL, nil
	}
	return "", NewError(CodeNotFound, "获取下载地址", "无法获取音频流")
}

func firstStreamURL(value any) string {
	items := mapList(value)
	if item, ok := value.(map[string]any); ok {
		items = []map[string]any{item}
	}
	if typed, ok := value.([]map[string]any); ok {
		items = typed
	}
	for _, item := range items {
		for _, key := range []string{"baseUrl", "base_url", "url"} {
			if result := stringValue(item[key]); result != "" {
				return result
			}
		}
		for _, key := range []string{"backupUrl", "backup_url"} {
			if result := firstURL(item[key]); result != "" {
				return result
			}
		}
	}
	return ""
}

func firstURL(value any) string {
	if result := stringValue(value); result != "" {
		return result
	}
	if item, ok := value.(map[string]any); ok {
		for _, key := range []string{"baseUrl", "base_url", "url"} {
			if result := stringValue(item[key]); result != "" {
				return result
			}
		}
		for _, key := range []string{"backupUrl", "backup_url"} {
			if result := firstURL(item[key]); result != "" {
				return result
			}
		}
		return ""
	}
	if items, ok := value.([]string); ok {
		for _, item := range items {
			if result := firstURL(item); result != "" {
				return result
			}
		}
		return ""
	}
	if items, ok := value.([]map[string]any); ok {
		for _, item := range items {
			if result := firstURL(item); result != "" {
				return result
			}
		}
		return ""
	}
	for _, item := range listValue(value) {
		if result := firstURL(item); result != "" {
			return result
		}
	}
	return ""
}
