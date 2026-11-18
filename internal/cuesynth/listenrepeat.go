package cuesynth

import (
	"math"
	"sort"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
)

// Listen and Repeat, as the mod's Gameplay/ListenRepeat.cs: the chart's notes are split into groups.
// Each group's notes play as a call, shifted earlier by whole beats so the rhythm is the song's, and
// the notes that fall where the call plays are taken out of the chart. The player then plays the
// group back at its real time (the answer).

// SwellLength is the listen and your-turn swells' length: each ends where its call or answer starts.
const SwellLength = 0.4

// Group is one call and its answer, in ticks and song seconds.
type Group struct {
	CallStartTick, StartTick, EndTick int
	CallStart, CallEnd, Start, End    float64
}

// Call is a kept note's call: when its sound plays (Time) and the group it is in.
type Call struct {
	Time  float64
	Group *Group
}

// Calls is a part cut for Listen and Repeat: its groups (by call start) and each kept note's call.
// A nil *Calls is Play What You Hear: no note has a call.
type Calls struct {
	Groups      []*Group
	Size        int // the Call Length the part was cut with
	Kept, Total int // notes kept of the part's notes
	marks       map[any]*Call
}

// Of is a note's call, or nil (always nil on a nil *Calls). note is the kept note's pointer.
func (c *Calls) Of(note any) *Call {
	if c == nil {
		return nil
	}
	return c.marks[note]
}

// plan is ListenRepeat.Plan: groups up to size notes (a group also ends at a gap over a bar or a span
// over two bars), shifts each by the smallest whole number of beats covering the group plus one beat,
// and skips a group whose call would start before the song or less than half a beat after the answer
// before it ends. It returns each grouped note's call, nil for a note taken out or a fixed one.
// ends are the notes' end ticks (their longest sustain). Fixed notes (big rock endings) are never
// grouped or taken out, and a group stops at one.
func plan(ticks, ends []int, times []float64, fixed []bool, tempo *chartfile.TempoMap, size int) ([]*Call, []*Group) {
	n := len(ticks)
	res := tempo.Resolution
	marks := make([]*Call, n)
	var groups []*Group
	prevEnd, havePrev := 0, false
	for k := 0; k < n; {
		if fixed[k] {
			k++
			continue
		}
		members := []int{k}
		j := k + 1
		for j < n && len(members) < size && !fixed[j] {
			if ticks[j]-ticks[j-1] > 4*res || ticks[j]-ticks[k] > 8*res {
				break
			}
			members = append(members, j)
			j++
		}
		first := ticks[k]
		lastEnd := first
		for _, m := range members {
			lastEnd = max(lastEnd, min(ends[m], ticks[m]+res))
		}
		beats := int(math.Ceil((float64(lastEnd-first) + float64(res)) / float64(res)))
		shift := beats * res
		callStart := first - shift
		if callStart < 0 || havePrev && callStart < prevEnd+res/2 {
			k++
			continue
		}
		g := &Group{CallStartTick: callStart, StartTick: first, CallStart: tempo.Time(callStart),
			CallEnd: tempo.Time(lastEnd - shift), Start: times[k]}
		realEnd := first
		for _, m := range members {
			realEnd = max(realEnd, ends[m])
			marks[m] = &Call{Time: tempo.Time(ticks[m] - shift), Group: g}
		}
		g.EndTick = realEnd
		g.End = tempo.Time(realEnd)
		groups = append(groups, g)
		prevEnd, havePrev = realEnd, true
		k = j
	}
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].CallStart < groups[b].CallStart })
	return marks, groups
}

// flags is a note's star power and activator flags.
type flags struct{ star, start, end, act bool }

// moveFlags is ListenRepeat.Remove's flag moves for the notes taken out (keep false): a phrase start
// moves to the next kept note when that note is star power and the phrase does not end before it, a
// phrase end to the kept note before when it is star power, and a drum activator to the next kept
// note. It returns the kept notes' new flags (by the original index).
func moveFlags(f []flags, keep []bool) []flags {
	n := len(f)
	out := append([]flags(nil), f...)
	for i := 0; i < n; i++ {
		if keep[i] {
			continue
		}
		next, prev := -1, -1
		for x := i + 1; x < n; x++ {
			if keep[x] {
				next = x
				break
			}
		}
		for x := i - 1; x >= 0; x-- {
			if keep[x] {
				prev = x
				break
			}
		}
		if f[i].start && next >= 0 && f[next].star {
			ends := false
			for x := i; x < next; x++ {
				ends = ends || f[x].end
			}
			if !ends {
				out[next].start = true
			}
		}
		if f[i].end && prev >= 0 && f[prev].star {
			out[prev].end = true
		}
		if f[i].act && next >= 0 {
			out[next].act = true
		}
	}
	return out
}

// keepOf is the notes kept: the grouped ones and the fixed ones.
func keepOf(marks []*Call, fixed []bool) []bool {
	keep := make([]bool, len(marks))
	for i := range marks {
		keep[i] = marks[i] != nil || fixed[i]
	}
	return keep
}

func newCalls(groups []*Group, size, total int) *Calls {
	return &Calls{Groups: groups, Size: size, Total: total, marks: map[any]*Call{}}
}

// CutNotes is ListenRepeat.Cut for a five-fret part: the kept notes (copies, with star power flags
// moved) and their calls. Big rock ending notes are kept, with no call.
func CutNotes(notes []*chartfile.Note, tempo *chartfile.TempoMap, size int) ([]*chartfile.Note, *Calls) {
	n := len(notes)
	ticks, ends, times := make([]int, n), make([]int, n), make([]float64, n)
	fixed, f := make([]bool, n), make([]flags, n)
	for i, x := range notes {
		ticks[i], ends[i], times[i] = x.Tick, max(x.Tick, x.EndTick), x.Time
		fixed[i], f[i] = x.BRE, flags{x.Star, x.StarStart, x.StarEnd, false}
	}
	marks, groups := plan(ticks, ends, times, fixed, tempo, size)
	keep := keepOf(marks, fixed)
	nf := moveFlags(f, keep)
	c := newCalls(groups, size, n)
	var kept []*chartfile.Note
	for i, x := range notes {
		if !keep[i] {
			continue
		}
		cp := *x
		cp.StarStart, cp.StarEnd = nf[i].start, nf[i].end
		kept = append(kept, &cp)
		if marks[i] != nil {
			c.marks[&cp] = marks[i]
		}
	}
	c.Kept = len(kept)
	return kept, c
}

// CutDrums is ListenRepeat.Cut for drums (the notes have no length).
func CutDrums(notes []*chartfile.DrumNote, tempo *chartfile.TempoMap, size int) ([]*chartfile.DrumNote, *Calls) {
	n := len(notes)
	ticks, times := make([]int, n), make([]float64, n)
	fixed, f := make([]bool, n), make([]flags, n)
	for i, x := range notes {
		ticks[i], times[i] = x.Tick, x.Time
		fixed[i], f[i] = x.BRE, flags{x.Star, x.StarStart, x.StarEnd, x.Activator}
	}
	marks, groups := plan(ticks, ticks, times, fixed, tempo, size)
	keep := keepOf(marks, fixed)
	nf := moveFlags(f, keep)
	c := newCalls(groups, size, n)
	var kept []*chartfile.DrumNote
	for i, x := range notes {
		if !keep[i] {
			continue
		}
		cp := *x
		cp.Hits = append([]chartfile.DrumHit(nil), x.Hits...)
		cp.StarStart, cp.StarEnd, cp.Activator = nf[i].start, nf[i].end, nf[i].act
		kept = append(kept, &cp)
		if marks[i] != nil {
			c.marks[&cp] = marks[i]
		}
	}
	c.Kept = len(kept)
	return kept, c
}

// CutKeys is ListenRepeat.Cut for pro keys (a chord ends with its longest key).
func CutKeys(notes []*chartfile.KeyNote, tempo *chartfile.TempoMap, size int) ([]*chartfile.KeyNote, *Calls) {
	n := len(notes)
	ticks, ends, times := make([]int, n), make([]int, n), make([]float64, n)
	fixed, f := make([]bool, n), make([]flags, n)
	for i, x := range notes {
		ticks[i], times[i] = x.Tick, x.Time
		ends[i] = x.Tick
		for _, e := range x.EndTicks {
			ends[i] = max(ends[i], e)
		}
		fixed[i], f[i] = x.BRE, flags{x.Star, x.StarStart, x.StarEnd, false}
	}
	marks, groups := plan(ticks, ends, times, fixed, tempo, size)
	keep := keepOf(marks, fixed)
	nf := moveFlags(f, keep)
	c := newCalls(groups, size, n)
	var kept []*chartfile.KeyNote
	for i, x := range notes {
		if !keep[i] {
			continue
		}
		cp := *x
		cp.StarStart, cp.StarEnd = nf[i].start, nf[i].end
		kept = append(kept, &cp)
		if marks[i] != nil {
			c.marks[&cp] = marks[i]
		}
	}
	c.Kept = len(kept)
	return kept, c
}

// Swells is ListenRepeat.Tick's sounds: the falling listen swell ending where each call starts and the
// rising your-turn swell ending where its answer starts. None on a nil *Calls.
func (c *Calls) Swells(settings *Settings, bank *Bank) []*Play {
	if c == nil {
		return nil
	}
	var out []*Play
	for _, g := range c.Groups {
		out = append(out, newPlay(g.CallStart-SwellLength, bank.Listen, 0.0, clamp01(0.7*settings.Volume("listen")*settings.Master)))
		out = append(out, newPlay(g.Start-SwellLength, bank.YourTurn, 0.0, clamp01(0.7*settings.Volume("yourturn")*settings.Master)))
	}
	return out
}
