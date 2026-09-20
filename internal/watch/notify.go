package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Notifier struct {
	Webhook string
	Exec    string
	HTTP    *http.Client
	Logger  *slog.Logger
}

func (n Notifier) Send(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	var first error
	if err := n.sendWebhook(ctx, events); err != nil {
		n.logger().Warn("订阅 webhook 通知失败", "error", err, "events", len(events))
		first = err
	}
	if err := n.sendExec(ctx, events); err != nil {
		n.logger().Warn("订阅命令通知失败", "error", err, "events", len(events))
		if first == nil {
			first = err
		}
	}
	return first
}

func (n Notifier) sendWebhook(ctx context.Context, events []Event) error {
	webhook := strings.TrimSpace(n.Webhook)
	if webhook == "" {
		return nil
	}
	if !strings.HasPrefix(webhook, "http://") && !strings.HasPrefix(webhook, "https://") {
		return fmt.Errorf("webhook 必须是 http 或 https 地址")
	}
	payload, err := json.Marshal(map[string]any{
		"ok":             true,
		"schema_version": "1",
		"data":           map[string]any{"events": events},
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := n.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回 HTTP %d", response.StatusCode)
	}
	n.logger().Info("已发送订阅 webhook", "events", len(events), "status", response.StatusCode)
	return nil
}

func (n Notifier) sendExec(ctx context.Context, events []Event) error {
	command := strings.TrimSpace(n.Exec)
	if command == "" {
		return nil
	}
	var first error
	for _, event := range events {
		if err := n.runExec(ctx, command, event); err != nil {
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (n Notifier) runExec(ctx context.Context, command string, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, shellName(), shellArgs(command)...)
	cmd.Env = append(os.Environ(),
		"BILI_WATCH_KIND="+string(event.Kind),
		"BILI_WATCH_TITLE="+event.Title,
		"BILI_WATCH_URL="+event.URL,
		"BILI_WATCH_SUMMARY="+event.Summary,
		fmt.Sprintf("BILI_WATCH_RULE_ID=%d", event.RuleID),
		"BILI_WATCH_JSON="+string(payload),
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		n.logger().Warn("订阅命令执行失败", "error", err, "output", text)
		if text != "" {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	return nil
}

func shellName() string {
	if runtime.GOOS == "windows" {
		if comspec := strings.TrimSpace(os.Getenv("ComSpec")); comspec != "" {
			return comspec
		}
		return "cmd.exe"
	}
	return "/bin/sh"
}

func shellArgs(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/C", command}
	}
	return []string{"-c", command}
}

func (n Notifier) logger() *slog.Logger {
	if n.Logger != nil {
		return n.Logger
	}
	return slog.Default()
}
