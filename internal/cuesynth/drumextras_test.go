package cuesynth

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
)

// drumsChart is a 120 BPM, 4/4 drums .chart (192 ticks a beat, half a second) with the given lines
// in [ExpertDrums] and [Events].
func drumsChart(track, events []string) *chartfile.Chart {
	var b strings.Builder
	b.WriteString("[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = TS 4\n  0 = B 120000\n}\n")
	b.WriteString("[Events]\n{\n")
	for _, l := range events {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("}\n[ExpertDrums]\n{\n")
	for _, l := range track {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("}\n")
	return chartfile.ReadChart(b.String(), map[string]string{})
}

func TestDrumRollLane(t *testing.T) {
	// A roll lane over eight red sixteenths from beat 4, a kick with the fourth; then a blue note.
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, fmt.Sprintf("%d = N 1 0", 768+48*i))
	}
	lines = append(lines, "912 = N 0 0", "768 = S 65 384", "1536 = N 3 0")
	ch := drumsChart(lines, nil)
	notes := ch.DrumNotes("expert")
	s := DefaultSettings()
	bank := NewBank(9, true, s.DrumLookAhead, true)
	plays := ScheduleDrums(notes, ch.DrumMode, s, bank, nil, nil, nil)
	var loops []*Play
	kick, red, blue := 0, 0, 0
	for _, p := range plays {
		switch {
		case p.Looped:
			loops = append(loops, p)
		case p.Sound == bank.Pads["kick"]:
			kick++
		case p.Sound == bank.Pads["padred"]:
			red++
		case p.Sound == bank.Pads["padblue"]:
			blue++
		}
	}
	if len(loops) != 1 || loops[0].Sound != bank.Roll("padred", false) || !near(loops[0].Start, 2.0-0.2) ||
		!near(loops[0].Stop, ch.Tempo.Time(768+48*7)) || !near(loops[0].FadeIn, 0.02) || !near(loops[0].Volume, 0.7) {
		t.Fatalf("roll loops %+v", loops)
	}
	if red != 0 || kick != 1 || blue != 1 {
		t.Fatalf("red %d kick %d blue %d: the lane's notes must have no cues", red, kick, blue)
	}
	if len(bank.Roll("padred", false).Samples) != RATE/2 {
		t.Fatal("a roll loop is half a second")
	}

	// A swell: red and yellow in turn, two loops, the second half a strike later.
	lines = nil
	for i := 0; i < 8; i++ {
		lines = append(lines, fmt.Sprintf("%d = N %d 0", 768+48*i, 1+i%2))
	}
	lines = append(lines, "768 = S 66 384")
	ch = drumsChart(lines, nil)
	loops = nil
	for _, p := range ScheduleDrums(ch.DrumNotes("expert"), ch.DrumMode, s, bank, nil, nil, nil) {
		if p.Looped {
			loops = append(loops, p)
		} else if p.Sound == bank.Pads["padred"] || p.Sound == bank.Pads["padyellow"] {
			t.Fatal("a swell note has a cue")
		}
	}
	if len(loops) != 2 || loops[0].Sound != bank.Roll("padred", false) || loops[1].Sound != bank.Roll("padyellow", true) {
		t.Fatalf("swell loops %+v", loops)
	}

	// Listen and Repeat: the lane's notes play as notes.
	kept, c := CutDrums(ch.DrumNotes("expert"), ch.Tempo, 4)
	for _, p := range ScheduleDrums(kept, ch.DrumMode, s, bank, c, nil, nil) {
		if p.Looped {
			t.Fatal("a roll loop in Listen and Repeat")
		}
	}
}

func TestDrumFlam(t *testing.T) {
	H := chartfile.NewDrumHit
	notes := []*chartfile.DrumNote{
		{Tick: 0, Hits: []chartfile.DrumHit{H(1, false)}, Time: 1.0},
		{Tick: 1, Hits: []chartfile.DrumHit{H(2, false)}, Time: 1.02},  // a flam: adds yellow
		{Tick: 2, Hits: []chartfile.DrumHit{H(2, false)}, Time: 1.04},  // nothing new: silent
		{Tick: 3, Hits: []chartfile.DrumHit{H(3, false)}, Time: 2.0},   // far: plays
		{Tick: 4, Hits: []chartfile.DrumHit{H(3, false)}, Time: 2.021}, // same pad under 44 ms: silent
	}
	s := DefaultSettings()
	s.CueRepeats = true
	bank := NewBank(9, true, s.DrumLookAhead, true)
	var got []string
	for _, p := range ScheduleDrums(notes, chartfile.PRO, s, bank, nil, nil, nil) {
		for _, id := range []string{"padred", "padyellow", "padblue"} {
			if p.Sound == bank.Pads[id] {
				got = append(got, fmt.Sprintf("%s@%.3f", id, p.Start+0.2))
			}
		}
	}
	want := "padred@1.000 padyellow@1.020 padblue@2.000"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestDrumFillAndActivator(t *testing.T) {
	// Notes on every beat for 16 beats; two star power phrases (beats 0 and 4) fill half the bar;
	// a fill over beats 8 to 12 ends on the beat 12 note, which activates star power.
	var lines []string
	for i := 0; i < 16; i++ {
		lines = append(lines, fmt.Sprintf("%d = N 1 0", 192*i))
	}
	lines = append(lines, "0 = S 2 1", "768 = S 2 1", "1536 = S 64 768")
	ch := drumsChart(lines, nil)
	notes := ch.DrumNotes("expert")
	fills := ch.DrumFills("expert")
	if len(fills) != 1 || !notes[12].Activator {
		t.Fatalf("fills %v, activator %v", fills, notes[12].Activator)
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.DrumLookAhead, true)
	sim := DrumStarSim(notes, ch.Tempo)
	var rise, act *Play
	for _, p := range ScheduleDrums(notes, ch.DrumMode, s, bank, nil, fills, sim) {
		if p.Sound == bank.FillRise(2.0) {
			rise = p
		}
		if p.Sound == bank.Activator {
			act = p
		}
	}
	if rise == nil || !near(rise.Start, 4.0-0.2) || act == nil || !near(act.Start, 6.0-0.2) {
		t.Fatalf("rise %+v, activator %+v", rise, act)
	}
	// The rise ends as the activator's sound plays.
	if math.Abs(rise.Start+float64(len(rise.Sound.Samples))/RATE-act.Start) > 1e-3 {
		t.Fatal("the rise does not end on the activator")
	}
	// Star power runs from the activator: half a bar, four measures (8 s).
	if _, on := sim.At(7.0); !on {
		t.Fatal("star power not running after the activator")
	}
	if _, on := sim.At(14.5); on {
		t.Fatal("star power still running after four measures")
	}

	// With one phrase the bar is a quarter: no rise, no star power hit.
	lines = append(lines[:16], "0 = S 2 1", "1536 = S 64 768")
	ch = drumsChart(lines, nil)
	notes = ch.DrumNotes("expert")
	for _, p := range ScheduleDrums(notes, ch.DrumMode, s, bank, nil, ch.DrumFills("expert"), DrumStarSim(notes, ch.Tempo)) {
		if p.Sound == bank.Activator || p.Sound == bank.FillRise(2.0) {
			t.Fatal("fill sounds with a quarter bar")
		}
	}
}

func TestBigRockEndingNotCut(t *testing.T) {
	// 32 eighths; a [coda] at beat 12 turns the fill from there into a big rock ending.
	var lines []string
	for i := 0; i < 32; i++ {
		lines = append(lines, fmt.Sprintf("%d = N 1 0", 96*i))
	}
	lines = append(lines, "2304 = S 64 96")
	ch := drumsChart(lines, []string{`2304 = E "coda"`})
	notes := ch.DrumNotes("expert")
	if len(ch.DrumFills("expert")) != 0 {
		t.Fatal("a fill in the coda is still a fill")
	}
	bre := 0
	for _, n := range notes {
		if n.BRE {
			bre++
		}
	}
	if bre != 2 { // ticks 2304 and 2400 (the end is included)
		t.Fatalf("%d big rock ending notes", bre)
	}
	kept, c := CutDrums(notes, ch.Tempo, 4)
	in := 0
	for _, n := range kept {
		if n.BRE {
			in++
			if c.Of(n) != nil {
				t.Fatal("a big rock ending note has a call")
			}
		}
	}
	if in != 2 {
		t.Fatalf("%d of 2 big rock ending notes kept", in)
	}
	for _, g := range c.Groups {
		if g.StartTick <= 2400 && g.EndTick >= 2304 {
			t.Fatalf("a group over the big rock ending: %+v", g)
		}
	}

	// Five-fret: HighwaySound plays nothing for big rock ending notes, and they are never cut.
	five := []*chartfile.Note{
		{Tick: 0, Frets: []int{1}, EndTick: 0, Kind: chartfile.STRUM, Time: 1, End: 1, Safe: 1},
		{Tick: 192, Frets: []int{2}, EndTick: 192, Kind: chartfile.STRUM, Time: 1.5, End: 1.5, Safe: 1.5, BRE: true},
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.LookAhead, false)
	for _, p := range Schedule(five, ch.Tempo, s, bank, nil, nil, 0, math.Inf(1)) {
		if p.Sound == bank.Strike[2] {
			t.Fatal("a big rock ending note sounds")
		}
	}
	k5, _ := CutNotes(five, ch.Tempo, 4)
	if len(k5) == 0 || !k5[len(k5)-1].BRE {
		t.Fatal("the big rock ending note was cut")
	}
}

func TestPhraseStartChimeNeedsQuarterBar(t *testing.T) {
	// Three one-note phrases a bar apart: the first finds an empty bar (sparkle only), the second a
	// quarter (the chime); the second completes half a bar, star power turns on, so the third does not.
	ch := chartfile.ReadChart(testChart, map[string]string{})
	var notes []*chartfile.Note
	for i := 0; i < 3; i++ {
		tk := 768 * i
		notes = append(notes, &chartfile.Note{Tick: tk, Frets: []int{1 + i}, EndTick: tk, Kind: chartfile.STRUM,
			Star: true, StarStart: true, StarEnd: true, Time: ch.Tempo.Time(tk)})
		notes[i].End, notes[i].Safe = notes[i].Time, notes[i].Time
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.LookAhead, false)
	var chimes, sparkles []float64
	for _, p := range Schedule(notes, ch.Tempo, s, bank, nil, NoteStarSim(notes, ch.Tempo), -10, math.Inf(1)) {
		if p.Sound == bank.SPStart {
			chimes = append(chimes, round3(p.Start+s.LookAhead))
		}
		if p.Sound == bank.SPNote {
			sparkles = append(sparkles, round3(p.Start+s.LookAhead))
		}
	}
	if len(chimes) != 1 || chimes[0] != 2.0 || len(sparkles) != 2 {
		t.Fatalf("chimes %v sparkles %v", chimes, sparkles)
	}
}

func TestGeneratedFillSoundsLikeCharted(t *testing.T) {
	// A chart with no fills gets YARG's own: here over bar 3 (ticks 2304 to 3072). It must sound
	// exactly like the same fill charted, with and without Listen and Repeat.
	var lines []string
	for i := 0; i <= 32; i++ {
		lines = append(lines, fmt.Sprintf("%d = N 1 0", 192*i))
	}
	lines = append(lines, "0 = S 2 1", "768 = S 2 1")
	made := drumsChart(lines, nil)
	charted := drumsChart(append(lines, "2304 = S 64 768"), nil)
	if f := made.DrumFills("expert"); len(f) != 1 || f[0].Tick != 2304 || f[0].EndTick != 3072 {
		t.Fatalf("made fills %+v", f)
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.DrumLookAhead, true)
	type cue struct {
		start, vol, pan float64
		sound           *Sound
	}
	schedule := func(ch *chartfile.Chart, listen bool) []cue {
		notes := ch.DrumNotes("expert")
		var calls *Calls
		if listen {
			notes, calls = CutDrums(notes, ch.Tempo, s.CallLength)
		}
		var out []cue
		for _, p := range ScheduleDrums(notes, ch.DrumMode, s, bank, calls, ch.DrumFills("expert"), DrumStarSim(notes, ch.Tempo)) {
			out = append(out, cue{p.Start, p.Volume, p.Pan, p.Sound})
		}
		return out
	}
	for _, listen := range []bool{false, true} {
		a, b := schedule(made, listen), schedule(charted, listen)
		if len(a) != len(b) {
			t.Fatalf("listen %v: %d plays made, %d charted", listen, len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("listen %v: play %d %+v vs %+v", listen, i, a[i], b[i])
			}
		}
		act := false
		for _, p := range a {
			act = act || p.sound == bank.Activator
		}
		if !listen && !act {
			t.Fatal("no star power hit on the made fill")
		}
	}
}
