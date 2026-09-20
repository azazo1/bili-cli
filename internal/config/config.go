package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const CurrentVersion = 3

const DefaultTOML = `# Bilibili CLI settings.
version = 3

[output]
# auto, rich, json, or yaml.
format = "auto"

[network]
timeout_seconds = 30

[download]
# 媒体下载使用的 HTTP Range 并发线程数.
threads = 8

[safety]
# Block account write actions and dangerous operations.
read_only = false
# Ask before deleting a dynamic or unfollowing an account.
confirm_dangerous_actions = true

[watch]
# 常驻轮询间隔, 单位秒.
interval_seconds = 300
# 规则之间的请求间隔, 单位毫秒.
request_gap_ms = 400

[watch.notify]
# 事件 JSON POST 地址, 留空表示关闭.
webhook = ""
# 每条事件执行的命令, 留空表示关闭.
exec = ""
`

type Config struct {
	Version  int            `toml:"version" json:"version" yaml:"version"`
	Output   OutputConfig   `toml:"output" json:"output" yaml:"output"`
	Network  NetworkConfig  `toml:"network" json:"network" yaml:"network"`
	Download DownloadConfig `toml:"download" json:"download" yaml:"download"`
	Safety   SafetyConfig   `toml:"safety" json:"safety" yaml:"safety"`
	Watch    WatchConfig    `toml:"watch" json:"watch" yaml:"watch"`
}

type OutputConfig struct {
	Format string `toml:"format" json:"format" yaml:"format"`
}

type NetworkConfig struct {
	TimeoutSeconds int `toml:"timeout_seconds" json:"timeout_seconds" yaml:"timeout_seconds"`
}

type DownloadConfig struct {
	Threads int `toml:"threads" json:"threads" yaml:"threads"`
}

type SafetyConfig struct {
	ReadOnly                bool `toml:"read_only" json:"read_only" yaml:"read_only"`
	ConfirmDangerousActions bool `toml:"confirm_dangerous_actions" json:"confirm_dangerous_actions" yaml:"confirm_dangerous_actions"`
}

type WatchConfig struct {
	IntervalSeconds int               `toml:"interval_seconds" json:"interval_seconds" yaml:"interval_seconds"`
	RequestGapMs    int               `toml:"request_gap_ms" json:"request_gap_ms" yaml:"request_gap_ms"`
	Notify          WatchNotifyConfig `toml:"notify" json:"notify" yaml:"notify"`
}

type WatchNotifyConfig struct {
	Webhook string `toml:"webhook" json:"webhook" yaml:"webhook"`
	Exec    string `toml:"exec" json:"exec" yaml:"exec"`
}

func Default() Config {
	return Config{
		Version: CurrentVersion,
		Output: OutputConfig{
			Format: "auto",
		},
		Network: NetworkConfig{
			TimeoutSeconds: 30,
		},
		Download: DownloadConfig{
			Threads: 8,
		},
		Safety: SafetyConfig{
			ReadOnly:                false,
			ConfirmDangerousActions: true,
		},
		Watch: WatchConfig{
			IntervalSeconds: 300,
			RequestGapMs:    400,
		},
	}
}

type Store struct {
	Dir  string
	File string
}

func NewStore() *Store {
	if dir := strings.TrimSpace(os.Getenv("BILI_CONFIG_DIR")); dir != "" {
		return &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".config", "bilibili-cli")
	return &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
}

func (s *Store) Load() (Config, error) {
	data, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}
	config, _, err := decode(data)
	if err != nil {
		return Config{}, err
	}
	return config, nil
}

func (s *Store) Exists() bool {
	_, err := os.Stat(s.File)
	return err == nil
}

func (s *Store) Ensure() (Config, error) {
	data, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		config := Default()
		if err := s.write([]byte(DefaultTOML)); err != nil {
			return Config{}, err
		}
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}
	config, migrated, err := decode(data)
	if err != nil {
		return Config{}, err
	}
	if migrated {
		if err := s.Save(config); err != nil {
			return Config{}, err
		}
	}
	return config, nil
}

func (s *Store) Save(config Config) error {
	config, _, err := migrate(config, config.Version)
	if err != nil {
		return err
	}
	config, _, err = validate(config)
	if err != nil {
		return err
	}
	data, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("编码配置失败: %w", err)
	}
	return s.write(data)
}

func (s *Store) write(data []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	temporary, err := os.CreateTemp(s.Dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("创建临时配置失败: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置配置权限失败: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("写入配置失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("同步配置失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭临时配置失败: %w", err)
	}
	if err := os.Rename(temporaryName, s.File); err != nil {
		return fmt.Errorf("替换配置失败: %w", err)
	}
	return nil
}

func decode(data []byte) (Config, bool, error) {
	var version struct {
		Version int `toml:"version"`
	}
	if err := toml.Unmarshal(data, &version); err != nil {
		return Config{}, false, fmt.Errorf("解析 config.toml 失败: %w", err)
	}
	config := Default()
	if err := toml.Unmarshal(data, &config); err != nil {
		return Config{}, false, fmt.Errorf("解析 config.toml 失败: %w", err)
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Config{}, false, fmt.Errorf("解析 config.toml 失败: %w", err)
	}
	if value, present := rawPath(raw, "download", "threads"); present {
		if number, ok := rawIntValue(value); ok && number == 0 {
			return Config{}, false, fmt.Errorf("download.threads 必须在 1 到 128 之间")
		}
	}
	config, migrated, err := migrate(config, version.Version)
	if err != nil {
		return Config{}, false, err
	}
	normalized, changed, err := validate(config)
	if err != nil {
		return Config{}, false, err
	}
	return normalized, migrated || changed, nil
}

func rawIntValue(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case int8:
		return int(number), true
	case int16:
		return int(number), true
	case int32:
		return int(number), true
	case int64:
		return int(number), true
	case uint:
		return int(number), true
	case uint8:
		return int(number), true
	case uint16:
		return int(number), true
	case uint32:
		return int(number), true
	case uint64:
		return int(number), true
	case float64:
		return int(number), true
	default:
		return 0, false
	}
}

func rawPath(raw map[string]any, path ...string) (any, bool) {
	if len(path) == 0 || raw == nil {
		return nil, false
	}
	var current any = raw
	for index, key := range path {
		values, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := values[key]
		if !exists {
			return nil, false
		}
		if index == len(path)-1 {
			return value, true
		}
		current = value
	}
	return nil, false
}

func validate(config Config) (Config, bool, error) {
	changed := false
	format := strings.ToLower(strings.TrimSpace(config.Output.Format))
	if format == "" {
		format = Default().Output.Format
		changed = true
	}
	switch format {
	case "auto", "rich", "json", "yaml":
		config.Output.Format = format
	default:
		return Config{}, false, fmt.Errorf("output.format 不支持: %s", config.Output.Format)
	}
	if config.Network.TimeoutSeconds == 0 {
		config.Network.TimeoutSeconds = Default().Network.TimeoutSeconds
		changed = true
	}
	if config.Network.TimeoutSeconds < 1 || config.Network.TimeoutSeconds > 300 {
		return Config{}, false, fmt.Errorf("network.timeout_seconds 必须在 1 到 300 之间")
	}
	if config.Download.Threads == 0 {
		config.Download.Threads = Default().Download.Threads
		changed = true
	}
	if config.Download.Threads < 1 || config.Download.Threads > 128 {
		return Config{}, false, fmt.Errorf("download.threads 必须在 1 到 128 之间")
	}
	if config.Watch.IntervalSeconds == 0 {
		config.Watch.IntervalSeconds = Default().Watch.IntervalSeconds
		changed = true
	}
	if config.Watch.IntervalSeconds < 10 || config.Watch.IntervalSeconds > 86400 {
		return Config{}, false, fmt.Errorf("watch.interval_seconds 必须在 10 到 86400 之间")
	}
	if config.Watch.RequestGapMs < 0 || config.Watch.RequestGapMs > 10000 {
		return Config{}, false, fmt.Errorf("watch.request_gap_ms 必须在 0 到 10000 之间")
	}
	webhook := strings.TrimSpace(config.Watch.Notify.Webhook)
	if webhook != config.Watch.Notify.Webhook {
		config.Watch.Notify.Webhook = webhook
		changed = true
	}
	if webhook != "" && !strings.HasPrefix(webhook, "http://") && !strings.HasPrefix(webhook, "https://") {
		return Config{}, false, fmt.Errorf("watch.notify.webhook 必须是 http 或 https 地址")
	}
	execCommand := strings.TrimSpace(config.Watch.Notify.Exec)
	if execCommand != config.Watch.Notify.Exec {
		config.Watch.Notify.Exec = execCommand
		changed = true
	}
	return config, changed, nil
}
