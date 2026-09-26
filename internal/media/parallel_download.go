package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultDownloadThreads = 8

const rangeDownloadRetries = 3

var errRangeUnsupported = errors.New("服务器不支持 HTTP Range")

type downloadRange struct {
	start int64
	end   int64
}

type rangeProbe struct {
	total       int64
	supported   bool
	response    *http.Response
}

const progressEmitInterval = 100 * time.Millisecond

type progressReporter struct {
	callback ProgressFunc
	total    int64
	written  int64
	mu       sync.Mutex
	lastEmit time.Time
}

func newProgressReporter(callback ProgressFunc, total int64) *progressReporter {
	reporter := &progressReporter{callback: callback, total: total}
	if callback != nil {
		callback(DownloadProgress{Total: total})
	}
	return reporter
}

func (r *progressReporter) add(delta int64) int64 {
	if r == nil || delta == 0 {
		if r == nil {
			return 0
		}
		return atomic.LoadInt64(&r.written)
	}
	updated := atomic.AddInt64(&r.written, delta)
	r.emit(delta < 0)
	return updated
}

func (r *progressReporter) emit(force bool) {
	if r == nil || r.callback == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if !force && !r.lastEmit.IsZero() && now.Sub(r.lastEmit) < progressEmitInterval {
		return
	}
	r.lastEmit = now
	r.callback(DownloadProgress{Written: atomic.LoadInt64(&r.written), Total: r.total})
}

func (r *progressReporter) finish() {
	if r == nil || r.callback == nil {
		return
	}
	total := atomic.LoadInt64(&r.written)
	if r.total >= 0 {
		total = r.total
	}
	r.mu.Lock()
	r.callback(DownloadProgress{Written: total, Total: r.total})
	r.mu.Unlock()
}

func downloadOnceParallel(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, label string, progress ProgressFunc, threads int) (int64, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if threads < 1 {
		threads = 1
	}
	probe, err := probeDownloadRange(ctx, mediaURL)
	if err != nil {
		return 0, err
	}
	if !probe.supported {
		if probe.response != nil {
			return writeDownloadResponse(ctx, probe.response, outputPath, logger, label, progress)
		}
		return downloadOnce(ctx, mediaURL, outputPath, logger, label, progress)
	}
	if probe.total <= 1 {
		return downloadOnce(ctx, mediaURL, outputPath, logger, label, progress)
	}

	ranges := splitDownloadRanges(probe.total, threads)
	if len(ranges) <= 1 {
		return downloadOnce(ctx, mediaURL, outputPath, logger, label, progress)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return 0, err
	}
	temporaryPath := outputPath + ".part"
	_ = os.Remove(temporaryPath)
	file, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := file.Truncate(probe.total); err != nil {
		return 0, err
	}

	reporter := newProgressReporter(progress, probe.total)
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan downloadRange)
	workerCount := len(ranges)
	if threads < workerCount {
		workerCount = threads
	}
	var workers sync.WaitGroup
	var firstErr error
	var errorMu sync.Mutex
	for index := 0; index < workerCount; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				if workContext.Err() != nil {
					return
				}
				if err := downloadRangeWithRetry(workContext, mediaURL, file, item, reporter, logger, label); err != nil {
					errorMu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					errorMu.Unlock()
					return
				}
				logger.Info(label+"分段下载进度", "bytes", atomic.LoadInt64(&reporter.written), "total", probe.total)
			}
		}()
	}
	for _, item := range ranges {
		select {
		case jobs <- item:
		case <-workContext.Done():
			break
		}
		if workContext.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if firstErr != nil {
		return 0, firstErr
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return 0, err
	}
	reporter.finish()
	logger.Info(label+"分段下载完成", "threads", workerCount, "bytes", probe.total)
	return probe.total, nil
}

func probeDownloadRange(ctx context.Context, mediaURL string) (rangeProbe, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return rangeProbe{}, err
	}
	for key, value := range downloadHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rangeProbe{}, err
	}
	if resp.StatusCode == http.StatusPartialContent {
		total := parseContentRangeTotal(resp.Header.Get("Content-Range"))
		_ = resp.Body.Close()
		if total > 1 {
			return rangeProbe{total: total, supported: true}, nil
		}
		return rangeProbe{}, nil
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return rangeProbe{response: resp}, nil
	}
	status := resp.StatusCode
	_ = resp.Body.Close()
	if status == http.StatusRequestedRangeNotSatisfiable || status == http.StatusMethodNotAllowed {
		return rangeProbe{}, nil
	}
	return rangeProbe{}, fmt.Errorf("HTTP %d", status)
}

func parseContentRangeTotal(value string) int64 {
	value = strings.TrimSpace(value)
	separator := strings.LastIndex(value, "/")
	if separator < 0 || separator == len(value)-1 {
		return 0
	}
	total, err := strconv.ParseInt(value[separator+1:], 10, 64)
	if err != nil || total < 0 {
		return 0
	}
	return total
}

func splitDownloadRanges(total int64, threads int) []downloadRange {
	if total <= 0 {
		return nil
	}
	if threads < 1 {
		threads = 1
	}
	if int64(threads) > total {
		threads = int(total)
	}
	ranges := make([]downloadRange, 0, threads)
	base := total / int64(threads)
	remainder := total % int64(threads)
	var start int64
	for index := 0; index < threads; index++ {
		length := base
		if int64(index) < remainder {
			length++
		}
		ranges = append(ranges, downloadRange{start: start, end: start + length - 1})
		start += length
	}
	return ranges
}

func downloadRangeWithRetry(ctx context.Context, mediaURL string, file *os.File, item downloadRange, reporter *progressReporter, logger *slog.Logger, label string) error {
	if logger == nil {
		logger = slog.Default()
	}
	var lastErr error
	for attempt := 1; attempt <= rangeDownloadRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var credited int64
		err := downloadRangeOnce(ctx, mediaURL, file, item, func(n int64) {
			credited += n
			if reporter != nil {
				reporter.add(n)
			}
		})
		if err == nil {
			return nil
		}
		if credited != 0 && reporter != nil {
			reporter.add(-credited)
		}
		lastErr = err
		if errors.Is(err, errRangeUnsupported) {
			return err
		}
		if attempt < rangeDownloadRetries {
			logger.Warn(label+"分段下载失败, 准备重试", "start", item.start, "end", item.end, "attempt", attempt, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return lastErr
}

func downloadRangeOnce(ctx context.Context, mediaURL string, file *os.File, item downloadRange, onBytes func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return err
	}
	for key, value := range downloadHeaders {
		req.Header.Set(key, value)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", item.start, item.end))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable || resp.StatusCode == http.StatusMethodNotAllowed {
			return errRangeUnsupported
		}
		return fmt.Errorf("分段请求返回 HTTP %d", resp.StatusCode)
	}
	expected := item.end - item.start + 1
	var written int64
	buffer := make([]byte, 256*1024)
	for {
		read, readErr := resp.Body.Read(buffer)
		if read > 0 {
			if written+int64(read) > expected {
				return errors.New("分段响应超出请求范围")
			}
			count, writeErr := file.WriteAt(buffer[:read], item.start+written)
			if writeErr != nil {
				return writeErr
			}
			if count != read {
				return io.ErrShortWrite
			}
			written += int64(count)
			if onBytes != nil && count > 0 {
				onBytes(int64(count))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if written != expected {
		return fmt.Errorf("分段响应长度不匹配: got %d, want %d", written, expected)
	}
	return nil
}

func writeDownloadResponse(ctx context.Context, resp *http.Response, outputPath string, logger *slog.Logger, label string, progress ProgressFunc) (int64, error) {
	if resp == nil || resp.Body == nil {
		return 0, errors.New("下载响应为空")
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return 0, err
	}
	temporaryPath := outputPath + ".part"
	file, err := os.Create(temporaryPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
	}()
	total := resp.ContentLength
	if progress != nil {
		progress(DownloadProgress{Total: total})
	}
	var written int64
	lastReport := time.Now()
	lastProgress := time.Now()
	buffer := make([]byte, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		read, readErr := resp.Body.Read(buffer)
		if read > 0 {
			count, writeErr := file.Write(buffer[:read])
			written += int64(count)
			if writeErr != nil {
				return 0, writeErr
			}
			if written%(1024*1024) < int64(read) || time.Since(lastReport) >= 5*time.Second {
				logger.Info(label+"下载进度", "bytes", written)
				lastReport = time.Now()
			}
			if progress != nil && (written == total || time.Since(lastProgress) >= 100*time.Millisecond) {
				progress(DownloadProgress{Written: written, Total: total})
				lastProgress = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, readErr
		}
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return 0, err
	}
	if progress != nil {
		progress(DownloadProgress{Written: written, Total: total})
	}
	return written, nil
}
