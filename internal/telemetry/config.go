package telemetry

import (
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

const configDir = "inframind"
const configFile = "config.yaml"

type Config struct {
	TelemetryEnabled bool      `yaml:"telemetry_enabled"`
	InstallationID   string    `yaml:"installation_id"`
	FirstSeen        time.Time `yaml:"first_seen"`
	NoticePrinted    bool      `yaml:"notice_printed"`
}

func ConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.TempDir(), ".config")
	}
	return filepath.Join(dir, configDir, configFile)
}

func LoadConfig() (*Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func DefaultConfig() *Config {
	return &Config{
		TelemetryEnabled: true,
		InstallationID:   uuid.NewString(),
		FirstSeen:        time.Now().UTC(),
		NoticePrinted:    false,
	}
}
