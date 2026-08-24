package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var downloadHeaders = map[string]string{
	"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.2 Safari/605.1.15",
	"Referer":    "https://www.bilibili.com/",
}

type DownloadProgress struct {
	Written  int64
	Total    int64
	Elapsed  time.Duration
}

type ProgressFunc func(DownloadProgress)

func Download(ctx context.Context, audioURL, outputPath string, logger *slog.Logger) (int64, error) {
	return DownloadWithThreads(ctx, audioURL, outputPath, logger, DefaultDownloadThreads)
}

func DownloadWithThreads(ctx context.Context, audioURL, outputPath string, logger *slog.Logger, threads int) (int64, error) {
	return downloadWithThreads(ctx, audioURL, outputPath, logger, "音频", nil, threads)
}

func DownloadFile(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger) (int64, error) {
	return DownloadFileWithThreads(ctx, mediaURL, outputPath, logger, DefaultDownloadThreads)
}

func DownloadFileWithProgress(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, progress ProgressFunc) (int64, error) {
	return DownloadFileWithProgressAndThreads(ctx, mediaURL, outputPath, logger, progress, DefaultDownloadThreads)
}

func DownloadFileWithThreads(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, threads int) (int64, error) {
	return DownloadFileWithProgressAndThreads(ctx, mediaURL, outputPath, logger, nil, threads)
}

func DownloadFileWithProgressAndThreads(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, progress ProgressFunc, threads int) (int64, error) {
	return downloadWithThreads(ctx, mediaURL, outputPath, logger, "媒体", progress, threads)
}

func DownloadLiveStreamWithProgress(ctx context.Context, streamURL, outputPath string, logger *slog.Logger, progress ProgressFunc) (int64, error) {
	if strings.TrimSpace(streamURL) == "" {
		return 0, errors.New("直播流地址为空")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return downloadLiveStreamOnce(ctx, streamURL, outputPath, logger, progress)
}

func downloadLiveStreamOnce(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, progress ProgressFunc) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return 0, err
	}
	for key, value := range downloadHeaders {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return 0, err
	}
	tmpPath := outputPath + ".part"
	file, err := os.Create(tmpPath)
	if err != nil {
		return 0, err
	}
	keepPartial := false
	defer func() {
		_ = file.Close()
		if !keepPartial {
			_ = os.Remove(tmpPath)
		}
	}()
	started := time.Now()
	var written int64
	lastReport := started
	lastProgress := started
	if progress != nil {
		progress(DownloadProgress{Total: resp.ContentLength, Elapsed: 0})
	}
	buffer := make([]byte, 256*1024)
	for {
		read, readErr := resp.Body.Read(buffer)
		if read > 0 {
			count, writeErr := file.Write(buffer[:read])
			written += int64(count)
			if writeErr != nil {
				return 0, writeErr
			}
			elapsed := time.Since(started)
			if written%(1024*1024) < int64(read) || time.Since(lastReport) >= 5*time.Second {
				logger.Info("直播流录制进度", "bytes", written, "elapsed", elapsed.Round(time.Second))
				lastReport = time.Now()
			}
			if progress != nil && time.Since(lastProgress) >= 100*time.Millisecond {
				progress(DownloadProgress{Written: written, Total: resp.ContentLength, Elapsed: elapsed})
				lastProgress = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				keepPartial = true
				if closeErr := file.Close(); closeErr != nil {
					return 0, closeErr
				}
				if renameErr := os.Rename(tmpPath, outputPath); renameErr != nil {
					return 0, renameErr
				}
				if progress != nil {
					progress(DownloadProgress{Written: written, Total: resp.ContentLength, Elapsed: time.Since(started)})
				}
				return written, nil
			}
			return 0, readErr
		}
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return 0, err
	}
	if progress != nil {
		progress(DownloadProgress{Written: written, Total: resp.ContentLength, Elapsed: time.Since(started)})
	}
	return written, nil
}
func download(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, label string, progress ProgressFunc) (int64, error) {
	return downloadWithThreads(ctx, mediaURL, outputPath, logger, label, progress, DefaultDownloadThreads)
}

func downloadWithThreads(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, label string, progress ProgressFunc, threads int) (int64, error) {
	if strings.TrimSpace(mediaURL) == "" {
		return 0, errors.New(label + "地址为空")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if threads < 1 {
		threads = DefaultDownloadThreads
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if written, err := downloadOnceWithThreads(ctx, mediaURL, outputPath, logger, label, progress, threads); err == nil {
			return written, nil
		} else {
			lastErr = err
			if attempt < 3 {
				logger.Warn(label+"下载失败, 准备重试", "attempt", attempt, "error", err)
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
		}
	}
	return 0, fmt.Errorf("%s下载失败: %w", label, lastErr)
}

func downloadOnceWithThreads(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, label string, progress ProgressFunc, threads int) (int64, error) {
	if threads <= 1 {
		return downloadOnce(ctx, mediaURL, outputPath, logger, label, progress)
	}
	written, err := downloadOnceParallel(ctx, mediaURL, outputPath, logger, label, progress, threads)
	if errors.Is(err, errRangeUnsupported) {
		return downloadOnce(ctx, mediaURL, outputPath, logger, label, progress)
	}
	return written, err
}

func downloadOnce(ctx context.Context, mediaURL, outputPath string, logger *slog.Logger, label string, progress ProgressFunc) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return 0, err
	}
	for key, value := range downloadHeaders {
		req.Header.Set(key, value)
	}
	client := http.DefaultClient
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return 0, err
	}
	tmpPath := outputPath + ".part"
	file, err := os.Create(tmpPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		file.Close()
		_ = os.Remove(tmpPath)
	}()
	var written int64
	lastReport := time.Now()
	lastProgress := time.Now()
	if progress != nil {
		progress(DownloadProgress{Total: resp.ContentLength})
	}
	buffer := make([]byte, 256*1024)
	for {
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
			if progress != nil && (written == resp.ContentLength || time.Since(lastProgress) >= 100*time.Millisecond) {
				progress(DownloadProgress{Written: written, Total: resp.ContentLength})
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
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return 0, err
	}
	if progress != nil {
		progress(DownloadProgress{Written: written, Total: resp.ContentLength})
	}
	return written, nil
}

func SplitAudio(ctx context.Context, inputPath, outputDir string, segmentSeconds int, logger *slog.Logger) ([]string, error) {
	if segmentSeconds <= 0 {
		return nil, errors.New("segment_seconds 必须大于 0")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}
	if segments, ok, err := splitPCM16WAV(inputPath, outputDir, segmentSeconds); ok {
		return segments, err
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, errors.New("音频切分需要 ffmpeg, 未找到可执行文件")
	}
	pattern := filepath.Join(outputDir, "seg_%03d.wav")
	logger.Info("开始使用 ffmpeg 切分音频", "segment_seconds", segmentSeconds)
	command := exec.CommandContext(ctx, "ffmpeg", "-y", "-hide_banner", "-loglevel", "error", "-i", inputPath, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "segment", "-segment_time", fmt.Sprintf("%d", segmentSeconds), "-reset_timestamps", "1", pattern)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("音频解码失败: %s", message)
	}
	segments, err := filepath.Glob(filepath.Join(outputDir, "seg_*.wav"))
	if err != nil {
		return nil, err
	}
	sort.Strings(segments)
	if len(segments) == 0 {
		return nil, errors.New("音频解码失败: 无帧数据")
	}
	return segments, nil
}

func splitPCM16WAV(inputPath, outputDir string, segmentSeconds int) ([]string, bool, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, false, err
	}
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, false, nil
	}
	var channels, sampleRate, bits int
	var pcm []byte
	position := 12
	for position+8 <= len(data) {
		kind := string(data[position : position+4])
		size := int(binary.LittleEndian.Uint32(data[position+4 : position+8]))
		position += 8
		if size < 0 || position+size > len(data) {
			return nil, true, errors.New("WAV 数据块损坏")
		}
		chunk := data[position : position+size]
		switch kind {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, true, errors.New("WAV 格式块损坏")
			}
			format := binary.LittleEndian.Uint16(chunk[0:2])
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bits = int(binary.LittleEndian.Uint16(chunk[14:16]))
			if format != 1 {
				return nil, false, nil
			}
		case "data":
			pcm = chunk
		}
		position += size
		if size%2 == 1 {
			position++
		}
	}
	if channels != 1 || sampleRate != 16000 || bits != 16 || len(pcm) == 0 {
		return nil, false, nil
	}
	bytesPerSegment := sampleRate * segmentSeconds * channels * bits / 8
	if bytesPerSegment <= 0 {
		return nil, true, errors.New("WAV 分段长度无效")
	}
	paths := make([]string, 0, (len(pcm)+bytesPerSegment-1)/bytesPerSegment)
	for index, start := 0, 0; start < len(pcm); index, start = index+1, start+bytesPerSegment {
		end := start + bytesPerSegment
		if end > len(pcm) {
			end = len(pcm)
		}
		path := filepath.Join(outputDir, fmt.Sprintf("seg_%03d.wav", index))
		if err := writePCM16WAV(path, pcm[start:end], sampleRate); err != nil {
			return nil, true, err
		}
		paths = append(paths, path)
	}
	return paths, true, nil
}

func writePCM16WAV(path string, pcm []byte, sampleRate int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	byteRate := sampleRate * 2
	blockAlign := uint16(2)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+len(pcm)))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(pcm)))
	if _, err := file.Write(header); err != nil {
		return err
	}
	_, err = file.Write(pcm)
	return err
}
