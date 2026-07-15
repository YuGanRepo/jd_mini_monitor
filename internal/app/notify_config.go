package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"mini-proxy/internal/notify"
)

// defaultNotifyConfig returns the disabled, out-of-the-box notification config.
func defaultNotifyConfig() notify.Config {
	return notify.Config{
		Enabled:      false,
		Format:       notify.FormatText,
		DiscountRate: 0.95,
		Title:        "京东购物车价格变动",
	}
}

// LoadNotifyConfig reads the notification config from path. A missing file
// yields the default config (not an error) so first-run is seamless.
func LoadNotifyConfig(path string) (notify.Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultNotifyConfig(), nil
		}
		return defaultNotifyConfig(), err
	}
	config := defaultNotifyConfig()
	if err := json.Unmarshal(content, &config); err != nil {
		return defaultNotifyConfig(), err
	}
	return config, nil
}

// SaveNotifyConfig writes the notification config to path as indented JSON.
func SaveNotifyConfig(path string, config notify.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// loadNotifier builds a Notifier from the config file at path. On any load or
// build error it logs and returns nil so the proxy still runs without
// notifications.
func loadNotifier(path string, logger *log.Logger) *notify.Notifier {
	config, err := LoadNotifyConfig(path)
	if err != nil {
		if logger != nil {
			logger.Printf("notify: load config failed, notifications disabled: %v", err)
		}
		return nil
	}
	notifier, err := notify.New(config, logger)
	if err != nil {
		if logger != nil {
			logger.Printf("notify: build notifier failed, notifications disabled: %v", err)
		}
		return nil
	}
	return notifier
}
