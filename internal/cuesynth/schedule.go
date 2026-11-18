package cuesynth

import (
	"math"
	"sort"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
)

// Timing constants shared with the mod.
const (
	MinHold     = 0.25 // shorter sustains get no held tone or Let go
	MinClickGap = 0.07 // the hit bar click needs at least this gap to the note before
	KeyMinGap   = 0.044
)

// Play is one sound placed on the timeline (song seconds).
type Play struct {
	Start  float64
	Sound  *Sound
	Pan    float64
	Volume float64
	Looped bool
	// Stop is when the sound fades out, when HasStop; otherwise it plays out (a looped sound without a
	// stop plays for one second).
	Stop    float64
	HasStop bool
	FadeIn  float64
	FadeOut float64
}

func newPlay(start float64, s *Sound, pan, volume float64) *Play {
	return &Play{Start: start, Sound: s, Pan: pan, Volume: volume, FadeOut: 0.005}
}

func newHold(start float64, s *Sound, pan, volume, stop float64) *Play {
	return &Play{Start: start, Sound: s, Pan: pan, Volume: volume, Looped: true, Stop: stop, HasStop: true,
		FadeIn: 0.06, FadeOut: 0.025}
}

// clamp01 is the schedulers' min(1.0, max(0.0, volume)).
func clamp01(v float64) float64 { return pmin(1.0, pmax(0.0, v)) }

// cutOff stops the sounds of the cue before when a new cue comes under 0.11 s after it.
func cutOff(last []*Play, cue float64) {
	for _, p := range last {
		if !p.HasStop || p.Stop > cue {
			p.Stop, p.HasStop, p.FadeOut = cue, true, 0.005
		}
	}
}

type noteKind uint8

const (
	kindStrum noteKind = iota
	kindHammerOn
	kindPullOff
	kindTap
)

type event struct {
	note    *chartfile.Note
	mask    int
	isOpen  bool
	kind    noteKind
	runEnd  float64
	runSafe float64
	repeat  bool
	gap     float64
	call    *Call // Listen and Repeat: the note's call, nil without one
	cue     float64
}

func events(notes []*chartfile.Note, tempo *chartfile.TempoMap) []*event {
	var out []*event
	prevTop := -1
	for _, n := range notes {
		if n.BRE { // HighwaySound skips big rock ending notes
			continue
		}
		isOpen := false
		for _, f := range n.Frets {
			if f == 0 {
				isOpen = true
			}
		}
		mask := 0
		if isOpen {
			mask = 0x3E
		}
		top := 0
		for _, f := range n.Frets {
			if f != 0 {
				mask |= 1 << f
				top = max(top, f)
			}
		}
		if mask == 0 {
			continue
		}
		if isOpen {
			top = 0
		}
		kind := kindStrum
		switch n.Kind {
		case chartfile.TAP:
			kind = kindTap
		case chartfile.HOPO:
			if prevTop >= 0 && top < prevTop {
				kind = kindPullOff
			} else {
				kind = kindHammerOn
			}
		}
		prevTop = top
		out = append(out, &event{note: n, mask: mask, isOpen: isOpen, kind: kind, runEnd: n.End,
			runSafe: n.Safe, gap: math.Inf(1)})
	}
	prevEnd := math.Inf(-1)
	runStart := -1
	for i, e := range out {
		if i > 0 {
			e.gap = e.note.Time - out[i-1].note.Time
		}
		rest := pmax(tempo.BeatLength(e.note.Time), 0.6)
		if i > 0 && e.note.Time-prevEnd < rest {
			p := out[i-1]
			e.repeat = e.mask == p.mask && (e.kind == kindStrum) == (p.kind == kindStrum)
		}
		if e.repeat && runStart >= 0 {
			r := out[runStart]
			r.runEnd = pmax(r.runEnd, e.note.End)
			r.runSafe = pmax(r.runSafe, e.note.Safe)
		} else {
			runStart = i
		}
		prevEnd = pmax(prevEnd, e.note.End)
	}
	return out
}

// Schedule is every five-fret sound whose cue is from start to end (song seconds), the way
// HighwaySound.Tick fires them: each note's sound the Look-ahead before its hit, held tones through a
// sustain (or a run of the same fret) until YARG has scored it, the chord marker, the hit bar riser
// or click, star power, and the Let go chimes.
//
// With calls (Listen and Repeat: notes and calls from CutNotes) each note's sound plays at its call
// instead: every note sounds (no runs), a held tone stops 50 ms before the answer starts at the
// latest, there is no riser, and the hit bar click plays on each answer note's hit time.
//
// sim is the star power bar for the phrase start chime (nil: every phrase start chimes).
func Schedule(notes []*chartfile.Note, tempo *chartfile.TempoMap, settings *Settings, bank *Bank, calls *Calls,
	sim *StarSim, start, end float64) []*Play {
	lead := settings.LookAhead
	evs := events(notes, tempo)
	for _, e := range evs {
		e.call = calls.Of(e.note)
		e.cue = e.note.Time - lead
		if e.call != nil {
			e.cue = e.call.Time
		}
	}
	vol, master := settings.Volume, settings.Master
	var plays []*Play
	add := func(p *Play) *Play {
		p.Volume = clamp01(p.Volume)
		plays = append(plays, p)
		return p
	}
	var lastCue []*Play
	lastCueTime, lastSpark := -999.0, -999.0
	sparkle := func(n *chartfile.Note, at float64) {
		if n.StarStart && sim.MakesReady(at) {
			lastSpark = n.Time
			add(newPlay(at, bank.SPStart, 0.0, 0.8*vol("spstart")*master))
			return
		}
		if n.Time-lastSpark < 0.15 {
			return
		}
		lastSpark = n.Time
		add(newPlay(at, bank.SPNote, 0.0, 0.8*vol("spnote")*master))
	}

	sorted := append([]*event(nil), evs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].cue < sorted[j].cue })
	for _, e := range sorted {
		cue := e.cue
		if cue < start || cue >= end {
			continue
		}
		hasCall := e.call != nil
		if settings.CueRepeats || !e.repeat || hasCall {
			gap := e.note.Time - lastCueTime
			if gap >= 0.044 {
				if gap < 0.11 {
					cutOff(lastCue, cue)
				}
				lastCue = nil
				holdEnd, holdStop := e.runEnd, e.runSafe
				if settings.CueRepeats || hasCall {
					holdEnd, holdStop = e.note.End, e.note.Safe
				}
				if hasCall {
					holdStop = pmin(holdStop-(e.note.Time-e.call.Time), e.call.Group.Start-0.05)
				}
				held := holdEnd-e.note.Time >= MinHold
				for f := 0; f < 6; f++ {
					if f == 0 && !e.isOpen || f > 0 && (e.isOpen || e.mask&(1<<f) == 0) {
						continue
					}
					kindVol := 1.0
					data := bank.Strike[f]
					switch e.kind {
					case kindHammerOn:
						data, kindVol = bank.Hammer[f], vol("hammeron")
					case kindPullOff:
						data, kindVol = bank.Pull[f], vol("pulloff")
					case kindTap:
						data, kindVol = bank.Tap[f], vol("tap")
					}
					lastCue = append(lastCue, add(newPlay(cue, data, Pans[f], vol(FretIDs[f])*kindVol*master)))
					if held {
						add(newHold(cue, bank.Hold[f], Pans[f], vol(FretIDs[f])*master, holdStop))
					}
				}
				if e.note.Star {
					sparkle(e.note, cue)
				}
				if !e.isOpen && e.mask&(e.mask-1) != 0 {
					add(newPlay(cue, bank.Chord, 0.0, 0.8*vol("chord")*master))
				}
				lastCueTime = e.note.Time
			}
		} else if e.note.Star {
			sparkle(e.note, cue)
		}
		if !hasCall && e.gap >= lead+0.02 {
			add(newPlay(cue, bank.Riser, 0.0, 0.8*vol("sweep")*master))
		}
	}

	for _, e := range evs {
		t := e.note.Time
		if start <= t && t < end && (e.call != nil || MinClickGap <= e.gap && e.gap < lead+0.02) {
			add(newPlay(t, bank.Click, 0.0, 0.7*vol("click")*master))
		}
	}
	for _, e := range evs {
		n := e.note
		if n.End-n.Time >= MinHold && start <= n.Safe && n.Safe < end {
			add(newPlay(n.Safe, bank.Release, 0.0, 0.5*vol("release")*master))
		}
	}
	return plays
}

// NoteStarSim is a five-fret player's star power bar: phrases complete on their last note, and star
// power is turned on as soon as it is ready (the preview can't know when a player would).
func NoteStarSim(notes []*chartfile.Note, tempo *chartfile.TempoMap) *StarSim {
	var awards []float64
	end := 0.0
	for _, n := range notes {
		if n.StarEnd && !n.BRE {
			awards = append(awards, n.Time)
		}
		end = pmax(end, n.End)
	}
	return NewStarSim(tempo, awards, nil, true, end)
}

// KeyStarSim is NoteStarSim for pro keys.
func KeyStarSim(notes []*chartfile.KeyNote, tempo *chartfile.TempoMap) *StarSim {
	var awards []float64
	end := 0.0
	for _, n := range notes {
		if n.StarEnd {
			awards = append(awards, n.Time)
		}
		end = pmax(end, n.End)
	}
	return NewStarSim(tempo, awards, nil, true, end)
}

// cueOrder is the notes' indexes in the order their sounds play (by cue, ties in chart order).
func cueOrder(ev []*chartfile.KeyNote, cl []*Call, lead float64) []int {
	cue := func(i int) float64 {
		if cl[i] != nil {
			return cl[i].Time
		}
		return ev[i].Time - lead
	}
	order := make([]int, len(ev))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return cue(order[a]) < cue(order[b]) })
	return order
}

// KeyMidi is the pitch a key plays: its own, an octave up (key 0, the chart's C3, plays middle C).
func KeyMidi(key int) int { return 60 + key }

// KeyPan is a key's pan, low C left to high C right.
func KeyPan(key int) float64 {
	return -0.9 + 1.8*float64(min(24, max(0, key)))/24
}

func sameKeys(a, b []int) bool {
	set := map[int]bool{}
	for _, k := range a {
		set[k] = true
	}
	other := map[int]bool{}
	for _, k := range b {
		if !set[k] {
			return false
		}
		other[k] = true
	}
	return len(other) == len(set)
}

// ScheduleProkeys is every pro keys sound, the Look-ahead before its hit, the way ProKeysSound.Tick
// fires them. A glissando plays as one white-key run.
//
// With calls (Listen and Repeat, from CutKeys) each note plays at its call as on five-fret, a
// glissando note as a plain note. As in ProKeysSound.Fire, a held tone plays only when its stop is
// after its start: the note's hit time, or its call time in a call.
func ScheduleProkeys(notes []*chartfile.KeyNote, settings *Settings, bank *Bank, keys *KeyBank, calls *Calls,
	sim *StarSim) []*Play {
	lead := settings.LookAhead
	vol, master := settings.Volume, settings.Master
	var plays []*Play
	add := func(p *Play) *Play {
		p.Volume = clamp01(p.Volume)
		plays = append(plays, p)
		return p
	}

	var ev []*chartfile.KeyNote
	for _, n := range notes {
		if len(n.Keys) > 0 {
			ev = append(ev, n)
		}
	}
	cl := make([]*Call, len(ev))
	gliss := make([]bool, len(ev))
	for i, n := range ev {
		cl[i] = calls.Of(n)
		gliss[i] = n.Gliss && cl[i] == nil
	}
	gaps := make([]float64, len(ev))
	repeat := make([]bool, len(ev))
	runEnd := make([]float64, len(ev))
	runSafe := make([]float64, len(ev))
	prevEnd, runStart := math.Inf(-1), -1
	for i, n := range ev {
		gaps[i] = math.Inf(1)
		if i > 0 {
			gaps[i] = n.Time - ev[i-1].Time
		}
		rep := i > 0 && !gliss[i] && n.Time-prevEnd < 0.6 && sameKeys(n.Keys, ev[i-1].Keys) && !gliss[i-1]
		repeat[i] = rep
		runEnd[i], runSafe[i] = n.End, n.Safe
		if rep && runStart >= 0 {
			runEnd[runStart] = pmax(runEnd[runStart], n.End)
			runSafe[runStart] = pmax(runSafe[runStart], n.Safe)
		} else {
			runStart = i
		}
		prevEnd = pmax(prevEnd, n.End)
	}

	glissAt := map[int]*Sound{}
	for i := 0; i < len(ev); {
		if !gliss[i] {
			i++
			continue
		}
		j := i
		for j+1 < len(ev) && gliss[j+1] {
			j++
		}
		length := pmax(0.15, pmin(1.0, ev[j].Time-ev[i].Time))
		glissAt[i] = snd(Gliss(KeyMidi(ev[i].Keys[0]), KeyMidi(ev[j].Keys[0]), length))
		i = j + 1
	}

	var lastCue []*Play
	lastCueTime, lastSpark := -999.0, -999.0
	for _, i := range cueOrder(ev, cl, lead) {
		n := ev[i]
		c := cl[i]
		cue := n.Time - lead
		if c != nil {
			cue = c.Time
		}
		runs := !settings.CueRepeats && c == nil
		if gliss[i] {
			if g, ok := glissAt[i]; ok {
				add(newPlay(cue, g, 0.0, 0.8*vol("keygliss")*master))
			}
		} else if !repeat[i] || !runs {
			gap := n.Time - lastCueTime
			if gap >= KeyMinGap {
				if gap < 0.11 {
					cutOff(lastCue, cue)
				}
				lastCue = nil
				end := n.End
				if runs {
					end = runEnd[i]
				}
				for k, key := range n.Keys {
					m, pan := KeyMidi(key), KeyPan(key)
					lastCue = append(lastCue, add(newPlay(cue, keys.Strike(m), pan, 0.9*vol("keynote")*master)))
					stop := n.Safes[k]
					if runs && runSafe[i] > n.Safe {
						stop = runSafe[i]
					}
					if c != nil {
						stop = pmin(stop-(n.Time-c.Time), c.Group.Start-0.05)
					}
					from := n.Time
					if c != nil {
						from = c.Time
					}
					if end-n.Time >= MinHold && stop > from {
						add(newHold(cue, keys.Hold(m), pan, vol("keynote")*master, stop))
					}
				}
				lastCueTime = n.Time
			}
		}
		if n.Star {
			if n.StarStart && sim.MakesReady(cue) {
				lastSpark = n.Time
				add(newPlay(cue, bank.SPStart, 0.0, 0.8*vol("spstart")*master))
			} else if n.Time-lastSpark >= 0.15 {
				lastSpark = n.Time
				add(newPlay(cue, bank.SPNote, 0.0, 0.8*vol("spnote")*master))
			}
		}
		if !gliss[i] && c == nil && gaps[i] >= lead+0.02 {
			add(newPlay(cue, bank.Riser, 0.0, 0.8*vol("sweep")*master))
		}
	}
	for i, n := range ev {
		if !gliss[i] && (cl[i] != nil || MinClickGap <= gaps[i] && gaps[i] < lead+0.02) {
			add(newPlay(n.Time, bank.Click, 0.0, 0.7*vol("click")*master))
		}
		if n.End-n.Time >= MinHold {
			add(newPlay(n.Safe, bank.Release, 0.0, 0.5*vol("release")*master))
		}
	}
	return plays
}

// BeatTicks is a soft tick on every beat and a stronger one on each bar's first beat, on time (not
// ahead), up to end seconds. None when Beat Ticks is off.
func BeatTicks(tempo *chartfile.TempoMap, settings *Settings, bank *Bank, end float64) []*Play {
	if !settings.BeatTicks {
		return nil
	}
	var out []*Play
	for _, b := range tempo.Beats(end) {
		cueID, s := "beat", bank.Beat
		if b.First {
			cueID, s = "measure", bank.Measure
		}
		out = append(out, newPlay(b.Time, s, 0.0, pmin(1.0, 0.8*settings.Volume(cueID)*settings.Master)))
	}
	return out
}
