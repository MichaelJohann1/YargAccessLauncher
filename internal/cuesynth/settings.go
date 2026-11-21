package cuesynth

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Settings is the mod's sound options that change the preview.
type Settings struct {
	LookAhead     float64 // seconds; guitar, bass and keys
	DrumLookAhead float64 // seconds
	CueRepeats    bool
	BeatTicks     bool
	ListenRepeat  bool    // Play Style: Listen and Repeat (false: Play What You Hear)
	CallLength    int     // Listen and Repeat: bars in each pattern, 1, 2 or 4
	Master        float64 // 0..1
	Volumes       map[string]float64
}

// DefaultSettings is the mod's defaults.
func DefaultSettings() *Settings {
	return &Settings{LookAhead: 0.3, DrumLookAhead: 0.2, BeatTicks: true, ListenRepeat: true, CallLength: 2,
		Master: 1.0, Volumes: map[string]float64{}}
}

// Volume is one cue's volume, 0..1 (1 when it was never set).
func (s *Settings) Volume(cueID string) float64 {
	if v, ok := s.Volumes[cueID]; ok {
		return v
	}
	return 1.0
}

var sectionRe = regexp.MustCompile(`^\[(.+)\]$`)

// ReadSettings reads the [YargAccess] section of the game's UserData/MelonPreferences.cfg. Defaults
// when it is missing.
func ReadSettings(gameDir string) *Settings {
	s := DefaultSettings()
	raw, err := os.ReadFile(filepath.Join(gameDir, "UserData", "MelonPreferences.cfg"))
	if err != nil {
		return s
	}
	section := ""
	values := map[string]string{}
	var order []string
	for _, line := range pySplitLines(decodeUTF8Sig(raw)) {
		line = pyStrip(line)
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			section = m[1]
			continue
		}
		if section != "YargAccess" || !strings.Contains(line, "=") || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		k = pyStrip(k)
		if _, seen := values[k]; !seen {
			order = append(order, k)
		}
		values[k] = strings.Trim(pyStrip(v), `"`)
	}
	num := func(key string, def float64) float64 {
		v, ok := values[key]
		if !ok {
			return def
		}
		if f, ok := pyFloat(v); ok {
			return f
		}
		return def
	}
	get := func(key, def string) string {
		if v, ok := values[key]; ok {
			return v
		}
		return def
	}
	clamp := func(v float64) float64 { return pmax(0.0, pmin(100.0, v)) / 100 }

	if ms := num("LookAheadMs", 300); ms > 0 {
		s.LookAhead = ms / 1000
	} else {
		s.LookAhead = 0.3
	}
	if ms := num("DrumLookAheadMs", 200); ms > 0 {
		s.DrumLookAhead = ms / 1000
	} else {
		s.DrumLookAhead = 0.2
	}
	s.CueRepeats = strings.ToLower(get("CueRepeats", "false")) == "true"
	s.BeatTicks = strings.ToLower(get("BeatTicks", "true")) == "true"
	// ModSettings: PlayMode is "repeat" or "hear" (anything else is put back to "repeat"); CallLength
	// is "1", "2" or "4" (anything else is put back to "2"). A value saved before CallLengthBars was
	// set counted notes, and the mod resets it to 2 at its next start.
	s.ListenRepeat = get("PlayMode", "repeat") != "hear"
	length := get("CallLength", "2")
	if strings.ToLower(get("CallLengthBars", "false")) != "true" {
		length = "2"
	}
	switch length {
	case "1":
		s.CallLength = 1
	case "4":
		s.CallLength = 4
	default:
		s.CallLength = 2
	}
	s.Master = clamp(num("CueVolume", 100))
	for _, k := range order {
		if strings.HasPrefix(k, "Volume_") {
			s.Volumes[k[7:]] = clamp(num(k, 100))
		}
	}
	return s
}
