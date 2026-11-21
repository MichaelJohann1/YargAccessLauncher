// Package chartfile reads the five-fret, drums and pro keys parts of a chart (notes.chart or
// notes.mid) for the sound preview.
//
// It follows YARG.Core (its Moonscraper readers) where it matters for how a note sounds:
//   - natural HOPO: a single note, not the same fret as the note before (or that note was a chord),
//     no further than the HOPO threshold after it. The threshold is a third of a beat: plus 1 tick in
//     a .mid, plus resolution / 192 in a .chart, or song.ini's hopo_frequency / eighthnote_hopo.
//   - .chart: N 5 flips a note between strum and HOPO, N 6 makes it a tap, N 7 is the open note.
//   - .mid: per difficulty the frets start at 60 / 72 / 84 / 96, the note below is an open note when
//     the track has ENHANCED_OPENS, +5 forces HOPO, +6 forces strum, 104 is a tap, 116 star power;
//     Phase Shift SysEx events mark open notes and taps. Sustains shorter than a third of a beat are cut.
//   - the key comes from the vocals (and harmonies) and pro keys parts, as the mod's KeyDetect does.
//   - drums: lane 0 is the kick, 1..5 red, yellow, blue, green (orange on five-lane) and five-lane green.
//     The chart's own kind is used: five-lane when it has fifth-lane notes, pro when song.ini says
//     pro_drums or the chart marks cymbals (.chart N 66-68) or toms (.mid 110-112), else plain four-lane.
//     In a .mid yellow, blue and green are cymbals unless a tom marker covers them; in a .chart they are
//     toms unless a cymbal marker does. Ghost and accent: .chart N 39-44 / 33-38, .mid velocity 1 / 127
//     when the track has [ENABLE_CHART_DYNAMICS]. The Expert+ double kick is left out.
//   - drum fills: .chart S 64, .mid 120-124 (not in a coda). A chart with none gets YARG's own
//     (SongChart.CreateDrumActivationPhrases, fillgen.go), which do count the Expert+ double kick.
//   - pro keys (.mid only, PART REAL_KEYS_E/M/H/X): MIDI 48-72 are the 25 keys (key 0 = C3), 116 star
//     power, 126 glissando. Sustains shorter than the cutoff are cut, as for the other parts.
package chartfile

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Note kinds.
const (
	STRUM = "strum"
	HOPO  = "hopo"
	TAP   = "tap"
)

// Drum kinds.
const (
	FOUR = "four"
	PRO  = "pro"
	FIVE = "five"
)

// Difficulties in order.
var Difficulties = []string{"easy", "medium", "hard", "expert"}

// GuitarParts is the five-fret parts in the order Parts lists them.
var GuitarParts = []string{"guitar", "guitarcoop", "rhythm", "bass", "keys"}

// ChartSections is each five-fret part's .chart section suffix.
var ChartSections = map[string]string{
	"guitar": "Single", "guitarcoop": "DoubleGuitar", "rhythm": "DoubleRhythm",
	"bass": "DoubleBass", "keys": "Keyboard",
}

// ChartDiff is each difficulty's .chart section prefix.
var ChartDiff = map[string]string{"easy": "Easy", "medium": "Medium", "hard": "Hard", "expert": "Expert"}

// MidiTracks is each five-fret part's .mid track names, the first found wins.
var MidiTracks = map[string][]string{
	"guitar": {"PART GUITAR", "T1 GEMS"}, "guitarcoop": {"PART GUITAR COOP"},
	"rhythm": {"PART RHYTHM"}, "bass": {"PART BASS"}, "keys": {"PART KEYS"},
}

// MidiBase is each difficulty's first .mid note (green fret, or the kick on drums).
var MidiBase = map[string]int{"easy": 60, "medium": 72, "hard": 84, "expert": 96}

// MidiDiffIndex is each difficulty's number in Phase Shift SysEx events.
var MidiDiffIndex = map[string]int{"easy": 0, "medium": 1, "hard": 2, "expert": 3}

// ProkeysTracks is each difficulty's pro keys track.
var ProkeysTracks = map[string]string{"easy": "PART REAL_KEYS_E", "medium": "PART REAL_KEYS_M",
	"hard": "PART REAL_KEYS_H", "expert": "PART REAL_KEYS_X"}

// ChartError is a chart that cannot be read.
type ChartError struct{ Msg string }

func (e *ChartError) Error() string { return e.Msg }

// Note is one chord or single note. Frets: 0 = open, 1..5 = Green..Orange. Times in seconds.
type Note struct {
	Tick      int
	Frets     []int
	EndTick   int
	Kind      string
	Star      bool
	StarStart bool // the first note of a star power phrase
	StarEnd   bool // the last note of a star power phrase
	BRE       bool // in a big rock ending (coda)
	Time      float64
	End       float64
	Safe      float64
}

// DrumHit is one pad of a drum note.
type DrumHit struct {
	Lane   int
	Cymbal bool
	Volume float64
	// Roll lanes, as YARG's DrumsFinalPass marks them: InLane is a note of a roll (tremolo) or swell
	// (trill) lane, Swell a swell's, LaneStart and LaneEnd the lane's first and last note.
	InLane, Swell, LaneStart, LaneEnd bool
}

// NewDrumHit is a hit at the default volume, 0.8.
func NewDrumHit(lane int, cymbal bool) DrumHit {
	return DrumHit{Lane: lane, Cymbal: cymbal, Volume: 0.8}
}

// DrumNote is the pads hit at one moment.
type DrumNote struct {
	Tick      int
	Hits      []DrumHit
	Star      bool
	StarStart bool
	StarEnd   bool
	BRE       bool
	Activator bool // the note that activates star power at the end of a fill
	Time      float64
}

// KeyNote is the keys pressed at one moment (pro keys): key 0..24 = C3..C5, each with its own end.
type KeyNote struct {
	Tick      int
	Keys      []int
	EndTicks  []int
	Star      bool
	StarStart bool
	StarEnd   bool
	BRE       bool
	Gliss     bool
	Time      float64
	End       float64
	Safe      float64
	Safes     []float64
}

// Tempo is a tempo change: microseconds per beat from a tick.
type Tempo struct{ Tick, US int }

// Signature is a time signature from a tick.
type Signature struct{ Tick, Num, Den int }

// TempoMap turns ticks into seconds.
type TempoMap struct {
	Resolution int
	Tempos     []Tempo
	Signatures []Signature
	// Bars is the tick of each bar line (the song's Measure beat lines), set when a chart is read.
	Bars     []int
	ticks    []int
	secs     []float64
	sigTicks []int
}

// Finish sorts the changes, adds 120 BPM and 4/4 at tick 0 when missing, and builds the lookup.
func (m *TempoMap) Finish() {
	sort.Slice(m.Tempos, func(i, j int) bool {
		a, b := m.Tempos[i], m.Tempos[j]
		return a.Tick < b.Tick || a.Tick == b.Tick && a.US < b.US
	})
	if len(m.Tempos) == 0 || m.Tempos[0].Tick != 0 {
		m.Tempos = append([]Tempo{{0, 500000}}, m.Tempos...)
	}
	sort.Slice(m.Signatures, func(i, j int) bool {
		a, b := m.Signatures[i], m.Signatures[j]
		if a.Tick != b.Tick {
			return a.Tick < b.Tick
		}
		if a.Num != b.Num {
			return a.Num < b.Num
		}
		return a.Den < b.Den
	})
	if len(m.Signatures) == 0 || m.Signatures[0].Tick != 0 {
		m.Signatures = append([]Signature{{0, 4, 4}}, m.Signatures...)
	}
	m.ticks, m.secs = m.ticks[:0], m.secs[:0]
	t := 0.0
	prevTick, prevUS := 0, m.Tempos[0].US
	res := float64(m.Resolution)
	for _, tp := range m.Tempos {
		t += float64(tp.Tick-prevTick) * float64(prevUS) / 1e6 / res
		m.ticks = append(m.ticks, tp.Tick)
		m.secs = append(m.secs, t)
		prevTick, prevUS = tp.Tick, tp.US
	}
	m.sigTicks = m.sigTicks[:0]
	for _, s := range m.Signatures {
		m.sigTicks = append(m.sigTicks, s.Tick)
	}
}

// bisectRightInt is Python's bisect_right.
func bisectRightInt(a []int, x int) int {
	return sort.Search(len(a), func(i int) bool { return a[i] > x })
}

func bisectRightFloat(a []float64, x float64) int {
	return sort.Search(len(a), func(i int) bool { return a[i] > x })
}

// Time is a tick's time in seconds.
func (m *TempoMap) Time(tick int) float64 {
	i := max(0, bisectRightInt(m.ticks, tick)-1)
	return m.secs[i] + float64(tick-m.ticks[i])*float64(m.Tempos[i].US)/1e6/float64(m.Resolution)
}

// TickAt is the tick at a time, truncated toward zero.
func (m *TempoMap) TickAt(seconds float64) int {
	i := max(0, bisectRightFloat(m.secs, seconds)-1)
	return int(math.Trunc(float64(m.ticks[i]) + (seconds-m.secs[i])*1e6*float64(m.Resolution)/float64(m.Tempos[i].US)))
}

// BeatLength is the length of one beat (the time signature's denominator note) at a time, in seconds.
func (m *TempoMap) BeatLength(seconds float64) float64 {
	tick := m.TickAt(seconds)
	i := max(0, bisectRightInt(m.ticks, tick)-1)
	j := max(0, bisectRightInt(m.sigTicks, tick)-1)
	den := m.Signatures[j].Den
	if den == 0 {
		den = 4
	}
	return pmax(0.05, float64(m.Tempos[i].US)/1e6*4/float64(den))
}

// Beat is one beat line.
type Beat struct {
	Time  float64
	First bool // the first beat of a bar
	Tick  int
}

// Beats is the beat lines up to endSeconds: one line per beat of the time signature, like YARG's
// Measure and Strong beat lines.
func (m *TempoMap) Beats(endSeconds float64) []Beat {
	var out []Beat
	endTick := m.TickAt(endSeconds)
	sigs := m.Signatures
	for k, s := range sigs {
		stop := endTick + 1
		if k+1 < len(sigs) {
			stop = sigs[k+1].Tick
		}
		den := s.Den
		if den == 0 {
			den = 4
		}
		step := max(1, floorDiv(m.Resolution*4, den))
		num := max(1, s.Num)
		n := 0
		for t := s.Tick; t < stop && t <= endTick; t += step {
			out = append(out, Beat{Time: m.Time(t), First: pyMod(n, num) == 0, Tick: t})
			n++
		}
	}
	return out
}

// Part is one playable part and the difficulties it has notes on.
type Part struct {
	Name         string
	Difficulties []string
}

// Key is a detected key: tonic 0..11 (C = 0) and whether it is minor.
type Key struct {
	Tonic int
	Minor bool
}

// Chart is a read chart.
type Chart struct {
	Tempo         *TempoMap
	IsMidi        bool
	HopoThreshold int
	// Key is nil when the chart has too few pitched notes to tell (and always for a .chart).
	Key      *Key
	DrumMode string

	notes   map[string]map[string][]*Note
	drums   map[string][]*DrumNote
	prokeys map[string][]*KeyNote

	events    []textEvent                       // the song's global text events, in tick order
	bre       map[string]map[string][]tickRange // big rock ending phrases: part -> difficulty
	drumLanes map[string][]laneRange            // roll and swell lanes by difficulty
	drumFills map[string][]tickRange            // drum fills (not the ones in a coda) by difficulty

	gen          drumPhrases // what YARG makes fills from when the chart has none (fillgen.go)
	beatTrack    bool        // a .mid BEAT track with beat lines: its bars replace the generated ones
	beatMeasures []int       // the BEAT track's Measure lines (note 12), in tick order
}

func newChart(tempo *TempoMap, isMidi bool, threshold int) *Chart {
	return &Chart{Tempo: tempo, IsMidi: isMidi, HopoThreshold: threshold, DrumMode: FOUR,
		notes: map[string]map[string][]*Note{}, drums: map[string][]*DrumNote{},
		prokeys: map[string][]*KeyNote{}, bre: map[string]map[string][]tickRange{},
		drumLanes: map[string][]laneRange{}, drumFills: map[string][]tickRange{},
		gen: drumPhrases{stars: map[string][]tickRange{}, solos: map[string][]tickRange{}}}
}

// Parts is each part with notes: the five-fret parts in GuitarParts order, then "drums", then
// "prokeys", each with its difficulties in order.
func (c *Chart) Parts() []Part {
	var out []Part
	for _, p := range GuitarParts {
		diffs, ok := c.notes[p]
		if !ok {
			continue
		}
		var have []string
		for _, d := range Difficulties {
			if len(diffs[d]) > 0 {
				have = append(have, d)
			}
		}
		if len(have) > 0 {
			out = append(out, Part{p, have})
		}
	}
	var drums, keys []string
	for _, d := range Difficulties {
		if len(c.drums[d]) > 0 {
			drums = append(drums, d)
		}
		if len(c.prokeys[d]) > 0 {
			keys = append(keys, d)
		}
	}
	if drums != nil {
		out = append(out, Part{"drums", drums})
	}
	if keys != nil {
		out = append(out, Part{"prokeys", keys})
	}
	return out
}

// PartsMap is Parts as a map (no order).
func (c *Chart) PartsMap() map[string][]string {
	out := map[string][]string{}
	for _, p := range c.Parts() {
		out[p.Name] = p.Difficulties
	}
	return out
}

// ProkeysNotes is a pro keys difficulty's notes with their times filled in. A key's safe time is a
// quarter beat before its end (when the hold is at least that long), when YARG has scored the hold.
func (c *Chart) ProkeysNotes(difficulty string) []*KeyNote {
	notes := c.prokeys[difficulty]
	burst := floorDiv(c.Tempo.Resolution, 4)
	for _, n := range notes {
		n.Time = c.Tempo.Time(n.Tick)
		n.Safes = make([]float64, len(n.EndTicks))
		maxEnd := n.EndTicks[0]
		for k, e := range n.EndTicks {
			if e-n.Tick >= burst {
				n.Safes[k] = pmax(n.Time, c.Tempo.Time(e-burst))
			} else {
				n.Safes[k] = c.Tempo.Time(e)
			}
			if e > maxEnd {
				maxEnd = e
			}
		}
		n.End = c.Tempo.Time(maxEnd)
		n.BRE = inInclusive(c.bre["prokeys"][difficulty], n.Tick)
		n.Safe = n.Time
		if len(n.Safes) > 0 {
			n.Safe = n.Safes[0]
			for _, s := range n.Safes[1:] {
				n.Safe = pmax(n.Safe, s)
			}
		}
	}
	return notes
}

// DrumNotes is a drums difficulty's notes with their times filled in.
func (c *Chart) DrumNotes(difficulty string) []*DrumNote {
	notes := c.drums[difficulty]
	for _, n := range notes {
		n.Time = c.Tempo.Time(n.Tick)
		n.BRE = inInclusive(c.bre["drums"][difficulty], n.Tick)
	}
	applyLanes(notes, c.drumLanes[difficulty], c.DrumMode)
	c.setActivators(notes, difficulty)
	return notes
}

// Notes is a five-fret part's notes on one difficulty with their times filled in. Safe is a quarter
// beat before a sustain's end (YARG has finished scoring the hold by then), else the end.
func (c *Chart) Notes(part, difficulty string) []*Note {
	notes := c.notes[part][difficulty]
	burst := floorDiv(c.Tempo.Resolution, 4)
	for _, n := range notes {
		n.Time = c.Tempo.Time(n.Tick)
		n.End = c.Tempo.Time(n.EndTick)
		n.BRE = inInclusive(c.bre[part][difficulty], n.Tick)
		if n.EndTick-n.Tick >= burst {
			n.Safe = pmax(n.Time, c.Tempo.Time(n.EndTick-burst))
		} else {
			n.Safe = n.End
		}
	}
	return notes
}

// hopoThreshold follows YARG.Core IniSubEntry, then the Moonscraper readers.
func hopoThreshold(resolution int, ini map[string]string, isMidi bool) int {
	v, present := ini["hopo_frequency"]
	if freq, ok := pyInt(v, present); ok && freq > 0 {
		if isMidi {
			return freq + 1
		}
		return freq
	}
	e := strings.ToLower(pyStrip(ini["eighthnote_hopo"]))
	div := 3
	if e == "1" || e == "true" {
		div = 2
	}
	t := floorDiv(resolution, div)
	if isMidi {
		return t + 1
	}
	return t + int(math.Trunc(float64(resolution)/192))
}

func isTrue(text string) bool {
	t := strings.ToLower(pyStrip(text))
	return t == "1" || t == "true"
}

// classify sets strum, HOPO or tap for each note. forced: tick -> "flip" (.chart N 5), "hopo" or
// "strum" (.mid), "tap".
func classify(groups []*Note, threshold int, forced map[int]string) {
	var prev *Note
	for _, n := range groups {
		f := forced[n.Tick]
		natural := prev != nil && len(n.Frets) == 1 &&
			(len(prev.Frets) > 1 || prev.Frets[0] != n.Frets[0]) &&
			n.Tick-prev.Tick <= threshold
		switch {
		case f == TAP:
			n.Kind = TAP
		case f == "flip":
			if natural {
				n.Kind = STRUM
			} else {
				n.Kind = HOPO
			}
		case f == "hopo":
			n.Kind = HOPO
		case f == "strum":
			n.Kind = STRUM
		default:
			if natural {
				n.Kind = HOPO
			} else {
				n.Kind = STRUM
			}
		}
		prev = n
	}
}

type tickRange struct{ a, b int }

// markStar marks the notes in star power phrases, as YARG's MoonSongLoader: a note is in a phrase
// from its tick up to (not including) its end; StarStart is a phrase's first note and StarEnd its last.
func markStar(ticks []int, phrases []tickRange, set func(i int, star, start, end bool)) {
	phraseOf := make([]int, len(ticks))
	for i, tick := range ticks {
		phraseOf[i] = -1
		for k, p := range phrases {
			if p.a <= tick && tick < p.b {
				phraseOf[i] = k
				break
			}
		}
	}
	for i := range ticks {
		ph := phraseOf[i]
		star := ph >= 0
		start := star && (i == 0 || phraseOf[i-1] != ph)
		end := star && (i+1 == len(ticks) || phraseOf[i+1] != ph)
		set(i, star, start, end)
	}
}

func markNotes(groups []*Note, phrases []tickRange) {
	ticks := make([]int, len(groups))
	for i, n := range groups {
		ticks[i] = n.Tick
	}
	markStar(ticks, phrases, func(i int, s, st, e bool) { groups[i].Star, groups[i].StarStart, groups[i].StarEnd = s, st, e })
}

func markDrums(notes []*DrumNote, phrases []tickRange) {
	ticks := make([]int, len(notes))
	for i, n := range notes {
		ticks[i] = n.Tick
	}
	markStar(ticks, phrases, func(i int, s, st, e bool) { notes[i].Star, notes[i].StarStart, notes[i].StarEnd = s, st, e })
}

func markKeys(notes []*KeyNote, phrases []tickRange) {
	ticks := make([]int, len(notes))
	for i, n := range notes {
		ticks[i] = n.Tick
	}
	markStar(ticks, phrases, func(i int, s, st, e bool) { notes[i].Star, notes[i].StarStart, notes[i].StarEnd = s, st, e })
}

// ws is Python's \s for str patterns.
const ws = `[\pZ\t\n\v\f\r\x{1c}-\x{1f}\x{85}]`

var (
	syncRe  = regexp.MustCompile(strings.ReplaceAll(`^(\d+)WS*=WS*(\w+)WS+(\d+)(?:WS+(\d+))?`, "WS", ws))
	eventRe = regexp.MustCompile(strings.ReplaceAll(`^(\d+)WS*=WS*EWS+(.*)$`, "WS", ws))
	trackRe = regexp.MustCompile(strings.ReplaceAll(`^(\d+)WS*=WS*([NS])WS+(\d+)WS+(\d+)`, "WS", ws))
)

// atoi parses a run of ASCII digits; values past 2^62 are clamped (Python's ints have no limit).
func atoi(s string) int {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v > 1<<62 {
		return 1 << 62
	}
	return int(v)
}

// pow2 is 2**e for a time signature denominator, capped at 2^62 (any larger acts the same: a beat
// step of one tick and the 0.05 s minimum beat length).
func pow2(e int) int {
	if e > 62 {
		e = 62
	}
	return 1 << e
}

// ReadChart reads a .chart file's text.
func ReadChart(text string, ini map[string]string) *Chart {
	sections := map[string][]string{}
	name := ""
	for _, line := range pySplitLines(text) {
		line = pyStrip(line)
		if len(line) >= 2 && strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name = line[1 : len(line)-1]
			sections[name] = []string{}
		} else if name != "" && line != "{" && line != "}" && line != "" {
			sections[name] = append(sections[name], line)
		}
	}
	res := 192
	for _, line := range sections["Song"] {
		k, v, _ := strings.Cut(line, "=")
		if strings.ToLower(pyStrip(k)) == "resolution" {
			if r, ok := pyInt(strings.Trim(pyStrip(v), `"`), true); ok && r != 0 {
				res = r
			} else {
				res = 192
			}
		}
	}
	tempo := &TempoMap{Resolution: res}
	for _, line := range sections["SyncTrack"] {
		m := syncRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tick, kind, a := atoi(m[1]), m[2], atoi(m[3])
		if kind == "B" && a > 0 {
			tempo.Tempos = append(tempo.Tempos, Tempo{tick, int(math.RoundToEven(60e9 / float64(a)))})
		} else if kind == "TS" {
			den := 4
			if m[4] != "" {
				den = pow2(atoi(m[4]))
			}
			tempo.Signatures = append(tempo.Signatures, Signature{tick, a, den})
		}
	}
	tempo.Finish()
	chart := newChart(tempo, false, hopoThreshold(res, ini, false))
	for _, line := range sections["Events"] {
		if m := eventRe.FindStringSubmatch(line); m != nil {
			chart.events = append(chart.events, textEvent{atoi(m[1]), normalizeText(strings.Trim(pyStrip(m[2]), `"`))})
		}
	}
	sort.SliceStable(chart.events, func(i, j int) bool { return chart.events[i].tick < chart.events[j].tick })
	for _, part := range GuitarParts {
		for _, diff := range Difficulties {
			lines := sections[ChartDiff[diff]+ChartSections[part]]
			if len(lines) > 0 {
				if chart.notes[part] == nil {
					chart.notes[part] = map[string][]*Note{}
				}
				chart.notes[part][diff] = chartTrack(lines, chart.HopoThreshold)
			}
		}
	}
	raw := map[string][]*DrumNote{}
	fills := map[string][]tickRange{}
	five, marked, any := false, false, false
	for _, diff := range Difficulties {
		lines := sections[ChartDiff[diff]+"Drums"]
		if len(lines) == 0 {
			continue
		}
		any = true
		notes, cym, f, lanes, ex := chartDrums(lines)
		raw[diff] = notes
		chart.gen.stars[diff], chart.gen.solos[diff] = ex.stars, ex.solos
		if diff == "expert" {
			chart.gen.plusKicks = ex.plusKicks
		}
		fills[diff] = f
		chart.drumLanes[diff] = lanes
		marked = marked || cym
		for _, n := range notes {
			for _, h := range n.Hits {
				if h.Lane == 5 {
					five = true
				}
			}
		}
	}
	if any {
		switch {
		case five:
			chart.DrumMode = FIVE
		case marked || isTrue(ini["pro_drums"]):
			chart.DrumMode = PRO
		default:
			chart.DrumMode = FOUR
		}
		for d, notes := range raw {
			if len(notes) > 0 {
				chart.drums[d] = notes
			}
		}
		chart.splitFills(fills)
		chart.generateFills()
	}
	chart.setBars()
	return chart
}

type tickLane struct{ tick, lane int }

// chartDrumExtras is what a .chart drums track has for YARG's own fills: its star power phrases
// (S 2, as charted), its solos (E solo .. E soloend, ChartReader.ConvertSoloEvents) and its double
// kicks (N 32).
type chartDrumExtras struct {
	stars, solos []tickRange
	plusKicks    []int
}

// chartDrums reads a .chart drums track: its notes, whether it marks any cymbals, its fills (S 64),
// its roll and swell lanes (S 65, S 66) and its chartDrumExtras.
func chartDrums(lines []string) ([]*DrumNote, bool, []tickRange, []laneRange, chartDrumExtras) {
	var ex chartDrumExtras
	const none = -1
	soloStart, soloNext := none, none
	var fills []tickRange
	var lanes []laneRange
	byTick := map[int]map[int]*DrumHit{}
	cym, accent, ghost := map[tickLane]bool{}, map[tickLane]bool{}, map[tickLane]bool{}
	var phrases []tickRange
	for _, line := range lines {
		if e := eventRe.FindStringSubmatch(line); e != nil {
			tick := atoi(e[1])
			switch normalizeText(strings.Trim(pyStrip(e[2]), `"`)) {
			case "solo":
				if soloStart == none {
					soloStart = tick
				} else {
					soloNext = tick
				}
			case "soloend":
				if soloStart != none {
					ex.solos = append(ex.solos, tickRange{soloStart, tick})
					if soloNext != tick {
						soloStart = none
					} else {
						soloStart, soloNext = soloNext, none
					}
				}
			}
			continue
		}
		m := trackRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tick, kind, a, length := atoi(m[1]), m[2], atoi(m[3]), atoi(m[4])
		switch {
		case kind == "S":
			switch a {
			case 2:
				phrases = append(phrases, tickRange{tick, tick + max(1, length)})
				ex.stars = append(ex.stars, tickRange{tick, tick + length})
			case 64:
				fills = append(fills, tickRange{tick, tick + length})
			case 65, 66:
				lanes = append(lanes, laneRange{tick, tick + length, a == 66})
			}
		case a <= 5:
			if byTick[tick] == nil {
				byTick[tick] = map[int]*DrumHit{}
			}
			h := NewDrumHit(a, false)
			byTick[tick][a] = &h
		case a == 32:
			ex.plusKicks = append(ex.plusKicks, tick)
		case a >= 66 && a <= 68:
			cym[tickLane{tick, a - 64}] = true
		case a >= 33 && a <= 38:
			accent[tickLane{tick, a - 33}] = true
		case a >= 39 && a <= 44:
			ghost[tickLane{tick, a - 39}] = true
		}
	}
	var notes []*DrumNote
	for _, tick := range sortedKeys(byTick) {
		hits := byTick[tick]
		n := &DrumNote{Tick: tick}
		for _, lane := range sortedKeys(hits) {
			h := hits[lane]
			tl := tickLane{tick, lane}
			h.Cymbal = cym[tl]
			switch {
			case accent[tl]:
				h.Volume = 1.0
			case ghost[tl]:
				h.Volume = 0.45
			default:
				h.Volume = 0.8
			}
			n.Hits = append(n.Hits, *h)
		}
		notes = append(notes, n)
	}
	markDrums(notes, phrases)
	return notes, len(cym) > 0, fills, lanes, ex
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func containsInt(a []int, x int) bool {
	for _, v := range a {
		if v == x {
			return true
		}
	}
	return false
}

func chartTrack(lines []string, threshold int) []*Note {
	byTick := map[int]*Note{}
	forced := map[int]string{}
	var phrases []tickRange
	for _, line := range lines {
		m := trackRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tick, kind, a, length := atoi(m[1]), m[2], atoi(m[3]), atoi(m[4])
		if kind == "S" {
			if a == 2 {
				phrases = append(phrases, tickRange{tick, tick + max(1, length)})
			}
			continue
		}
		switch a {
		case 5:
			if _, ok := forced[tick]; !ok {
				forced[tick] = "flip"
			}
		case 6:
			forced[tick] = TAP
		case 0, 1, 2, 3, 4, 7:
			fret := a + 1
			if a == 7 {
				fret = 0
			}
			n := byTick[tick]
			if n == nil {
				n = &Note{Tick: tick, EndTick: tick, Kind: STRUM}
				byTick[tick] = n
			}
			if !containsInt(n.Frets, fret) {
				n.Frets = append(n.Frets, fret)
			}
			n.EndTick = max(n.EndTick, tick+length)
		}
	}
	groups := make([]*Note, 0, len(byTick))
	for _, t := range sortedKeys(byTick) {
		groups = append(groups, byTick[t])
	}
	for _, n := range groups {
		sort.Ints(n.Frets)
	}
	classify(groups, threshold, forced)
	markNotes(groups, phrases)
	return groups
}

// Load reads a song's chart. files: the package's chart file(s) by lower-case name; notes.mid wins.
func Load(files map[string][]byte, ini map[string]string) (*Chart, error) {
	if data, ok := files["notes.mid"]; ok {
		return ReadMidi(data, ini)
	}
	if data, ok := files["notes.chart"]; ok {
		return ReadChart(decodeUTF8Sig(data), ini), nil
	}
	return nil, &ChartError{"The song has no notes.chart or notes.mid."}
}
