package chartfile

import (
	"bytes"
	"encoding/binary"
	"math"
	"runtime"
	"sort"
	"strings"
)

type evKind uint8

const (
	evOn evKind = iota + 1
	evOff
	evText
	evTempo
	evTS
	evSysex
)

// midiEvent is one event of a track. on / off: a = note, b = velocity. text: text. tempo: a =
// microseconds per beat. ts: a = numerator, b = denominator. sysex: body.
type midiEvent struct {
	tick int
	kind evKind
	a, b int
	text string
	body []byte
}

type midiTrack struct {
	name   string
	events []midiEvent
}

// clip is Python's lenient slice data[a:b].
func clip(data []byte, a, b int) []byte {
	a = min(max(a, 0), len(data))
	b = min(max(b, a), len(data))
	return data[a:b]
}

func vlq(data []byte, pos int) (int, int) {
	v := 0
	for {
		b := data[pos]
		pos++
		v = v<<7 | int(b&0x7F)
		if b&0x80 == 0 {
			return v, pos
		}
	}
}

// midiTracks reads a MIDI file: its division and tracks. A read past the end of the data is
// reported as a damaged notes.mid.
func midiTracks(data []byte) (division int, tracks []midiTrack, err error) {
	if len(data) < 4 || !bytes.Equal(data[:4], []byte("MThd")) {
		return 0, nil, &ChartError{"notes.mid is not a MIDI file."}
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); !ok {
				panic(r)
			}
			division, tracks, err = 0, nil, &ChartError{"notes.mid is damaged."}
		}
	}()
	be := binary.BigEndian
	hlen := int(be.Uint32(data[4:8]))
	ntracks := int(be.Uint16(data[10:12]))
	division = int(be.Uint16(data[12:14]))
	if division&0x8000 != 0 {
		return 0, nil, &ChartError{"notes.mid uses SMPTE timing, which charts never use."}
	}
	pos := 8 + hlen
	for t := 0; t < ntracks; t++ {
		if !bytes.Equal(clip(data, pos, pos+4), []byte("MTrk")) {
			break
		}
		tlen := int(be.Uint32(data[pos+4 : pos+8]))
		pos += 8
		end := pos + tlen
		tick, status := 0, 0
		tr := midiTrack{}
		for pos < end {
			var delta int
			delta, pos = vlq(data, pos)
			tick += delta
			b := data[pos]
			if b == 0xFF {
				mtype := data[pos+1]
				var length int
				length, pos = vlq(data, pos+2)
				body := clip(data, pos, pos+length)
				pos += length
				switch {
				case mtype == 0x03 && tr.name == "":
					tr.name = latin1(body)
				case mtype == 0x01 || mtype == 0x05:
					tr.events = append(tr.events, midiEvent{tick: tick, kind: evText, text: latin1(body)})
				case mtype == 0x51 && length == 3:
					us := 0
					for _, c := range body {
						us = us<<8 | int(c)
					}
					tr.events = append(tr.events, midiEvent{tick: tick, kind: evTempo, a: us})
				case mtype == 0x58 && length >= 2:
					tr.events = append(tr.events, midiEvent{tick: tick, kind: evTS, a: int(body[0]), b: pow2(int(body[1]))})
				}
				continue
			}
			if b == 0xF0 || b == 0xF7 {
				var length int
				length, pos = vlq(data, pos+1)
				tr.events = append(tr.events, midiEvent{tick: tick, kind: evSysex, body: clip(data, pos, pos+length)})
				pos += length
				continue
			}
			if b&0x80 != 0 {
				status = int(b)
				pos++
			}
			kind := status & 0xF0
			if kind == 0xC0 || kind == 0xD0 {
				pos++
				continue
			}
			a, v := int(data[pos]), int(data[pos+1])
			pos += 2
			if kind == 0x90 && v > 0 {
				tr.events = append(tr.events, midiEvent{tick: tick, kind: evOn, a: a, b: v})
			} else if kind == 0x80 || kind == 0x90 {
				tr.events = append(tr.events, midiEvent{tick: tick, kind: evOff, a: a})
			}
		}
		pos = end
		tracks = append(tracks, tr)
	}
	return division, tracks, nil
}

// ranges is the (start, end) tick ranges a MIDI note is held for.
func ranges(events []midiEvent, note int) []tickRange {
	var out []tickRange
	start, held := 0, false
	for _, e := range events {
		if e.a != note || (e.kind != evOn && e.kind != evOff) {
			continue
		}
		if e.kind == evOn && !held {
			start, held = e.tick, true
		} else if e.kind == evOff && held {
			out = append(out, tickRange{start, e.tick})
			held = false
		}
	}
	return out
}

// rangesVel is ranges with each range's note-on velocity.
func rangesVel(events []midiEvent, note int) ([]tickRange, []int) {
	var out []tickRange
	var vel []int
	start, v, held := 0, 0, false
	for _, e := range events {
		if e.a != note || (e.kind != evOn && e.kind != evOff) {
			continue
		}
		if e.kind == evOn && !held {
			start, v, held = e.tick, e.b, true
		} else if e.kind == evOff && held {
			out = append(out, tickRange{start, e.tick})
			vel = append(vel, v)
			held = false
		}
	}
	return out, vel
}

func in(rs []tickRange, tick int) bool {
	for _, r := range rs {
		if r.a <= tick && tick < r.b {
			return true
		}
	}
	return false
}

// ReadMidi reads a notes.mid.
func ReadMidi(data []byte, ini map[string]string) (*Chart, error) {
	division, tracks, err := midiTracks(data)
	if err != nil {
		return nil, err
	}
	if division == 0 {
		// Tick times would divide by zero.
		return nil, &ChartError{"notes.mid has no ticks per beat."}
	}
	tempo := &TempoMap{Resolution: division}
	if len(tracks) > 0 {
		for _, e := range tracks[0].events {
			if e.kind == evTempo && e.a > 0 {
				tempo.Tempos = append(tempo.Tempos, Tempo{e.tick, e.a})
			} else if e.kind == evTS {
				tempo.Signatures = append(tempo.Signatures, Signature{e.tick, e.a, e.b})
			}
		}
	}
	tempo.Finish()
	chart := newChart(tempo, true, hopoThreshold(division, ini, true))
	v, present := ini["sustain_cutoff_threshold"]
	cutoff, ok := pyInt(v, present)
	if !ok {
		cutoff = floorDiv(division, 3)
	}
	byName := map[string][]midiEvent{}
	for _, t := range tracks {
		key := pyUpper(pyStrip(t.name))
		if _, seen := byName[key]; !seen {
			byName[key] = t.events
		}
	}
	for _, e := range byName["EVENTS"] {
		if e.kind == evText {
			chart.events = append(chart.events, textEvent{e.tick, normalizeText(e.text)})
		}
	}
	for _, part := range GuitarParts {
		var events []midiEvent
		found := false
		for _, n := range MidiTracks[part] {
			if ev, ok := byName[n]; ok {
				events, found = ev, true
				break
			}
		}
		if !found {
			continue
		}
		enhanced := false
		for _, e := range events {
			if e.kind == evText && strings.Trim(e.text, "[] ") == "ENHANCED_OPENS" {
				enhanced = true
				break
			}
		}
		stars := ranges(events, 116)
		tapsAll := ranges(events, 104)
		// Note 120 is a big rock ending on every difficulty.
		chart.bre[part] = map[string][]tickRange{}
		for _, diff := range Difficulties {
			chart.bre[part][diff] = ranges(events, 120)
		}
		for _, diff := range Difficulties {
			base := MidiBase[diff]
			groups := midiDifficulty(events, diff, base, enhanced, cutoff)
			if len(groups) == 0 {
				continue
			}
			forced := map[int]string{}
			hopoR, strumR := ranges(events, base+5), ranges(events, base+6)
			taps := append(append([]tickRange{}, tapsAll...), sysexRanges(events, diff, 4)...)
			for _, n := range groups {
				switch {
				case in(taps, n.Tick):
					forced[n.Tick] = TAP
				case in(hopoR, n.Tick):
					forced[n.Tick] = "hopo"
				case in(strumR, n.Tick):
					forced[n.Tick] = "strum"
				}
			}
			classify(groups, chart.HopoThreshold, forced)
			markNotes(groups, stars)
			if chart.notes[part] == nil {
				chart.notes[part] = map[string][]*Note{}
			}
			chart.notes[part][diff] = groups
		}
	}
	if drums := byName["PART DRUMS"]; len(drums) > 0 {
		midiDrums(chart, drums, ini)
	}
	chart.bre["prokeys"] = map[string][]tickRange{}
	expertBRE := ranges(byName[ProkeysTracks["expert"]], 120)
	for _, diff := range Difficulties {
		if events := byName[ProkeysTracks[diff]]; len(events) > 0 {
			if notes := midiProkeys(events, cutoff); len(notes) > 0 {
				chart.prokeys[diff] = notes
			}
			// Each track's note 120, and Expert's copied down (MidReader.CopyDownBREPhrases).
			chart.bre["prokeys"][diff] = ranges(events, 120)
			if diff != "expert" {
				chart.bre["prokeys"][diff] = append(chart.bre["prokeys"][diff], expertBRE...)
			}
		}
	}
	// The BEAT track's beat lines (MidReader.ReadSongBeats: 12 Measure, 13 Strong, 14 Weak).
	for _, e := range byName["BEAT"] {
		if e.kind == evOn && e.a >= 12 && e.a <= 14 {
			chart.beatTrack = true
			if e.a == 12 {
				chart.beatMeasures = append(chart.beatMeasures, e.tick)
			}
		}
	}
	chart.generateFills()
	chart.setBars()
	chart.Key = detectKey(byName, tempo)
	return chart, nil
}

// midiDrums reads PART DRUMS: per difficulty kick = base, red .. five-lane green = base + 1 .. 5.
func midiDrums(chart *Chart, events []midiEvent, ini map[string]string) {
	dynamics := false
	for _, e := range events {
		if e.kind == evText && strings.Contains(e.text, "ENABLE_CHART_DYNAMICS") {
			dynamics = true
			break
		}
	}
	toms := map[int][]tickRange{}
	marked := false
	for _, lane := range []int{2, 3, 4} {
		toms[lane] = ranges(events, 108+lane)
		marked = marked || len(toms[lane]) > 0
	}
	stars := ranges(events, 116)
	solos := ranges(events, 103)
	for _, d := range Difficulties {
		chart.gen.stars[d], chart.gen.solos[d] = stars, solos
	}
	for _, e := range events {
		if e.kind == evOn && e.a == 95 { // the Expert+ double kick
			chart.gen.plusKicks = append(chart.gen.plusKicks, e.tick)
		}
	}
	// Notes 120 to 124 are fills on every difficulty; the ones in a coda are big rock endings.
	fills := map[string][]tickRange{}
	for note := 120; note <= 124; note++ {
		for _, r := range ranges(events, note) {
			for _, d := range Difficulties {
				fills[d] = append(fills[d], r)
			}
		}
	}
	chart.splitFills(fills)
	// Lanes (MidReader.ProcessLanePhrase): 126 rolls, 127 swells, on Expert, and on Hard when the
	// note's velocity is 41 to 50.
	for note, swell := range map[int]bool{126: false, 127: true} {
		rs, vel := rangesVel(events, note)
		for k, r := range rs {
			lr := laneRange{r.a, r.b, swell}
			chart.drumLanes["expert"] = append(chart.drumLanes["expert"], lr)
			if vel[k] >= 41 && vel[k] <= 50 {
				chart.drumLanes["hard"] = append(chart.drumLanes["hard"], lr)
			}
		}
	}
	five := false
	for _, e := range events {
		if e.kind != evOn {
			continue
		}
		for _, base := range MidiBase {
			if e.a-base == 5 {
				five = true
			}
		}
	}
	switch {
	case five:
		chart.DrumMode = FIVE
	case marked || isTrue(ini["pro_drums"]):
		chart.DrumMode = PRO
	default:
		chart.DrumMode = FOUR
	}
	for _, diff := range Difficulties {
		base := MidiBase[diff]
		byTick := map[int]map[int]DrumHit{}
		for _, e := range events {
			lane := e.a - base
			if e.kind != evOn || lane < 0 || lane > 5 {
				continue
			}
			vol := 0.8
			if dynamics && lane > 0 {
				switch e.b {
				case 1:
					vol = 0.45
				case 127:
					vol = 1.0
				}
			}
			tr, isTomLane := toms[lane]
			cymbal := isTomLane && !in(tr, e.tick)
			if byTick[e.tick] == nil {
				byTick[e.tick] = map[int]DrumHit{}
			}
			byTick[e.tick][lane] = DrumHit{Lane: lane, Cymbal: cymbal, Volume: vol}
		}
		var notes []*DrumNote
		for _, t := range sortedKeys(byTick) {
			n := &DrumNote{Tick: t}
			for _, k := range sortedKeys(byTick[t]) {
				n.Hits = append(n.Hits, byTick[t][k])
			}
			notes = append(notes, n)
		}
		if len(notes) > 0 {
			markDrums(notes, stars)
			chart.drums[diff] = notes
		}
	}
}

func midiProkeys(events []midiEvent, cutoff int) []*KeyNote {
	byTick := map[int]*KeyNote{}
	for note := 48; note < 73; note++ {
		for _, r := range ranges(events, note) {
			n := byTick[r.a]
			if n == nil {
				n = &KeyNote{Tick: r.a}
				byTick[r.a] = n
			}
			if containsInt(n.Keys, note-48) {
				continue
			}
			n.Keys = append(n.Keys, note-48)
			if r.b-r.a >= cutoff {
				n.EndTicks = append(n.EndTicks, r.b)
			} else {
				n.EndTicks = append(n.EndTicks, r.a)
			}
		}
	}
	notes := make([]*KeyNote, 0, len(byTick))
	for _, t := range sortedKeys(byTick) {
		notes = append(notes, byTick[t])
	}
	gliss := ranges(events, 126)
	for _, n := range notes {
		n.Gliss = in(gliss, n.Tick)
	}
	markKeys(notes, ranges(events, 116))
	return notes
}

// sysexRanges is the Phase Shift SysEx phrases: 50 53 00 00 <difficulty or FF> <code> <1 on / 0 off>.
// code 1 = open notes, 4 = taps.
func sysexRanges(events []midiEvent, diff string, code int) []tickRange {
	var out []tickRange
	start, open := 0, false
	want := MidiDiffIndex[diff]
	for _, e := range events {
		body := e.body
		if e.kind != evSysex || len(body) < 7 || !bytes.Equal(body[:4], []byte("PS\x00\x00")) {
			continue
		}
		if (int(body[4]) != want && body[4] != 0xFF) || int(body[5]) != code {
			continue
		}
		if body[6] != 0 && !open {
			start, open = e.tick, true
		} else if body[6] == 0 && open {
			out = append(out, tickRange{start, e.tick + 1})
			open = false
		}
	}
	return out
}

func midiDifficulty(events []midiEvent, diff string, base int, enhanced bool, cutoff int) []*Note {
	opens := sysexRanges(events, diff, 1)
	lanes := map[int]int{}
	for i := 0; i < 5; i++ {
		lanes[base+i] = i + 1
	}
	if enhanced {
		lanes[base-1] = 0
	}
	byTick := map[int]*Note{}
	held := map[int]int{}
	for _, e := range events {
		fret, ok := lanes[e.a]
		if !ok || (e.kind != evOn && e.kind != evOff) {
			continue
		}
		if e.kind == evOn {
			held[e.a] = e.tick
			continue
		}
		start, ok := held[e.a]
		if !ok {
			continue
		}
		delete(held, e.a)
		if fret == 1 && in(opens, start) {
			fret = 0
		}
		length := e.tick - start
		if length < cutoff {
			length = 0
		}
		n := byTick[start]
		if n == nil {
			n = &Note{Tick: start, EndTick: start, Kind: STRUM}
			byTick[start] = n
		}
		if !containsInt(n.Frets, fret) {
			n.Frets = append(n.Frets, fret)
		}
		n.EndTick = max(n.EndTick, start+length)
	}
	groups := make([]*Note, 0, len(byTick))
	for _, t := range sortedKeys(byTick) {
		n := byTick[t]
		sort.Ints(n.Frets)
		groups = append(groups, n)
	}
	return groups
}

// Krumhansl-Kessler key profiles.
var (
	Major = []float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	Minor = []float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// MinNotes is the fewest pitched notes a key is guessed from.
const MinNotes = 16

func detectKey(byName map[string][]midiEvent, tempo *TempoMap) *Key {
	hist := make([]float64, 12)
	count := 0
	for _, name := range []string{"PART VOCALS", "HARM1", "HARM2", "HARM3"} {
		events := byName[name]
		if len(events) == 0 {
			continue
		}
		unpitched := map[int]bool{}
		for _, e := range events {
			if e.kind != evText {
				continue
			}
			t := strings.TrimRightFunc(e.text, pySpace)
			if strings.HasSuffix(t, "#") || strings.HasSuffix(t, "^") {
				unpitched[e.tick] = true
			}
		}
		for note := 36; note < 85; note++ {
			for _, r := range ranges(events, note) {
				if unpitched[r.a] {
					continue
				}
				hist[note%12] += pmax(0.1, tempo.Time(r.b)-tempo.Time(r.a))
				count++
			}
		}
	}
	if keys := byName["PART REAL_KEYS_X"]; len(keys) > 0 {
		for note := 48; note < 73; note++ {
			for _, r := range ranges(keys, note) {
				hist[note%12] += pmax(0.1, tempo.Time(r.b)-tempo.Time(r.a))
				count++
			}
		}
	}
	if count < MinNotes {
		return nil
	}
	best := math.Inf(-1)
	var result *Key
	for tonic := 0; tonic < 12; tonic++ {
		for _, minor := range []bool{false, true} {
			profile := Major
			if minor {
				profile = Minor
			}
			if r := correlate(hist, profile, tonic); r > best {
				best, result = r, &Key{tonic, minor}
			}
		}
	}
	return result
}

func correlate(hist, profile []float64, tonic int) float64 {
	mh, mp := fsum(hist)/12, fsum(profile)/12
	num, dh, dp := 0.0, 0.0, 0.0
	for i := 0; i < 12; i++ {
		h, p := hist[(i+tonic)%12]-mh, profile[i]-mp
		num += h * p
		dh += h * h
		dp += p * p
	}
	if dh <= 0 || dp <= 0 {
		return 0.0
	}
	return num / math.Pow(dh*dp, 0.5)
}
