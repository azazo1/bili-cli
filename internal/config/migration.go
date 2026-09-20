package config

import "fmt"

func migrate(config Config, sourceVersion int) (Config, bool, error) {
	switch {
	case sourceVersion == 0:
		config = mergeMigrationDefaults(config)
		config.Version = CurrentVersion
		return config, true, nil
	case sourceVersion == 1:
		config = mergeMigrationDefaults(config)
		config.Version = CurrentVersion
		return config, true, nil
	case sourceVersion == 2:
		config = mergeMigrationDefaults(config)
		config.Version = CurrentVersion
		return config, true, nil
	case sourceVersion == CurrentVersion:
		return config, false, nil
	case sourceVersion > CurrentVersion:
		return Config{}, false, fmt.Errorf("config.toml 版本 %d 高于当前支持版本 %d", sourceVersion, CurrentVersion)
	default:
		return Config{}, false, fmt.Errorf("不支持的 config.toml 版本: %d", sourceVersion)
	}
}

func mergeMigrationDefaults(config Config) Config {
	defaults := Default()
	if config.Output.Format == "" {
		config.Output.Format = defaults.Output.Format
	}
	if config.Network.TimeoutSeconds == 0 {
		config.Network.TimeoutSeconds = defaults.Network.TimeoutSeconds
	}
	if config.Download.Threads == 0 {
		config.Download.Threads = defaults.Download.Threads
	}
	if config.Watch.IntervalSeconds == 0 {
		config.Watch.IntervalSeconds = defaults.Watch.IntervalSeconds
	}
	return config
}
