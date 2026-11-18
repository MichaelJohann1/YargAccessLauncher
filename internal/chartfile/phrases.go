package chartfile

import (
	"math"
	"strings"
)

// The drum lanes, fills and big rock endings, as YARG reads them (YARG.Core's MidReader, ChartReader
// and MoonSongLoader).

type textEvent struct {
	tick int
	text string
}

// laneRange is a roll (tremolo) or swell (trill) lane phrase.
type laneRange struct {
	a, b  int // tick and end tick
	swell bool
}

// Phrase is a phrase on the song timeline.
type Phrase struct {
	Tick, EndTick int
	Time, End     float64
}

// normalizeText is YARG's TextEvents.NormalizeTextEvent: the text without its brackets and spaces.
func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// inInclusive is YARG's IsEventInPhrase with the end included (big rock endings).
func inInclusive(rs []tickRange, tick int) bool {
	for _, r := range rs {
		if r.a <= tick && tick <= r.b {
			return true
		}
	}
	return false
}

// codaRanges is MidReader.GetCodaRanges: the [midcoda] .. [midcoda_end] ranges and the range from
// [coda] to the end of the song. Unbalanced events drop them all.
func codaRanges(events []textEvent) []tickRange {
	const open = math.MaxInt
	var out []tickRange
	starts := 0
	for _, e := range events {
		switch e.text {
		case "midcoda":
			if len(out) > 0 && out[len(out)-1].b == open {
				return nil
			}
			starts++
			out = append(out, tickRange{e.tick, open})
		case "midcoda_end":
			if starts != len(out) || len(out) == 0 {
				return nil
			}
			out[len(out)-1].b = e.tick
		case "coda":
			if len(out) > 0 && out[len(out)-1].b == open {
				out = nil
			}
			return append(out, tickRange{e.tick, open})
		}
	}
	return out
}

// lastCoda is the tick of the song's last [coda] event, or -1.
func lastCoda(events []textEvent) int {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].text == "coda" {
			return events[i].tick
		}
	}
	return -1
}

// splitFills puts the drum fills in a coda among the big rock endings, as MidReader's
// ReplaceDrumFillDuringCoda and MoonSongLoader's ValidateDrumsPhrase do, and keeps the rest as fills.
// A fill marked on more than one of MIDI notes 120 to 124 counts once.
func (c *Chart) splitFills(fills map[string][]tickRange) {
	ranges, coda := codaRanges(c.events), lastCoda(c.events)
	for diff, list := range fills {
		seen := map[tickRange]bool{}
		for _, f := range list {
			if seen[f] {
				continue
			}
			seen[f] = true
			inCoda := coda >= 0 && f.a >= coda
			for _, r := range ranges {
				inCoda = inCoda || r.a <= f.a && f.a <= r.b
			}
			if inCoda {
				if c.bre["drums"] == nil {
					c.bre["drums"] = map[string][]tickRange{}
				}
				c.bre["drums"][diff] = append(c.bre["drums"][diff], f)
			} else {
				c.drumFills[diff] = append(c.drumFills[diff], f)
			}
		}
	}
}

// DrumFills is a drums difficulty's fills (YARG's DrumFill phrases, the star power activation
// phrases) with their times, in order.
func (c *Chart) DrumFills(difficulty string) []Phrase {
	var out []Phrase
	for _, f := range c.drumFills[difficulty] {
		out = append(out, Phrase{f.a, f.b, c.Tempo.Time(f.a), c.Tempo.Time(f.b)})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Tick < out[j-1].Tick; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Pad is a hit's YARG pad number: four-lane and pro drums kick 0, red 1, yellow 2, blue 3, green 4,
// yellow cymbal 5, blue cymbal 6, green cymbal 7 (cymbals only on pro drums); five-lane drums kick 0,
// red 1, yellow 2, blue 3, orange 4, green 5.
func Pad(h DrumHit, mode string) int {
	if mode == PRO && h.Cymbal && h.Lane >= 2 && h.Lane <= 4 {
		return h.Lane + 3
	}
	return h.Lane
}

// otherPad is FourLaneDrumPad.OtherPadOfSameColor: a tom's cymbal and a cymbal's tom, or -1.
func otherPad(pad int) int {
	switch pad {
	case 2, 3, 4:
		return pad + 3
	case 5, 6, 7:
		return pad - 3
	}
	return -1
}

// applyLanes is MoonSongLoader.DrumsFinalPass: in each lane phrase, the notes of the lane's pad (a
// roll) or two pads (a swell) are marked, the first as the lane start and the last as its end.
func applyLanes(notes []*DrumNote, lanes []laneRange, mode string) {
	for _, n := range notes {
		for k := range n.Hits {
			h := &n.Hits[k]
			h.InLane, h.Swell, h.LaneStart, h.LaneEnd = false, false, false, false
		}
	}
	if len(lanes) == 0 || len(notes) == 0 {
		return
	}
	sorted := append([]laneRange(nil), lanes...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].a < sorted[j-1].a; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	fourLane := mode != FIVE
	next := 0
	for i, ph := range sorted {
		// GetNotesInLanePhrase: a note on the end tick belongs to the next lane when one starts there.
		endsAtLane := false
		for _, o := range sorted[i:] {
			if o.a > ph.b {
				break
			}
			if o.a == ph.b {
				endsAtLane = true
				break
			}
		}
		var in []int
		for j := next; j < len(notes); j++ {
			t := notes[j].Tick
			if t < ph.a {
				continue
			}
			if endsAtLane && t == ph.b || t > ph.b {
				break
			}
			next = j + 1
			in = append(in, j)
		}
		var marked []*DrumHit
		if ph.swell {
			marked = trillNotes(notes, in, mode, fourLane)
		} else {
			marked = tremoloNotes(notes, in, mode, fourLane)
		}
		for _, h := range marked {
			h.InLane, h.Swell = true, ph.swell
		}
		if len(marked) > 0 {
			marked[0].LaneStart = true
			marked[len(marked)-1].LaneEnd = true
		}
	}
}

// tremoloNotes is GetDrumTremoloNotes and GetDrumTremoloPad: a roll is one pad, found from the first
// note (or the first pad of it that comes again); its notes run until the pad's other-colour pad.
func tremoloNotes(notes []*DrumNote, in []int, mode string, fourLane bool) []*DrumHit {
	if len(in) < 2 {
		return nil
	}
	var pads []int
	for _, h := range notes[in[0]].Hits {
		if p := Pad(h, mode); p != 0 {
			pads = append(pads, p)
		}
	}
	pad := -1
	switch {
	case len(pads) == 1:
		other := -1
		if fourLane {
			other = otherPad(pads[0])
		}
	search:
		for _, j := range in[1:] {
			for _, h := range notes[j].Hits {
				p := Pad(h, mode)
				if other >= 0 && p == other {
					break search
				}
				if p == pads[0] {
					pad = pads[0]
					break search
				}
			}
		}
	case len(pads) > 1:
	found:
		for _, j := range in[1:] {
			var hit []int
			for _, h := range notes[j].Hits {
				p := Pad(h, mode)
				if containsInt(pads, p) {
					hit = append(hit, p)
				} else if fourLane {
					if o := otherPad(p); o >= 0 {
						for k, q := range pads {
							if q == o {
								pads = append(pads[:k:k], pads[k+1:]...)
								break
							}
						}
					}
				}
			}
			if len(hit) == 1 {
				pad = hit[0]
				break found
			}
		}
	}
	if pad < 0 {
		return nil
	}
	other := -1
	if fourLane {
		other = otherPad(pad)
	}
	var out []*DrumHit
	for _, j := range in {
		n := notes[j]
		for k := range n.Hits {
			p := Pad(n.Hits[k], mode)
			if other >= 0 && p == other {
				return out
			}
			if p == pad {
				out = append(out, &n.Hits[k])
			}
		}
	}
	return out
}

// trillNotes is GetDrumTrillNotes and GetDrumTrillPads: a swell is two pads in turn, the first note's
// pad and the next different one; it runs while the notes keep to the two pads and alternate.
func trillNotes(notes []*DrumNote, in []int, mode string, fourLane bool) []*DrumHit {
	if len(in) < 3 {
		return nil
	}
	pad1 := -1
	for _, h := range notes[in[0]].Hits {
		if p := Pad(h, mode); p != 0 {
			if pad1 >= 0 {
				return nil
			}
			pad1 = p
		}
	}
	if pad1 < 0 {
		return nil
	}
	pad2 := -1
	for j := in[1]; j < len(notes) && pad2 < 0; j++ {
		if j+1 >= len(notes) {
			return nil
		}
		n := notes[j]
		if len(n.Hits) == 1 && Pad(n.Hits[0], mode) == 0 {
			continue
		}
		for _, h := range n.Hits {
			p := Pad(h, mode)
			if p == 0 {
				continue
			}
			if pad2 >= 0 || p == pad1 || fourLane && p == otherPad(pad1) {
				return nil
			}
			pad2 = p
		}
	}
	if pad2 < 0 {
		return nil
	}
	ok := false
check:
	for _, j := range in[2:] {
		n := notes[j]
		if len(n.Hits) == 1 && Pad(n.Hits[0], mode) == 0 {
			continue
		}
		for _, h := range n.Hits {
			if p := Pad(h, mode); p != 0 && p != pad1 {
				return nil
			}
		}
		ok = true
		break check
	}
	if !ok {
		return nil
	}
	var out []*DrumHit
	for _, j := range in {
		n := notes[j]
		for _, h := range n.Hits {
			p := Pad(h, mode)
			if p == 0 {
				continue
			}
			if p != pad1 && p != pad2 {
				return out
			}
			if len(out) > 0 && p == Pad(*out[len(out)-1], mode) {
				return out
			}
		}
		for k := range n.Hits {
			if Pad(n.Hits[k], mode) != 0 {
				out = append(out, &n.Hits[k])
				break
			}
		}
	}
	return out
}

// setActivators is InstrumentDifficultyExtensions.SetDrumActivationFlags with YARG's default
// (RightmostNote): the first note at or after each fill's end, or the last note, activates star power.
func (c *Chart) setActivators(notes []*DrumNote, difficulty string) {
	for _, n := range notes {
		n.Activator = false
	}
	i := 0
	for _, f := range c.DrumFills(difficulty) {
		for ; i < len(notes); i++ {
			if notes[i].Time >= f.End || i == len(notes)-1 {
				notes[i].Activator = true
				break
			}
		}
	}
}
