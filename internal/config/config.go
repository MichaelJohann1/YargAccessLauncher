// Package config holds the launcher settings, stored in %APPDATA%\YARG Access\config.json.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func envOrHome(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return home
}

var (
	Dir     = filepath.Join(envOrHome("APPDATA"), "YARG Access")
	Path    = filepath.Join(Dir, "config.json")
	LogPath = filepath.Join(Dir, "launcher.log")

	DefaultContentRoot = filepath.Join(envOrHome("LOCALAPPDATA"), "YARC")
)

const (
	DefaultModRepo  = "MichaelJohann1/YargAccess"
	DefaultModAsset = `^YargAccess-.*\.zip$`
)

// Config field names match the Python launcher's config.json, so both read the same file.
type Config struct {
	ContentRoot       string `json:"content_root"`
	GameDir           string `json:"game_dir"`
	NightlyGameDir    string `json:"nightly_game_dir"`
	Channel           string `json:"channel"`
	ModRepo           string `json:"mod_repo"`
	ModAssetPattern   string `json:"mod_asset_pattern"`
	ModLocalPath      string `json:"mod_local_path"`
	MelonLoaderVer    string `json:"melonloader_version"`
	SongsDir          string `json:"songs_dir"`
	AddSongsDirToYarg bool   `json:"add_songs_dir_to_yarg"`
	ProgressInterval  int    `json:"progress_interval"`
	NightlyWarned     bool   `json:"nightly_warned"`
	AnnounceProgress  bool   `json:"announce_progress"`
}

func Default() *Config {
	return &Config{
		ContentRoot:       DefaultContentRoot,
		Channel:           "stable",
		ModRepo:           DefaultModRepo,
		ModAssetPattern:   DefaultModAsset,
		MelonLoaderVer:    "v0.7.2",
		AddSongsDirToYarg: true,
		ProgressInterval:  5,
		AnnounceProgress:  true,
	}
}

// GameDirSetting is the game folder chosen for the current build: Stable and Nightly each have
// their own, so switching builds never installs one over the other.
func (c *Config) GameDirSetting() string {
	if c.Channel == "nightly" {
		return c.NightlyGameDir
	}
	return c.GameDir
}

func (c *Config) SetGameDirSetting(dir string) {
	if c.Channel == "nightly" {
		c.NightlyGameDir = dir
	} else {
		c.GameDir = dir
	}
}

func (c *Config) ResolvedGameDir() string {
	if d := c.GameDirSetting(); d != "" {
		return d
	}
	return filepath.Join(c.ContentRoot, "YARG Installs", "yarg-access-"+c.Channel, "installation")
}

func (c *Config) ResolvedSongsDir() string {
	if c.SongsDir != "" {
		return c.SongsDir
	}
	return filepath.Join(c.ContentRoot, "Songs", "Chorus")
}

// Load reads the settings; a missing or damaged file gives the defaults. Unknown keys are ignored.
func Load() *Config {
	cfg := Default()
	data, err := os.ReadFile(Path)
	if err != nil {
		return cfg
	}
	loaded := *cfg
	if json.Unmarshal(data, &loaded) == nil {
		*cfg = loaded
	}
	return cfg
}

func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path)
}
