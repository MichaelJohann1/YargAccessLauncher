package chartfile

import (
	"cmp"
	"sort"
	"strings"
)

// Drum fills YARG makes itself when a drums chart has none: SongChart.CreateDrumActivationPhrases,
// ParseForActivationPhrases, ApplyDrumActivationPhrases and IsIdealDrumActivationNote (YARG.Core's
// SongChart.AutoGeneration). The made fills join the chart's own (drumFills), so they sound and are
// cut by Listen and Repeat exactly like charted ones.

// drumPhrases is what the fill maker reads from a drums track, by difficulty: star power and solo
// phrases (tick to end tick, as charted), and the Expert+ double kick ticks.
type drumPhrases struct {
	stars, solos map[string][]tickRange
	plusKicks    []int
}

// timed is a chart event's tick and time.
type timed struct {
	tick int
	time float64
}

// yargSearch is CollectionExtensions.BinarySearch: the index of a match, else the last index looked
// at (next to where the target would go), -1 for an empty list. c(i) compares item i to the target.
func yargSearch(n int, c func(i int) int) int {
	lo, hi, mid := 0, n-1, -1
	for lo <= hi {
		mid = (lo + hi) / 2
		r := c(mid)
		if r == 0 {
			return mid
		}
		if r > 0 {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return mid
}

// lowerBound is ChartEventExtensions.LowerBound (before = true): the first item equal to the
// target, else the last item before it; -1 when every item is after it or the list is empty.
func lowerBound(n int, c func(i int) int) int {
	i := yargSearch(n, c)
	if i < 0 {
		return i
	}
	for i-1 >= 0 && c(i-1) >= 0 {
		i--
	}
	if c(i) > 0 {
		i--
	}
	return i
}

// upperBound is ChartEventExtensions.UpperBound: the first item after the target (n when none);
// -1 for an empty list.
func upperBound(n int, c func(i int) int) int {
	i := yargSearch(n, c)
	if i < 0 {
		return i
	}
	for i < n && c(i) <= 0 {
		i++
	}
	return i
}

func byTick(list []timed, tick int) func(int) int {
	return func(i int) int { return cmp.Compare(list[i].tick, tick) }
}

func byTime(list []timed, t float64) func(int) int {
	return func(i int) int { return cmp.Compare(list[i].time, t) }
}

// timedPhrases is phrases sorted by tick: their start ticks and times, their end times and the
// last one's end tick (GetLastTick, 0 when there are none).
func (c *Chart) timedPhrases(rs []tickRange) ([]timed, []float64, int) {
	sorted := append([]tickRange(nil), rs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].a < sorted[j].a })
	starts, ends := make([]timed, len(sorted)), make([]float64, len(sorted))
	for i, r := range sorted {
		starts[i] = timed{r.a, c.Tempo.Time(r.a)}
		ends[i] = c.Tempo.Time(r.b)
	}
	lastEnd := 0
	if len(sorted) > 0 {
		lastEnd = sorted[len(sorted)-1].b
	}
	return starts, ends, lastEnd
}

// sectionEvents is the song's sections, as TextEvents.TryParseSectionEvent finds them: [section x]
// or [prc_x] with a name.
func (c *Chart) sectionEvents() []timed {
	var out []timed
	for _, e := range c.events {
		rest, ok := strings.CutPrefix(e.text, "section")
		if !ok {
			if rest, ok = strings.CutPrefix(e.text, "prc"); !ok {
				continue
			}
		}
		if strings.TrimSpace(strings.TrimLeft(rest, "_")) != "" {
			out = append(out, timed{e.tick, c.Tempo.Time(e.tick)})
		}
	}
	return out
}

// signatures is the time signatures, one per tick (the last one read at a tick wins, as the beat
// lines take it).
func (c *Chart) signatures() []Signature {
	var out []Signature
	for _, s := range c.Tempo.Signatures {
		if len(out) > 0 && out[len(out)-1].Tick == s.Tick {
			out[len(out)-1] = s
		} else {
			out = append(out, s)
		}
	}
	return out
}

// lastTick is SongChart.GetLastTick from the parts the launcher reads: the latest end of the last
// note of each part and difficulty, and of the drums phrases.
func (c *Chart) lastTick() int {
	last := 0
	for _, diffs := range c.notes {
		for _, notes := range diffs {
			if len(notes) > 0 {
				last = max(last, notes[len(notes)-1].EndTick)
			}
		}
	}
	for _, notes := range c.drums {
		if len(notes) > 0 {
			last = max(last, notes[len(notes)-1].Tick)
		}
	}
	for _, notes := range c.prokeys {
		if len(notes) > 0 {
			for _, e := range notes[len(notes)-1].EndTicks {
				last = max(last, e)
			}
		}
	}
	for _, m := range []map[string][]tickRange{c.drumFills, c.gen.stars, c.gen.solos} {
		for _, rs := range m {
			for _, r := range rs {
				last = max(last, r.b)
			}
		}
	}
	for _, rs := range c.drumLanes {
		for _, r := range rs {
			last = max(last, r.b)
		}
	}
	return last
}

// measureLines is the song's Measure beat lines: a .mid's BEAT track (note 12) when it has beat
// lines, else SyncTrack.GenerateBeatlines up to the last tick (a Measure line on each bar's first
// beat; in a time signature of one beat only on its first beat).
func (c *Chart) measureLines() []timed {
	var out []timed
	if c.beatTrack {
		for _, t := range c.beatMeasures {
			out = append(out, timed{t, c.Tempo.Time(t)})
		}
		return out
	}
	gen := func(s Signature, start, end int) {
		den := s.Den
		if den == 0 {
			den = 4
		}
		step := max(1, c.Tempo.Resolution*4/den)
		num := max(1, s.Num)
		for t, n := start, 0; t <= end; t, n = t+step, n+1 {
			if num == 1 && n < 1 || num != 1 && n%num == 0 {
				out = append(out, timed{t, c.Tempo.Time(t)})
			}
		}
	}
	sigs := c.signatures()
	cur := sigs[0]
	for _, next := range sigs[1:] {
		// GenerateBeatlines skips a signature that ends at tick 0 (the next one starts at tick 1).
		if cur.Tick > 0 || next.Tick-1 > 0 {
			gen(cur, cur.Tick, next.Tick-1)
			cur = next
		}
	}
	gen(cur, cur.Tick, c.lastTick()+1)
	return out
}

// padCount is a note's tick and its number of pads (YARG's note and its child notes).
type padCount struct{ tick, pads int }

// noteCounts is a drums difficulty's notes as YARG counts them for fills; Expert counts as Expert+
// (with the double kicks), the difficulty YARG reads first.
func (c *Chart) noteCounts(diff string) []padCount {
	notes := c.drums[diff]
	counts := map[int]int{}
	kick := map[int]bool{}
	for _, n := range notes {
		counts[n.Tick] = len(n.Hits)
		for _, h := range n.Hits {
			kick[n.Tick] = kick[n.Tick] || h.Lane == 0
		}
	}
	if diff == "expert" {
		for _, t := range c.gen.plusKicks {
			if !kick[t] {
				counts[t]++
				kick[t] = true
			}
		}
	}
	var out []padCount
	for _, t := range sortedKeys(counts) {
		out = append(out, padCount{t, counts[t]})
	}
	return out
}

// generateFills is CreateDrumActivationPhrases: when the hardest drums difficulty has no fills, fills
// are found on it and set on every difficulty.
func (c *Chart) generateFills() {
	parse := ""
	for i := len(Difficulties) - 1; i >= 0 && parse == ""; i-- {
		if len(c.drums[Difficulties[i]]) > 0 {
			parse = Difficulties[i]
		}
	}
	if parse == "" {
		return
	}
	made := c.parseFills(parse)
	if len(made) == 0 {
		return
	}
	for _, d := range Difficulties {
		if len(c.drums[d]) > 0 {
			c.applyFills(d, made)
		}
	}
}

// parseFills is ParseForActivationPhrases: none when the difficulty has fills or no star power. From
// the end of the first star power phrase it steps through the bars four at a time (two when four bars
// on are more than 10 s away), moving to the start of a section or time signature it passes. A bar
// line at least 2 s after the latest end of a star power phrase, solo or fill passed, with at least
// 16 notes (counting each pad of a chord) after it and up to four bars on, ends a fill from the bar
// before.
func (c *Chart) parseFills(diff string) []tickRange {
	if len(c.drumFills[diff]) > 0 {
		return nil
	}
	sp, spEnd, _ := c.timedPhrases(c.gen.stars[diff])
	if len(sp) == 0 {
		return nil
	}
	solos, soloEnd, lastSolo := c.timedPhrases(c.gen.solos[diff])
	sections := c.sectionEvents()
	var sigs []timed
	for _, s := range c.signatures() {
		sigs = append(sigs, timed{s.Tick, c.Tempo.Time(s.Tick)})
	}
	measures := c.measureLines()
	notes := c.noteCounts(diff)

	last := spEnd[0]
	spIdx := 0
	count := len(measures)
	m := lowerBound(count, byTime(measures, last))
	secIdx := lowerBound(len(sections), byTime(sections, last))
	sigIdx := lowerBound(len(sigs), byTime(sigs, last))
	soloIdx := lowerBound(len(solos), byTime(solos, last))
	var out []tickRange
	for m < count-4 {
		step := 4
		if measures[m+step].time-last > 10.0 {
			step = 2
		}
		m += step
		bar := measures[m]
		if s := lowerBound(len(sections), byTick(sections, bar.tick)); s > secIdx {
			secIdx = s
			m = lowerBound(count, byTick(measures, sections[secIdx].tick))
		} else if s := lowerBound(len(sigs), byTick(sigs, bar.tick)); s > sigIdx {
			sigIdx = s
			m = lowerBound(count, byTick(measures, sigs[sigIdx].tick))
		}
		if m < 0 {
			return out // YARG throws here (a section or signature before the first bar line)
		}
		bar = measures[m]
		if s := lowerBound(len(sp), byTick(sp, bar.tick)); s > spIdx {
			spIdx = s
			last = max(spEnd[spIdx], last)
		}
		if len(solos) > 0 && bar.tick < lastSolo {
			if s := lowerBound(len(solos), byTick(solos, bar.tick)); s > soloIdx {
				soloIdx = s
				last = max(soloEnd[soloIdx], last)
			}
		}
		if bar.time-last < 2.0 {
			continue
		}
		upTo := measures[min(m+4, count-1)].tick
		n := 0
		after := upperBound(len(notes), func(i int) int { return cmp.Compare(notes[i].tick, bar.tick) })
		for j := after; n < 16 && j >= 0 && j < len(notes) && notes[j].tick <= upTo; j++ {
			n += notes[j].pads
		}
		if n >= 16 {
			if m < 1 {
				return out // YARG throws here (no bar line before)
			}
			last = bar.time
			out = append(out, tickRange{measures[m-1].tick, bar.tick})
		}
	}
	return out
}

// applyFills is ApplyDrumActivationPhrases: each fill (up to the difficulty's last note) ends on its
// activator note, the note on its end tick, or when that is missing or not ideal the first ideal note
// (else the first note) within an eighth of the fill's length either side.
func (c *Chart) applyFills(diff string, made []tickRange) {
	notes := c.drums[diff]
	last := notes[len(notes)-1].Tick
	at := func(tick int) int {
		i := upperBound(len(notes), func(i int) int { return cmp.Compare(notes[i].Tick, tick) })
		if i >= len(notes) {
			return -1
		}
		return i
	}
	for _, f := range made {
		end := f.b
		if end > last {
			break
		}
		pick := at(end - 1)
		search := true
		if pick >= 0 && notes[pick].Tick == end {
			search = !c.idealActivator(notes[pick], diff)
		} else {
			pick = -1
		}
		if search {
			span := (end - f.a) / 8
			for j := at(end - span - 1); j >= 0 && j < len(notes) && notes[j].Tick <= end+span; j++ {
				if pick < 0 {
					pick = j
				}
				if c.idealActivator(notes[j], diff) {
					pick = j
					break
				}
			}
		}
		if pick >= 0 {
			c.drumFills[diff] = append(c.drumFills[diff], tickRange{f.a, notes[pick].Tick})
		}
	}
}

// idealActivator is IsIdealDrumActivationNote: any note on Easy; from Medium a note with a crash (the
// green cymbal on pro drums, the green pad on four-lane, orange on five-lane) and a kick or snare.
func (c *Chart) idealActivator(n *DrumNote, diff string) bool {
	if diff == "easy" {
		return true
	}
	crashPad := 7
	switch c.DrumMode {
	case FIVE, FOUR:
		crashPad = 4
	}
	crash, kickOrSnare := false, false
	for _, h := range n.Hits {
		p := Pad(h, c.DrumMode)
		crash = crash || p == crashPad
		kickOrSnare = kickOrSnare || p == 0 || p == 1
	}
	return crash && kickOrSnare
}
