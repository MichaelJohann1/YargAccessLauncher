package cuesynth

import (
	"math"
	"sort"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
)

// Listen and Repeat, as the mod's Gameplay/ListenRepeat.cs: the chart is split into windows of Call
// Length bars (1, 2 or 4). The notes of one window play as a call in the window before it, whole bars
// earlier so the rhythm is the song's, and the chart's notes in that listen window are taken out. The
// player then plays the window back at its real time (the answer). Listen and answer both start on a
// bar line.

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
	Size        int // the Call Length the part was cut with, in bars
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

// plan is ListenRepeat.PlanBars: the answer is `size` whole bars starting on a bar line, the listen is
// the `size` bars before it, and each answer note's call is the note moved back by the listen's length
// in ticks. The chart's notes in the listen bars are taken out, and the next listen starts after the
// answer (or where the notes come back after a rest). A note whose call would not fit in a shorter
// listen bar (a time signature change) is taken out. It returns each grouped note's call, nil for a
// note taken out or a fixed one. ends are the notes' end ticks (their longest sustain). Fixed notes
// (big rock endings) are never grouped or taken out.
func plan(ticks, ends []int, times []float64, fixed []bool, tempo *chartfile.TempoMap, size int) ([]*Call, []*Group) {
	n := len(ticks)
	marks := make([]*Call, n)
	var groups []*Group
	if n == 0 || size <= 0 {
		return marks, groups
	}
	last := 0
	for i := range ticks {
		last = max(last, ticks[i], ends[i])
	}
	bars := barTicks(tempo, ticks[0], last, 2*size+2)
	minAnswer := size
	for k := 0; k < n; {
		if fixed[k] {
			k++
			continue
		}
		s := max(barOf(bars, ticks[k]), minAnswer)
		if s+size >= len(bars) {
			break
		}
		callStart, start, end := bars[s-size], bars[s], bars[s+size]
		shift := start - callStart
		j := k
		for j < n && ticks[j] < start {
			j++
		}
		var members []int
		realEnd := end - 1
		for ; j < n && ticks[j] < end; j++ {
			if fixed[j] || ticks[j]-shift >= start {
				continue
			}
			members = append(members, j)
			realEnd = max(realEnd, ends[j])
		}
		// The next listen starts on the first bar line after the answer, or after a held note that runs
		// past it.
		minAnswer = barOf(bars, realEnd) + 1 + size
		k = j
		if len(members) == 0 {
			continue
		}
		g := &Group{CallStartTick: callStart, StartTick: start, EndTick: realEnd,
			CallStart: tempo.Time(callStart), CallEnd: tempo.Time(start), Start: tempo.Time(start), End: tempo.Time(realEnd)}
		for _, m := range members {
			marks[m] = &Call{Time: tempo.Time(ticks[m] - shift), Group: g}
		}
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(a, b int) bool { return groups[a].CallStart < groups[b].CallStart })
	return marks, groups
}

// barTicks is ListenRepeat.BarTicks: the bar lines from before the first note to `extra` bars past the
// last tick (the chart's Measure lines, else bars from its time signatures; past the end the last bar's
// length repeated).
func barTicks(tempo *chartfile.TempoMap, first, last, extra int) []int {
	res := tempo.Resolution
	if res <= 0 {
		res = 480
	}
	bars := append([]int(nil), tempo.Bars...)
	if len(bars) < 2 {
		bars = bars[:0]
		sigs := tempo.Signatures
		for i, s := range sigs {
			l := barLength(s.Num, s.Den, res)
			stop := last + 1
			if i+1 < len(sigs) {
				stop = sigs[i+1].Tick
			}
			for t := s.Tick; t < stop; t += l {
				if len(bars) == 0 || t > bars[len(bars)-1] {
					bars = append(bars, t)
				}
			}
		}
		if len(bars) == 0 {
			bars = append(bars, 0)
		}
		if len(bars) < 2 {
			bars = append(bars, bars[0]+4*res)
		}
	}
	for bars[0] > first {
		l := bars[1] - bars[0]
		bars = append([]int{max(0, bars[0]-l)}, bars...)
	}
	lastLen := bars[len(bars)-1] - bars[len(bars)-2]
	lastBar := barOf(bars, last)
	for len(bars) <= lastBar+extra+1 {
		bars = append(bars, bars[len(bars)-1]+lastLen)
	}
	return bars
}

func barLength(num, den, res int) int {
	if num <= 0 || den <= 0 {
		return 4 * res
	}
	return max(1, int(math.Round(4*float64(res)*float64(num)/float64(den))))
}

// barOf is the bar a tick falls in: the last bar line at or before it.
func barOf(bars []int, tick int) int {
	return max(0, sort.Search(len(bars), func(i int) bool { return bars[i] > tick })-1)
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
