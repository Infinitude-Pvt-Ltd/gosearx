package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SelectorRule allows unmarshalling both a single string and a list of strings from YAML.
type SelectorRule []string

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (s *SelectorRule) UnmarshalYAML(value *yaml.Node) error {
	var str string
	if err := value.Decode(&str); err == nil {
		*s = []string{str}
		return nil
	}

	var slice []string
	if err := value.Decode(&slice); err == nil {
		*s = slice
		return nil
	}

	return fmt.Errorf("line %d, col %d: cannot unmarshal into string or string slice", value.Line, value.Column)
}

type RedisConfig struct {
	Address  string `yaml:"address"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type CacheConfig struct {
	Enabled bool        `yaml:"enabled"`
	Type    string      `yaml:"type"` // "memory" or "redis"
	TTL     int         `yaml:"ttl"`  // in seconds
	Redis   RedisConfig `yaml:"redis"`
}

type GeneralConfig struct {
	Debug       bool        `yaml:"debug"`
	Port        int         `yaml:"port"`
	BindAddress string      `yaml:"bind_address"`
	Cache       CacheConfig `yaml:"cache"`
	APIKeys     []string    `yaml:"api_keys"`
}

type OutgoingConfig struct {
	RequestTimeout      float64  `yaml:"request_timeout"`
	MaxRequestTimeout   float64  `yaml:"max_request_timeout"`
	PoolMaxIdleConns    int      `yaml:"pool_max_idle_conns"`
	PoolMaxConnsPerHost int      `yaml:"pool_max_conns_per_host"`
	Proxies             []string `yaml:"proxies"`
	TorProxy            string   `yaml:"tor_proxy"` // e.g. "socks5://127.0.0.1:9050"
}

type SelectorConfig struct {
	Result  SelectorRule `yaml:"result"`
	Title   SelectorRule `yaml:"title"`
	URL     SelectorRule `yaml:"url"`
	Content SelectorRule `yaml:"content"`
}

type EngineConfig struct {
	Name           string         `yaml:"name"`
	Type           string         `yaml:"type"` // e.g. "html" or "custom"
	Weight         float64        `yaml:"weight"`
	Categories     []string       `yaml:"categories"`
	SearchURL      string         `yaml:"search_url"`
	Timeout        float64        `yaml:"timeout"`
	Disabled       bool           `yaml:"disabled"`
	UsingTorProxy  bool           `yaml:"using_tor_proxy"` // routes requests through local Tor proxy
	UsingJSRender  bool           `yaml:"using_js_render"` // routes requests through headless Chrome allocator
	Selectors      SelectorConfig `yaml:"selectors"`
}

type Config struct {
	General  GeneralConfig  `yaml:"general"`
	Outgoing OutgoingConfig `yaml:"outgoing"`
	Engines  []EngineConfig `yaml:"engines"`
}

// LoadConfig reads and parses the settings.yml configuration file.
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode yaml: %w", err)
	}

	// Set sensible defaults if empty
	if cfg.General.Port == 0 {
		cfg.General.Port = 8888
	}
	if cfg.General.BindAddress == "" {
		cfg.General.BindAddress = "0.0.0.0"
	}
	if cfg.General.Cache.TTL <= 0 {
		cfg.General.Cache.TTL = 300
	}
	if cfg.General.Cache.Type == "" {
		cfg.General.Cache.Type = "memory"
	}
	if cfg.General.Cache.Redis.Address == "" {
		cfg.General.Cache.Redis.Address = "127.0.0.1:6379"
	}
	if cfg.Outgoing.RequestTimeout <= 0 {
		cfg.Outgoing.RequestTimeout = 2.0
	}
	if cfg.Outgoing.MaxRequestTimeout <= 0 {
		cfg.Outgoing.MaxRequestTimeout = 3.5
	}
	if cfg.Outgoing.PoolMaxIdleConns <= 0 {
		cfg.Outgoing.PoolMaxIdleConns = 100
	}
	if cfg.Outgoing.PoolMaxConnsPerHost <= 0 {
		cfg.Outgoing.PoolMaxConnsPerHost = 20
	}
	if cfg.Outgoing.TorProxy == "" {
		cfg.Outgoing.TorProxy = "socks5://127.0.0.1:9050"
	}

	// Default weights and values for engines
	for i := range cfg.Engines {
		if cfg.Engines[i].Weight <= 0 {
			cfg.Engines[i].Weight = 1.0
		}
		if cfg.Engines[i].Timeout <= 0 {
			cfg.Engines[i].Timeout = cfg.Outgoing.RequestTimeout
		}
		if len(cfg.Engines[i].Categories) == 0 {
			cfg.Engines[i].Categories = []string{"general"}
		}
	}

	return &cfg, nil
}

// FindAndLoadConfig attempts to locate settings.yml in several standard directories.
func FindAndLoadConfig() (*Config, error) {
	paths := []string{
		"config/settings.yml",
		"./settings.yml",
		"../config/settings.yml",
		"/etc/gosearx/settings.yml",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return LoadConfig(p)
		}
	}

	// Try using executable directory as base
	if execPath, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(execPath), "config", "settings.yml")
		if _, err := os.Stat(p); err == nil {
			return LoadConfig(p)
		}
	}

	return nil, fmt.Errorf("could not find settings.yml in standard paths")
}
