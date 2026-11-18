package chartfile

import (
	"math"
	"reflect"
	"testing"
)

const testChart = `[Song]
{
  Resolution = 192
}
[SyncTrack]
{
  0 = TS 4
  0 = B 120000
}
[ExpertSingle]
{
  0 = N 0 0
  48 = N 1 0
  96 = N 1 0
  144 = N 0 0
  144 = N 5 0
  192 = N 2 0
  192 = N 6 0
  384 = N 7 384
  384 = S 2 200
  960 = N 0 0
  960 = N 1 0
  1008 = N 2 0
}
[EasySingle]
{
  0 = N 0 0
}
`

const drumChart = `[Song]
{
  Resolution = 192
}
[SyncTrack]
{
  0 = TS 4
  0 = B 120000
}
[ExpertDrums]
{
  0 = N 0 0
  0 = N 1 0
  192 = N 2 0
  192 = N 66 0
  288 = N 2 0
  288 = N 66 0
  384 = N 3 0
  384 = N 42 0
  576 = N 4 0
  576 = N 37 0
  576 = S 2 100
}
`

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestChartNoteKinds(t *testing.T) {
	ch := ReadChart(testChart, map[string]string{})
	if ch.HopoThreshold != 65 {
		t.Fatalf("threshold %d", ch.HopoThreshold)
	}
	if got := ch.Parts(); !reflect.DeepEqual(got, []Part{{"guitar", []string{"easy", "expert"}}}) {
		t.Fatalf("parts %v", got)
	}
	notes := ch.Notes("guitar", "expert")
	type kind struct {
		tick  int
		frets []int
		kind  string
	}
	var got []kind
	for _, n := range notes {
		got = append(got, kind{n.Tick, n.Frets, n.Kind})
	}
	want := []kind{
		{0, []int{1}, "strum"},
		{48, []int{2}, "hopo"},
		{96, []int{2}, "strum"},
		{144, []int{1}, "strum"},
		{192, []int{3}, "tap"},
		{384, []int{0}, "strum"},
		{960, []int{1, 2}, "strum"},
		{1008, []int{3}, "hopo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds %v", got)
	}
	open := notes[5]
	if !open.Star || notes[4].Star {
		t.Fatal("star")
	}
	if !near(open.Time, 1.0) || !near(open.End, 2.0) || !near(open.Safe, 1.875) {
		t.Fatalf("times %v %v %v", open.Time, open.End, open.Safe)
	}
	if !near(ch.Tempo.BeatLength(1.0), 0.5) {
		t.Fatal("beat length")
	}
}

func TestChartDrums(t *testing.T) {
	ch := ReadChart(drumChart, map[string]string{})
	if got := ch.Parts(); !reflect.DeepEqual(got, []Part{{"drums", []string{"expert"}}}) || ch.DrumMode != PRO {
		t.Fatalf("parts %v mode %s", got, ch.DrumMode)
	}
	notes := ch.DrumNotes("expert")
	type row struct {
		tick int
		hits []DrumHit
	}
	var got []row
	for _, n := range notes {
		got = append(got, row{n.Tick, n.Hits})
	}
	want := []row{
		{0, []DrumHit{{Lane: 0, Cymbal: false, Volume: 0.8}, {Lane: 1, Cymbal: false, Volume: 0.8}}},
		{192, []DrumHit{{Lane: 2, Cymbal: true, Volume: 0.8}}},
		{288, []DrumHit{{Lane: 2, Cymbal: true, Volume: 0.8}}},
		{384, []DrumHit{{Lane: 3, Cymbal: false, Volume: 0.45}}},
		{576, []DrumHit{{Lane: 4, Cymbal: false, Volume: 1.0}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("drums %v", got)
	}
	if !notes[4].Star || !near(notes[1].Time, 0.5) {
		t.Fatal("star / time")
	}
	beats := ch.Tempo.Beats(2.1)
	wantBeats := []Beat{{Time: 0, First: true}, {Time: 0.5}, {Time: 1.0}, {Time: 1.5}, {Time: 2.0, First: true}}
	for i, b := range wantBeats {
		if !near(beats[i].Time, b.Time) || beats[i].First != b.First {
			t.Fatalf("beat %d %v", i, beats[i])
		}
	}
}

func TestLoadNoChart(t *testing.T) {
	_, err := Load(map[string][]byte{}, nil)
	if _, ok := err.(*ChartError); !ok {
		t.Fatalf("err %v", err)
	}
}

func TestPythonText(t *testing.T) {
	if got := pySplitLines("a\r\nb\rc\n\nd\u0085e"); !reflect.DeepEqual(got, []string{"a", "b", "c", "", "d", "e"}) {
		t.Fatalf("%q", got)
	}
	for in, want := range map[string]int{" 12 ": 12, "1_0": 10, "-2.9": -2, "1e2": 100} {
		if v, ok := pyInt(in, true); !ok || v != want {
			t.Fatalf("%q -> %d %v", in, v, ok)
		}
	}
	for _, in := range []string{"", "0x10", "1__0", "abc", "nan", "inf", "--1"} {
		if _, ok := pyInt(in, true); ok {
			t.Fatalf("%q should fail", in)
		}
	}
	if floorDiv(-5, 3) != -2 || pyMod(-1, 4) != 3 {
		t.Fatal("floor")
	}
}

// buildMidi makes a type 1 MIDI file from raw track bodies (without the end-of-track event).
func buildMidi(division int, tracks ...[]byte) []byte {
	out := []byte("MThd\x00\x00\x00\x06\x00\x01")
	out = append(out, byte(len(tracks)>>8), byte(len(tracks)), byte(division>>8), byte(division))
	for _, t := range tracks {
		t = append(t, 0x00, 0xFF, 0x2F, 0x00)
		n := len(t)
		out = append(out, "MTrk"...)
		out = append(out, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		out = append(out, t...)
	}
	return out
}

func meta(kind byte, body string) []byte {
	return append([]byte{0x00, 0xFF, kind, byte(len(body))}, body...)
}

func TestReadMidiFrets(t *testing.T) {
	tempoTrack := append(meta(0x51, "\x07\xA1\x20"), meta(0x58, "\x04\x02\x18\x08")...) // 120 BPM, 4/4
	// Expert green at 0 for 480 ticks (a sustain), red at 160 (natural HOPO: 160 <= 480/3+1), with
	// running status, then a forced-strum yellow at 320.
	var gtr []byte
	gtr = append(gtr, meta(0x03, "part guitar ")...)
	gtr = append(gtr, 0x00, 0x90, 96, 100) // green on at 0
	gtr = append(gtr, 0x81, 0x20, 97, 100) // +160: red on (running status)
	gtr = append(gtr, 0x10, 97, 0)         // +16: red off (velocity 0)
	gtr = append(gtr, 0x81, 0x10, 0x80, 102, 100)
	gtr = append(gtr, 0x00, 0x90, 98, 100, 0x00, 0x90, 102, 100) // +144 = 320: strum marker + yellow
	gtr = append(gtr, 0x81, 0x20, 0x80, 96, 0)                   // +160 = 480: green off
	gtr = append(gtr, 0x00, 0x80, 98, 0, 0x00, 0x80, 102, 0)
	ch, err := Load(map[string][]byte{"notes.mid": buildMidi(480, tempoTrack, gtr)}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if ch.HopoThreshold != 161 || ch.Key != nil {
		t.Fatalf("threshold %d key %v", ch.HopoThreshold, ch.Key)
	}
	notes := ch.Notes("guitar", "expert")
	if len(notes) != 3 {
		t.Fatalf("%d notes", len(notes))
	}
	if notes[0].Kind != STRUM || notes[1].Kind != HOPO || notes[2].Kind != STRUM {
		t.Fatalf("kinds %s %s %s", notes[0].Kind, notes[1].Kind, notes[2].Kind)
	}
	if notes[0].EndTick != 480 || notes[1].EndTick != 160 || !near(notes[2].Time, 1.0/3) {
		t.Fatalf("ends %d %d time %v", notes[0].EndTick, notes[1].EndTick, notes[2].Time)
	}
}

func TestReadMidiDamaged(t *testing.T) {
	data := buildMidi(480, meta(0x51, "\x07\xA1\x20"))
	if _, err := ReadMidi(data[:len(data)-6], nil); err == nil {
		t.Fatal("want an error for a cut-off file")
	}
}
