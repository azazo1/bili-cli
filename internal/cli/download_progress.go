package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/azazo1/bilibili-cli/internal/media"
)

const downloadProgressWidth = 28

type downloadProgressBar struct {
	destination io.Writer
	enabled     bool
	label       string
	lastWidth   int
}

func newDownloadProgressBar(destination io.Writer, label string) *downloadProgressBar {
	return &downloadProgressBar{
		destination: destination,
		enabled:     isInteractiveDownloadOutput(destination),
		label:       label,
	}
}

func (p *downloadProgressBar) Update(progress media.DownloadProgress) {
	if p == nil || !p.enabled {
		return
	}
	line := p.line(progress)
	padded := line
	if len(padded) < p.lastWidth {
		padded += strings.Repeat(" ", p.lastWidth-len(padded))
	}
	fmt.Fprintf(p.destination, "\r%s", padded)
	p.lastWidth = len(line)
}

func (p *downloadProgressBar) Finish() {
	if p != nil && p.enabled && p.lastWidth > 0 {
		fmt.Fprintln(p.destination)
	}
}

func (p *downloadProgressBar) line(progress media.DownloadProgress) string {
	elapsed := formatElapsed(progress.Elapsed)
	if progress.Total <= 0 {
		return fmt.Sprintf("%s [%s] %s 已录制 %s", p.label, strings.Repeat("-", downloadProgressWidth), formatDownloadSize(progress.Written), elapsed)
	}
	percentage := int(progress.Written * 100 / progress.Total)
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	filled := downloadProgressWidth * percentage / 100
	bar := strings.Repeat("=", filled) + strings.Repeat("-", downloadProgressWidth-filled)
	return fmt.Sprintf("%s [%s] %3d%% %s/%s", p.label, bar, percentage, formatDownloadSize(progress.Written), formatDownloadSize(progress.Total))
}

func formatElapsed(value time.Duration) string {
	seconds := int64(value / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds/60)%60, seconds%60)
}

func isInteractiveDownloadOutput(destination io.Writer) bool {
	file, ok := destination.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func formatDownloadSize(value int64) string {
	const kib = int64(1024)
	const mib = kib * 1024
	const gib = mib * 1024
	switch {
	case value >= gib:
		return fmt.Sprintf("%.1f GB", float64(value)/float64(gib))
	case value >= mib:
		return fmt.Sprintf("%.1f MB", float64(value)/float64(mib))
	case value >= kib:
		return fmt.Sprintf("%.1f KB", float64(value)/float64(kib))
	default:
		return fmt.Sprintf("%d B", value)
	}
}
