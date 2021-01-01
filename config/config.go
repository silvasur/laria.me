package config

import (
	"encoding/json"
	"os"
	"path"
)

type Config struct {
	ContentRoot  string
	DbDsn        string
	TemplatePath string
	StaticPath   string `json:",omitempty"`
	HttpLaddr    string
	Secret       string
	UpdateUrl    string
}

func loadConfig(configPath string) (*Config, error) {
	f, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var conf Config

	dec := json.NewDecoder(f)
	if err = dec.Decode(&conf); err != nil {
		return nil, err
	}

	return &conf, nil
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}

		configPath = path.Join(configDir, "laria.me", "config.json")

		return loadConfig(configPath)
	}

	return loadConfig(configPath)
}
