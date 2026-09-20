package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

func (s *Store) Upgrade() (Config, error) {
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

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("解析 config.toml 失败: %w", err)
	}
	sourceVersion := rawInt(raw, "version")
	if sourceVersion > CurrentVersion {
		return Config{}, fmt.Errorf("config.toml 版本 %d 高于当前支持版本 %d", sourceVersion, CurrentVersion)
	}
	config, _, err := decode(data)
	if err != nil {
		return Config{}, err
	}

	var defaults map[string]any
	if err := toml.Unmarshal([]byte(DefaultTOML), &defaults); err != nil {
		return Config{}, fmt.Errorf("解析默认配置失败: %w", err)
	}
	merged := mergeTOMLMaps(defaults, raw)
	setKnownValues(merged, config)
	encoded, err := toml.Marshal(merged)
	if err != nil {
		return Config{}, fmt.Errorf("编码配置失败: %w", err)
	}
	if err := s.write(encoded); err != nil {
		return Config{}, err
	}
	return config, nil
}

func mergeTOMLMaps(defaults, values map[string]any) map[string]any {
	merged := make(map[string]any, len(defaults)+len(values))
	for key, value := range defaults {
		merged[key] = value
	}
	for key, value := range values {
		defaultValue, hasDefault := merged[key]
		defaultMap, defaultIsMap := defaultValue.(map[string]any)
		valueMap, valueIsMap := value.(map[string]any)
		if hasDefault && defaultIsMap && valueIsMap {
			merged[key] = mergeTOMLMaps(defaultMap, valueMap)
			continue
		}
		merged[key] = value
	}
	return merged
}

func setKnownValues(values map[string]any, config Config) {
	values["version"] = config.Version
	setTOMLValue(values, []string{"output", "format"}, config.Output.Format)
	setTOMLValue(values, []string{"network", "timeout_seconds"}, config.Network.TimeoutSeconds)
	setTOMLValue(values, []string{"download", "threads"}, config.Download.Threads)
	setTOMLValue(values, []string{"safety", "read_only"}, config.Safety.ReadOnly)
	setTOMLValue(values, []string{"safety", "confirm_dangerous_actions"}, config.Safety.ConfirmDangerousActions)
	setTOMLValue(values, []string{"watch", "interval_seconds"}, config.Watch.IntervalSeconds)
	setTOMLValue(values, []string{"watch", "request_gap_ms"}, config.Watch.RequestGapMs)
	setTOMLValue(values, []string{"watch", "notify", "webhook"}, config.Watch.Notify.Webhook)
	setTOMLValue(values, []string{"watch", "notify", "exec"}, config.Watch.Notify.Exec)
}

func setTOMLValue(values map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	current := values
	for _, key := range path[:len(path)-1] {
		nested, ok := current[key].(map[string]any)
		if !ok {
			nested = map[string]any{}
			current[key] = nested
		}
		current = nested
	}
	current[path[len(path)-1]] = value
}
