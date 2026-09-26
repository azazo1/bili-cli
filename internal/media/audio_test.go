package media

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadFileReportsProgress(t *testing.T) {
	const content = "downloaded media"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = w.Write([]byte(content))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "media.bin")
	updates := []DownloadProgress{}
	written, err := DownloadFileWithProgress(context.Background(), server.URL, path, nil, func(progress DownloadProgress) {
		updates = append(updates, progress)
	})
	if err != nil || written != int64(len(content)) {
		t.Fatalf("DownloadFileWithProgress() = %d, %v", written, err)
	}
	if len(updates) == 0 {
		t.Fatal("missing progress updates")
	}
	last := updates[len(updates)-1]
	if last.Written != int64(len(content)) || last.Total != int64(len(content)) {
		t.Fatalf("unexpected final progress: %#v", last)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("unexpected download: %q, %v", data, err)
	}
}

func TestDownloadFileWithThreadsUsesHTTPRanges(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 100)
	var active int32
	var maximum int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end := 0, len(content)-1
		if value := r.Header.Get("Range"); value != "" {
			if _, err := fmt.Sscanf(value, "bytes=%d-%d", &start, &end); err != nil {
				http.Error(w, "invalid range", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
			w.WriteHeader(http.StatusPartialContent)
		}
		current := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&maximum)
			if current <= old || atomic.CompareAndSwapInt32(&maximum, old, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write(content[start : end+1])
		atomic.AddInt32(&active, -1)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "media.bin")
	written, err := DownloadFileWithThreads(context.Background(), server.URL, path, nil, 4)
	if err != nil || written != int64(len(content)) {
		t.Fatalf("DownloadFileWithThreads() = %d, %v", written, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("unexpected ranged download: %d bytes, %v", len(data), err)
	}
	if atomic.LoadInt32(&maximum) < 2 {
		t.Fatalf("range requests were not concurrent: maximum=%d", maximum)
	}
}

func TestParallelDownloadReportsProgressBeforeRangeCompletes(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 200)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unlock()
	started := make(chan struct{})
	var startedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, end := 0, len(content)-1
		if value := r.Header.Get("Range"); value != "" {
			if _, err := fmt.Sscanf(value, "bytes=%d-%d", &start, &end); err != nil {
				http.Error(w, "invalid range", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
			w.WriteHeader(http.StatusPartialContent)
		}
		if end == start {
			_, _ = w.Write(content[start : end+1])
			return
		}
		payload := content[start : end+1]
		head := len(payload) / 5
		if head < 1 {
			head = 1
		}
		_, _ = w.Write(payload[:head])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(payload[head:])
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "media.bin")
	var sawPartial atomic.Bool
	done := make(chan error, 1)
	go func() {
		_, err := DownloadFileWithProgressAndThreads(context.Background(), server.URL, path, nil, func(progress DownloadProgress) {
			if progress.Total > 0 && progress.Written > 0 && progress.Written < progress.Total {
				sawPartial.Store(true)
			}
		}, 4)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("range download did not start")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !sawPartial.Load() && time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("download finished before partial progress: %v", err)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !sawPartial.Load() {
		t.Fatal("progress stayed at 0 while range data was already written")
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("download did not finish")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, content) {
		t.Fatalf("unexpected ranged download: %d bytes, %v", len(data), err)
	}
}

func TestSplitPCM16WAV(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	pcm := make([]byte, 16000*3*2)
	if err := writePCM16WAV(input, pcm, 16000); err != nil {
		t.Fatal(err)
	}
	segments, err := SplitAudio(context.Background(), input, filepath.Join(dir, "parts"), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segment count = %d", len(segments))
	}
	for _, path := range segments {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() <= 44 {
			t.Fatalf("invalid segment %s: %v", path, statErr)
		}
	}
}
