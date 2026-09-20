package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefaultsWithoutCreatingFile(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentVersion || config.Safety.ReadOnly || config.Output.Format != "auto" || config.Download.Threads != 8 || config.Watch.IntervalSeconds != 300 || config.Watch.RequestGapMs != 400 {
		t.Fatalf("unexpected defaults: %#v", config)
	}
	if _, err := os.Stat(store.File); !os.IsNotExist(err) {
		t.Fatalf("Load created config file: %v", err)
	}
}

func TestLoadMergesOldDefaultsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	original := "version = 1\n[output]\nformat = \"rich\"\n[safety]\nread_only = true\n"
	if err := os.WriteFile(store.File, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentVersion || config.Download.Threads != 8 || !config.Safety.ReadOnly {
		t.Fatalf("unexpected migrated config: %#v", config)
	}
	data, err := os.ReadFile(store.File)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("Load rewrote config: %q", data)
	}
}

func TestUpgradeMergesDefaultsAndPreservesValues(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	content := "version = 1\n[output]\nformat = \"rich\"\n[download]\nthreads = 4\n[safety]\nread_only = true\n[extra]\nvalue = \"keep\"\n"
	if err := os.WriteFile(store.File, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := store.Upgrade()
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentVersion || config.Download.Threads != 4 || !config.Safety.ReadOnly || config.Output.Format != "rich" {
		t.Fatalf("unexpected upgraded config: %#v", config)
	}
	data, err := os.ReadFile(store.File)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"version = 3", "threads = 4", "timeout_seconds = 30", "confirm_dangerous_actions = true", "interval_seconds = 300", "value ="} {
		if !strings.Contains(text, expected) {
			t.Fatalf("upgraded config missing %q: %s", expected, text)
		}
	}
}

func TestEnsureWritesDefaultTOML(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	config, err := store.Ensure()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.File)
	if err != nil {
		t.Fatal(err)
	}
	if config.Safety.ConfirmDangerousActions != true || len(data) == 0 {
		t.Fatalf("unexpected generated config: %#v", config)
	}
}

func TestLoadMigratesVersionZeroAndKeepsReadOnly(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	if err := os.WriteFile(store.File, []byte("[safety]\nread_only = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentVersion || !config.Safety.ReadOnly || !config.Safety.ConfirmDangerousActions {
		t.Fatalf("migration lost safety settings: %#v", config)
	}
}

func TestLoadRejectsUnknownOutputMode(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	if err := os.WriteFile(store.File, []byte("version = 1\n[output]\nformat = \"xml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted unknown output mode")
	}
}

func TestStatusReportsMissingAndSetFields(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	report := store.Status()
	if report.Status != "missing" || report.Exists || report.Loaded || !report.NeedsUpgrade {
		t.Fatalf("unexpected missing status: %#v", report)
	}
	if report.EffectiveVersion != CurrentVersion || len(report.Fields) != 10 {
		t.Fatalf("unexpected missing status details: %#v", report)
	}
	if err := os.WriteFile(store.File, []byte("version = 2\n[download]\nthreads = 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report = store.Status()
	if report.Status != "set" || !report.Loaded || !report.NeedsUpgrade {
		t.Fatalf("unexpected set status: %#v", report)
	}
	for _, field := range report.Fields {
		if field.Path == "download.threads" && (field.Status != "set" || field.Value != int64(4) && field.Value != 4) {
			t.Fatalf("unexpected thread status: %#v", field)
		}
	}
}

func TestStatusReportsConfigError(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	if err := os.WriteFile(store.File, []byte("version = 2\n[output]\nformat = \"xml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := store.Status()
	if report.Status != "error" || report.Error == "" || len(report.Errors) != 1 {
		t.Fatalf("unexpected error status: %#v", report)
	}
}

func TestStatusRejectsExplicitZeroDownloadThreads(t *testing.T) {
	dir := t.TempDir()
	store := &Store{Dir: dir, File: filepath.Join(dir, "config.toml")}
	if err := os.WriteFile(store.File, []byte("version = 2\n[download]\nthreads = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := store.Status()
	if report.Status != "error" || !strings.Contains(report.Error, "download.threads") {
		t.Fatalf("unexpected zero thread status: %#v", report)
	}
}
