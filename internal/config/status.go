package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type FieldStatus struct {
	Path   string `json:"path" yaml:"path"`
	Status string `json:"status" yaml:"status"`
	Value  any    `json:"value" yaml:"value"`
	Error  string `json:"error,omitempty" yaml:"error,omitempty"`
}

type StatusReport struct {
	File             string        `json:"file" yaml:"file"`
	Status           string        `json:"status" yaml:"status"`
	FileStatus       string        `json:"file_status" yaml:"file_status"`
	Exists           bool          `json:"exists" yaml:"exists"`
	Loaded           bool          `json:"loaded" yaml:"loaded"`
	SourceVersion    int           `json:"source_version" yaml:"source_version"`
	EffectiveVersion int           `json:"effective_version" yaml:"effective_version"`
	CurrentVersion   int           `json:"current_version" yaml:"current_version"`
	NeedsUpgrade     bool          `json:"needs_upgrade" yaml:"needs_upgrade"`
	Config           Config        `json:"config" yaml:"config"`
	Fields           []FieldStatus `json:"fields" yaml:"fields"`
	Error            string        `json:"error,omitempty" yaml:"error,omitempty"`
	Errors           []string      `json:"errors,omitempty" yaml:"errors,omitempty"`
}

type statusField struct {
	path  string
	value func(Config) any
}

var knownStatusFields = []statusField{
	{path: "version", value: func(config Config) any { return config.Version }},
	{path: "output.format", value: func(config Config) any { return config.Output.Format }},
	{path: "network.timeout_seconds", value: func(config Config) any { return config.Network.TimeoutSeconds }},
	{path: "download.threads", value: func(config Config) any { return config.Download.Threads }},
	{path: "safety.read_only", value: func(config Config) any { return config.Safety.ReadOnly }},
	{path: "safety.confirm_dangerous_actions", value: func(config Config) any {
		return config.Safety.ConfirmDangerousActions
	}},
	{path: "watch.interval_seconds", value: func(config Config) any { return config.Watch.IntervalSeconds }},
	{path: "watch.request_gap_ms", value: func(config Config) any { return config.Watch.RequestGapMs }},
	{path: "watch.notify.webhook", value: func(config Config) any { return config.Watch.Notify.Webhook }},
	{path: "watch.notify.exec", value: func(config Config) any { return config.Watch.Notify.Exec }},
}

func (s *Store) Status() StatusReport {
	report := StatusReport{
		File:           s.File,
		Status:         "missing",
		FileStatus:     "missing",
		EffectiveVersion: CurrentVersion,
		CurrentVersion: CurrentVersion,
		NeedsUpgrade:   true,
		Config:         Default(),
	}

	data, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		report.Fields = buildFieldStatuses(nil, report.Config, "")
		return report
	}
	if err != nil {
		report.Fields = buildFieldStatuses(nil, report.Config, err.Error())
		return report.withError(fmt.Errorf("读取配置失败: %w", err))
	}
	report.Exists = true
	report.Status = "set"
	report.FileStatus = "set"

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		report.Fields = buildFieldStatuses(nil, report.Config, err.Error())
		return report.withError(fmt.Errorf("解析 config.toml 失败: %w", err))
	}
	report.SourceVersion = rawInt(raw, "version")
	config, changed, decodeErr := decode(data)
	if decodeErr != nil {
		report.Fields = buildFieldStatuses(raw, report.Config, decodeErr.Error())
		return report.withError(decodeErr)
	}
	report.Loaded = true
	report.Config = config
	report.EffectiveVersion = config.Version
	report.Fields = buildFieldStatuses(raw, config, "")
	report.NeedsUpgrade = changed || report.SourceVersion < CurrentVersion || hasMissingFields(report.Fields)
	return report
}

func hasMissingFields(fields []FieldStatus) bool {
	for _, field := range fields {
		if field.Status == "missing" {
			return true
		}
	}
	return false
}

func (r StatusReport) withError(err error) StatusReport {
	r.Error = err.Error()
	r.Errors = []string{err.Error()}
	r.Status = "error"
	r.FileStatus = "error"
	r.NeedsUpgrade = false
	if r.EffectiveVersion == 0 {
		r.EffectiveVersion = r.Config.Version
	}
	return r
}

func buildFieldStatuses(raw map[string]any, config Config, decodeError string) []FieldStatus {
	fields := make([]FieldStatus, 0, len(knownStatusFields))
	for _, field := range knownStatusFields {
		value, present := rawPath(raw, strings.Split(field.path, ".")...)
		state := "missing"
		fieldError := ""
		if present {
			state = "set"
		}
		if decodeError != "" && present && strings.Contains(decodeError, field.path) {
			state = "error"
			fieldError = decodeError
		}
		if !present {
			value = field.value(config)
		}
		fields = append(fields, FieldStatus{
			Path:   field.path,
			Status: state,
			Value:  value,
			Error:  fieldError,
		})
	}
	return fields
}

func rawInt(raw map[string]any, key string) int {
	value, ok := raw[key]
	if !ok {
		return 0
	}
	if number, ok := rawIntValue(value); ok {
		return number
	}
	return 0
}
