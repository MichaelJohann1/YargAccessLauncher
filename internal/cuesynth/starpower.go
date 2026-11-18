package cuesynth

import (
	"sort"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
)

// StarSim is a perfect player's star power bar, for the sounds the mod plays from the live bar: the
// phrase start chime (HighwaySound.PhraseMakesReady), and on drums the fill rise and the star power
// hit (DrumsEngine.CanStarPowerActivate). As YARG's BaseEngine: a completed phrase (its last note
// hit) adds a quarter of the bar; star power can be activated from half a bar; while it runs the bar
// drains by an eighth per measure, and it ends when the bar is empty.
//
// The preview cannot know when a player turns star power on. On drums a perfect player activates it
// by hitting each fill's last note when the bar is at least half full (YARG activates on that hit).
// On guitar, bass, keys and pro keys the player is taken to turn it on as soon as it is ready.
type StarSim struct {
	events []starEvent
	bars   []int // bar start ticks
	tempo  *chartfile.TempoMap
	auto   bool
}

type starEvent struct {
	time     float64
	activate bool // an activator note hit; false: a phrase completed
}

// NewStarSim is the bar for phrases completed at awards (song seconds) and activator note hits at
// activators; auto: star power is turned on as soon as it is ready.
func NewStarSim(tempo *chartfile.TempoMap, awards, activators []float64, auto bool, end float64) *StarSim {
	s := &StarSim{tempo: tempo, auto: auto}
	for _, t := range awards {
		s.events = append(s.events, starEvent{t, false})
	}
	for _, t := range activators {
		s.events = append(s.events, starEvent{t, true})
	}
	// A phrase completed on an activator note counts first (DrumsEngine.HitNote awards, then activates).
	sort.SliceStable(s.events, func(i, j int) bool {
		a, b := s.events[i], s.events[j]
		return a.time < b.time || a.time == b.time && !a.activate && b.activate
	})
	for _, b := range tempo.Beats(end + 60) {
		if b.First {
			s.bars = append(s.bars, b.Tick)
		}
	}
	return s
}

// measure is the song position in measures (YARG drains star power by measure ticks).
func (s *StarSim) measure(t float64) float64 {
	if len(s.bars) == 0 {
		return 0
	}
	tick := s.tempo.TickAt(t)
	i := sort.SearchInts(s.bars, tick+1) - 1
	if i < 0 {
		return 0
	}
	length := 4 * s.tempo.Resolution
	if i+1 < len(s.bars) {
		length = s.bars[i+1] - s.bars[i]
	} else if i > 0 {
		length = s.bars[i] - s.bars[i-1]
	}
	return float64(i) + float64(tick-s.bars[i])/float64(max(1, length))
}

// At is the bar (0 to 1) and whether star power is running at song time t.
func (s *StarSim) At(t float64) (float64, bool) {
	bar, active := 0.0, false
	last := 0.0
	drain := func(to float64) {
		if active {
			bar -= (s.measure(to) - s.measure(last)) / 8
			if bar <= 1e-9 {
				bar, active = 0, false
			}
		}
		last = to
	}
	for _, e := range s.events {
		if e.time > t {
			break
		}
		drain(e.time)
		if e.activate {
			if bar >= 0.5-1e-9 {
				active = true
			}
			continue
		}
		bar = pmin(1, bar+0.25)
		if s.auto && bar >= 0.5-1e-9 {
			active = true
		}
	}
	drain(t)
	return bar, active
}

// MakesReady is HighwaySound.PhraseMakesReady at t: star power is not running and can't be
// activated yet, and one more phrase would make it ready.
func (s *StarSim) MakesReady(t float64) bool {
	if s == nil {
		return true
	}
	bar, active := s.At(t)
	return !active && bar < 0.5-1e-9 && bar+0.25 >= 0.5-1e-6
}

// CanActivate is BaseEngine.CanStarPowerActivate at t: half a bar or more (running or not).
func (s *StarSim) CanActivate(t float64) bool {
	if s == nil {
		return false
	}
	bar, _ := s.At(t)
	return bar >= 0.5-1e-9
}
