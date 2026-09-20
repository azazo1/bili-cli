package watch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	Dir  string
	File string
	Now  func() time.Time
}

func NewStore() *Store {
	dir := strings.TrimSpace(os.Getenv("BILI_CONFIG_DIR"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".config", "bilibili-cli")
	}
	return &Store{
		Dir:  dir,
		File: filepath.Join(dir, "watch.json"),
		Now:  time.Now,
	}
}

func emptyFile() File {
	return File{Version: CurrentVersion, NextID: 1, Rules: []Rule{}}
}

func (s *Store) Load() (File, error) {
	data, err := os.ReadFile(s.File)
	if errors.Is(err, os.ErrNotExist) {
		return emptyFile(), nil
	}
	if err != nil {
		return File{}, fmt.Errorf("读取订阅失败: %w", err)
	}
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return File{}, fmt.Errorf("解析 watch.json 失败: %w", err)
	}
	file, err = migrateFile(file)
	if err != nil {
		return File{}, err
	}
	if file.Rules == nil {
		file.Rules = []Rule{}
	}
	if file.NextID < 1 {
		file.NextID = nextIDFromRules(file.Rules)
	}
	return file, nil
}

func migrateFile(file File) (File, error) {
	switch {
	case file.Version == 0:
		file.Version = CurrentVersion
		if file.NextID < 1 {
			file.NextID = nextIDFromRules(file.Rules)
		}
		return file, nil
	case file.Version == CurrentVersion:
		return file, nil
	case file.Version > CurrentVersion:
		return File{}, fmt.Errorf("watch.json 版本 %d 高于当前支持版本 %d", file.Version, CurrentVersion)
	default:
		return File{}, fmt.Errorf("不支持的 watch.json 版本: %d", file.Version)
	}
}

func nextIDFromRules(rules []Rule) int {
	next := 1
	for _, rule := range rules {
		if rule.ID >= next {
			next = rule.ID + 1
		}
	}
	return next
}

func (s *Store) Save(file File) error {
	file.Version = CurrentVersion
	if file.Rules == nil {
		file.Rules = []Rule{}
	}
	if file.NextID < 1 {
		file.NextID = nextIDFromRules(file.Rules)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("编码订阅失败: %w", err)
	}
	data = append(data, '\n')
	return s.write(data)
}

func (s *Store) write(data []byte) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	temporary, err := os.CreateTemp(s.Dir, ".watch-*.json")
	if err != nil {
		return fmt.Errorf("创建临时订阅失败: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置订阅权限失败: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("写入订阅失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("同步订阅失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭临时订阅失败: %w", err)
	}
	if err := os.Rename(temporaryName, s.File); err != nil {
		return fmt.Errorf("替换订阅失败: %w", err)
	}
	return nil
}

func (s *Store) Add(rules ...Rule) ([]Rule, error) {
	file, err := s.Load()
	if err != nil {
		return nil, err
	}
	now := s.now()
	added := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if file.NextID < 1 {
			file.NextID = 1
		}
		rule.ID = file.NextID
		file.NextID++
		rule.Enabled = true
		if rule.CreatedAt.IsZero() {
			rule.CreatedAt = now
		}
		file.Rules = append(file.Rules, rule)
		added = append(added, rule)
	}
	if err := s.Save(file); err != nil {
		return nil, err
	}
	return added, nil
}

func (s *Store) Remove(id int) (Rule, error) {
	file, err := s.Load()
	if err != nil {
		return Rule{}, err
	}
	for index, rule := range file.Rules {
		if rule.ID != id {
			continue
		}
		file.Rules = append(file.Rules[:index], file.Rules[index+1:]...)
		if err := s.Save(file); err != nil {
			return Rule{}, err
		}
		return rule, nil
	}
	return Rule{}, fmt.Errorf("未找到订阅规则 %d", id)
}

func (s *Store) SetEnabled(id int, enabled bool) (Rule, error) {
	file, err := s.Load()
	if err != nil {
		return Rule{}, err
	}
	for index, rule := range file.Rules {
		if rule.ID != id {
			continue
		}
		file.Rules[index].Enabled = enabled
		if err := s.Save(file); err != nil {
			return Rule{}, err
		}
		return file.Rules[index], nil
	}
	return Rule{}, fmt.Errorf("未找到订阅规则 %d", id)
}

func (s *Store) UpdateState(id int, state State) error {
	file, err := s.Load()
	if err != nil {
		return err
	}
	for index, rule := range file.Rules {
		if rule.ID != id {
			continue
		}
		file.Rules[index].State = state
		return s.Save(file)
	}
	return fmt.Errorf("未找到订阅规则 %d", id)
}

func (s *Store) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
