package cuesynth

import (
	"sort"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
)

// DrumMinGap matches the mod's DrumSound: notes closer than this sound once.
const DrumMinGap = 0.044

// drumIDs is every pad cue id; a note's pads are a bit set over it.
var drumIDs = []string{"kick", "padred", "padyellow", "padblue", "padgreen", "padgreen5",
	"cymyellow", "cymblue", "cymgreen"}

func drumBit(id string) uint16 {
	for i, d := range drumIDs {
		if d == id {
			return 1 << i
		}
	}
	return 0
}

// DrumSound is the cue id and pan for a hit, as DrumSound.SoundOf / PanOf with YARG's default lane
// order.
func DrumSound(lane int, cymbal bool, mode string) (string, float64) {
	if lane == 0 {
		return "kick", 0.0
	}
	if mode == chartfile.FIVE {
		ids := map[int]string{1: "padred", 2: "cymyellow", 3: "padblue", 4: "cymgreen", 5: "padgreen5"}
		id, ok := ids[lane]
		if !ok {
			id = "padblue"
		}
		return id, []float64{-0.8, -0.4, 0.0, 0.4, 0.8}[min(5, lane)-1]
	}
	pan := 0.75
	switch lane {
	case 1:
		pan = -0.75
	case 2:
		pan = -0.25
	case 3:
		pan = 0.25
	}
	if mode != chartfile.FOUR && cymbal && lane >= 2 && lane <= 4 {
		return []string{"cymyellow", "cymblue", "cymgreen"}[lane-2], pan
	}
	return []string{"padred", "padyellow", "padblue", "padgreen"}[min(4, lane)-1], pan
}

type drumSnd struct {
	id     string
	pan    float64
	volume float64
	mark   int // 1 = ghost (a whisper with the pad), 2 = accent (a crack with it), 0 = plain
}

// drumLevel is DrumSound.Build's pad volume and mark for a hit: a ghost plays softly with a whisper,
// an accent loudly with a crack. The chart reader marks a ghost with Volume 0.45 and an accent with 1.
func drumLevel(h chartfile.DrumHit) (float64, int) {
	switch {
	case h.Volume < 0.6:
		return 0.35, 1
	case h.Volume > 0.9:
		return 1.0, 2
	}
	return 0.75, 0
}

type drumHold struct {
	id  string
	pan float64
}

type drumEvent struct {
	note      *chartfile.DrumNote
	sounds    []drumSnd
	mask      uint16
	call      *Call
	activator bool
	cue       float64
}

// drumLane is a roll or swell lane as DrumSound.Build gathers it: from its first note to its last,
// one or two pads.
type drumLane struct {
	start, end float64
	swell      bool
	pads       []drumHold
}

// ScheduleDrums is every drum sound, the way DrumSound.Tick fires it:
//   - each note's pads the Drums Look-ahead before its hit; a note under 44 ms after the one before
//     sounds only when it adds a pad (a flam), and then plays all its pads;
//   - a ghost note's pad plays softly with a whisper, an accent's loudly with a sharp crack, the
//     whisper or crack at the pad's time and pan;
//   - every note plays its own pads, repeats included, whatever Cue Repeated Notes is (no hit bar
//     sound on drums);
//   - a roll or swell lane plays one looping sound per pad, struck 12 times a second (a swell's two
//     pads in turn), from a Look-ahead before the lane to its last note; the lane's own notes have no
//     cues;
//   - while star power can be activated, a fill plays a slow rise from a Look-ahead before it to a
//     Look-ahead before its end, and its activator note plays the star power hit with its pads;
//   - the star power phrase start chime (when that phrase would make star power ready) and sparkle.
//
// fills are the chart's drum fills. sim is the star power bar (nil: never activatable, every phrase
// start chimes).
//
// With calls (Listen and Repeat, from CutDrums) each note's pads play at its call: every note sounds,
// lane notes play as plain notes, and the answer has no sounds.
func ScheduleDrums(notes []*chartfile.DrumNote, mode string, settings *Settings, bank *Bank, calls *Calls,
	fills []chartfile.Phrase, sim *StarSim) []*Play {
	lead := settings.DrumLookAhead
	vol, master := settings.Volume, settings.Master
	var plays []*Play
	play := func(p *Play) *Play {
		p.Volume = clamp01(p.Volume)
		plays = append(plays, p)
		return p
	}
	var events []*drumEvent
	var lanes []*drumLane
	var lane *drumLane
	for _, n := range notes {
		e := &drumEvent{note: n, call: calls.Of(n)}
		for _, h := range n.Hits {
			id, pan := DrumSound(h.Lane, h.Cymbal, mode)
			if h.InLane && e.call == nil {
				if h.LaneStart || lane == nil {
					if lane != nil {
						lanes = append(lanes, lane)
					}
					lane = &drumLane{start: n.Time, swell: h.Swell}
				}
				have := false
				for _, p := range lane.pads {
					have = have || p.id == id
				}
				if !have && len(lane.pads) < 2 {
					lane.pads = append(lane.pads, drumHold{id, pan})
				}
				lane.end = n.Time
				if h.LaneEnd {
					lanes = append(lanes, lane)
					lane = nil
				}
				continue
			}
			if n.Activator {
				e.activator = true
			}
			dup := false
			for _, s := range e.sounds {
				dup = dup || s.id == id
			}
			if !dup {
				volume, mark := drumLevel(h)
				e.sounds = append(e.sounds, drumSnd{id, pan, volume, mark})
				e.mask |= drumBit(id)
			}
		}
		if len(e.sounds) == 0 {
			continue
		}
		events = append(events, e)
	}
	if lane != nil {
		lanes = append(lanes, lane)
	}
	for _, e := range events {
		e.cue = e.note.Time - lead
		if e.call != nil {
			e.cue = e.call.Time
		}
	}
	order := make([]*drumEvent, len(events))
	copy(order, events)
	sort.SliceStable(order, func(i, j int) bool { return order[i].cue < order[j].cue })

	lastCueTime, lastSpark := -999.0, -999.0
	var lastCueMask uint16
	star := func(n *chartfile.DrumNote, cue float64) {
		if n.StarStart && sim.MakesReady(cue) {
			lastSpark = n.Time
			play(newPlay(cue, bank.SPStart, 0.0, 0.8*vol("spstart")*master))
		} else if n.Star && n.Time-lastSpark >= 0.15 {
			lastSpark = n.Time
			play(newPlay(cue, bank.SPNote, 0.0, 0.8*vol("spnote")*master))
		}
	}
	for _, e := range order {
		n, cue := e.note, e.cue
		if e.activator && sim.CanActivate(cue) {
			play(newPlay(cue, bank.Activator, 0.0, 0.8*vol("drumactivator")*master))
		}
		soon := n.Time-lastCueTime < DrumMinGap
		if !soon || e.mask&^lastCueMask != 0 {
			if soon {
				lastCueMask |= e.mask
			} else {
				lastCueMask = e.mask
			}
			for _, s := range e.sounds {
				play(newPlay(cue, bank.Pads[s.id], s.pan, s.volume*vol(s.id)*master))
				switch s.mark {
				case 1:
					play(newPlay(cue, bank.Ghost, s.pan, 0.9*vol("drumghost")*master))
				case 2:
					play(newPlay(cue, bank.Accent, s.pan, 0.9*vol("drumaccent")*master))
				}
			}
			lastCueTime = n.Time
		}
		star(n, cue)
	}
	for _, l := range lanes {
		cue := l.start - lead
		if l.end <= cue {
			continue
		}
		id := "drumroll"
		if l.swell {
			id = "drumswell"
		}
		for i, p := range l.pads {
			h := newHold(cue, bank.Roll(p.id, i == 1), p.pan, 0.7*vol(id)*master, l.end)
			h.FadeIn = 0.02
			play(h)
		}
	}
	for _, f := range fills {
		cue := f.Time - lead
		if f.End > cue && sim.CanActivate(cue) {
			play(newPlay(cue, bank.FillRise(f.End-lead-cue), 0.0, 0.7*vol("drumfill")*master))
		}
	}
	return plays
}

// DrumStarSim is a perfect drummer's star power bar for notes: phrases complete on their last note,
// and each fill's activator note turns star power on when it can.
func DrumStarSim(notes []*chartfile.DrumNote, tempo *chartfile.TempoMap) *StarSim {
	var awards, acts []float64
	end := 0.0
	for _, n := range notes {
		if n.StarEnd {
			awards = append(awards, n.Time)
		}
		if n.Activator {
			acts = append(acts, n.Time)
		}
		end = pmax(end, n.Time)
	}
	return NewStarSim(tempo, awards, acts, false, end)
}
