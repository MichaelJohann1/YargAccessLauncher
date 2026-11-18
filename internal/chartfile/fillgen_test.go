package chartfile

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/sng"
)

// fillChart is a 120 BPM 4/4 .chart (192 ticks a beat, a bar of 768 ticks is 2 s) with the given
// [ExpertDrums] and [EasyDrums] lines.
func fillChart(expert, easy []string) *Chart {
	var b strings.Builder
	b.WriteString("[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = TS 4\n  0 = B 120000\n}\n")
	for name, lines := range map[string][]string{"ExpertDrums": expert, "EasyDrums": easy} {
		if len(lines) == 0 {
			continue
		}
		b.WriteString("[" + name + "]\n{\n")
		for _, l := range lines {
			b.WriteString("  " + l + "\n")
		}
		b.WriteString("}\n")
	}
	return ReadChart(b.String(), map[string]string{})
}

// quarters is a red note on every beat from tick from to tick to, skipping the ticks in skip.
func quarters(from, to int, skip ...int) []string {
	var out []string
	for t := from; t <= to; t += 192 {
		if !containsInt(skip, t) {
			out = append(out, fmt.Sprintf("%d = N 1 0", t))
		}
	}
	return out
}

func TestGeneratedFillSixteenNoteRule(t *testing.T) {
	// Star power ends at 0.5 s, so the first bar looked at is bar 4 (tick 3072, 8 s). Sixteen notes
	// after it, up to bar 8 (tick 6144), make a fill over bar 3.
	ch := fillChart(append(quarters(0, 6144), "0 = S 2 192"), nil)
	fills := ch.DrumFills("expert")
	if len(fills) != 1 || fills[0].Tick != 2304 || fills[0].EndTick != 3072 {
		t.Fatalf("fills %+v", fills)
	}
	notes := ch.DrumNotes("expert")
	for i, n := range notes {
		if n.Activator != (n.Tick == 3072) {
			t.Fatalf("note %d at %d activator %v", i, n.Tick, n.Activator)
		}
	}

	// Fifteen notes: no fill.
	if f := fillChart(append(quarters(0, 6144, 6144), "0 = S 2 192"), nil).DrumFills("expert"); len(f) != 0 {
		t.Fatalf("15 notes made fills %+v", f)
	}
	// Chords count each pad: eight two-pad notes are sixteen.
	lines := []string{"0 = S 2 192", "3072 = N 1 0"}
	for t := 3072 + 384; t <= 6144; t += 384 {
		lines = append(lines, fmt.Sprintf("%d = N 1 0", t), fmt.Sprintf("%d = N 2 0", t))
	}
	if f := fillChart(lines, nil).DrumFills("expert"); len(f) != 1 || f[0].EndTick != 3072 {
		t.Fatalf("chords: fills %+v", f)
	}
	// No star power: no fills.
	if f := fillChart(quarters(0, 6144), nil).DrumFills("expert"); len(f) != 0 {
		t.Fatalf("no star power made fills %+v", f)
	}
	// A charted fill stops YARG making its own.
	f := fillChart(append(quarters(0, 6144), "0 = S 2 192", "768 = S 64 768"), nil).DrumFills("expert")
	if len(f) != 1 || f[0].Tick != 768 {
		t.Fatalf("charted fill: %+v", f)
	}
}

func TestGeneratedFillSkipsTwoSecondsAfterStarPower(t *testing.T) {
	// Star power runs to bar 4 (8 s): bar 4 is too close, bar 8 (16 s) is the first that can end a
	// fill (from bar 7).
	ch := fillChart(append(quarters(0, 12288), "0 = S 2 3072"), nil)
	fills := ch.DrumFills("expert")
	if len(fills) == 0 || fills[0].Tick != 5376 || fills[0].EndTick != 6144 {
		t.Fatalf("fills %+v", fills)
	}
}

func TestGeneratedFillActivatorChoice(t *testing.T) {
	// Pro drums: the note on the fill's end (red) is not ideal; a kick with the green cymbal 48
	// ticks later (within an eighth of the 768-tick fill) is, so the fill ends there. On Easy any
	// note is ideal: the fill ends on its end tick.
	expert := append(quarters(0, 6144), "0 = S 2 192", "3120 = N 0 0", "3120 = N 4 0", "3120 = N 68 0")
	easy := []string{"0 = N 1 0", "0 = S 2 192", "3072 = N 1 0", "3120 = N 0 0"}
	ch := fillChart(expert, easy)
	if ch.DrumMode != PRO {
		t.Fatalf("mode %s", ch.DrumMode)
	}
	if f := ch.DrumFills("expert"); len(f) != 1 || f[0].Tick != 2304 || f[0].EndTick != 3120 {
		t.Fatalf("expert fills %+v", f)
	}
	if f := ch.DrumFills("easy"); len(f) != 1 || f[0].EndTick != 3072 {
		t.Fatalf("easy fills %+v", f)
	}
	for _, n := range ch.DrumNotes("expert") {
		if n.Activator != (n.Tick == 3120) {
			t.Fatalf("activator at %d", n.Tick)
		}
	}
	// A difficulty whose last note is before the fill's end gets no fill.
	ch = fillChart(expert, []string{"0 = N 1 0", "0 = S 2 192", "3000 = N 1 0"})
	if f := ch.DrumFills("easy"); len(f) != 0 {
		t.Fatalf("easy fills past its last note %+v", f)
	}
}

func TestYargBounds(t *testing.T) {
	list := []timed{{0, 0}, {10, 0}, {10, 0}, {20, 0}}
	for _, c := range []struct{ tick, lower, upper int }{
		{-5, -1, 0}, {0, 0, 1}, {5, 0, 1}, {10, 1, 3}, {15, 2, 3}, {20, 3, 4}, {25, 3, 4},
	} {
		if got := lowerBound(len(list), byTick(list, c.tick)); got != c.lower {
			t.Errorf("lowerBound(%d) = %d, want %d", c.tick, got, c.lower)
		}
		if got := upperBound(len(list), byTick(list, c.tick)); got != c.upper {
			t.Errorf("upperBound(%d) = %d, want %d", c.tick, got, c.upper)
		}
	}
	if lowerBound(0, nil) != -1 || upperBound(0, nil) != -1 {
		t.Error("empty list")
	}
}

// sweetVictory is a charted song with drum fills, read from the .sng file named by SWEETVICTORY_SNG;
// the test is skipped without it.
func sweetVictory(t *testing.T) *Chart {
	path := os.Getenv("SWEETVICTORY_SNG")
	if path == "" {
		t.Skip("set SWEETVICTORY_SNG to a Sweet Victory .sng file to run this test")
	}
	f, err := sng.Open(path)
	if err != nil {
		t.Skipf("no Sweet Victory chart: %v", err)
	}
	files := map[string][]byte{}
	for _, name := range []string{"notes.mid", "notes.chart"} {
		if data, err := f.Read(name); err == nil {
			files[name] = data
		}
	}
	ch, err := Load(files, f.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func TestGeneratedFillsSweetVictory(t *testing.T) {
	ch := sweetVictory(t)
	charted := map[string][]Phrase{}
	for _, d := range Difficulties {
		charted[d] = ch.DrumFills(d)
	}
	if len(charted["expert"]) == 0 {
		t.Fatal("Sweet Victory has no charted fills")
	}
	// Strip the fills and let YARG's rules make them.
	ch.drumFills = map[string][]tickRange{}
	ch.generateFills()
	measures := map[int]bool{}
	for _, m := range ch.measureLines() {
		measures[m.tick] = true
	}
	for _, d := range Difficulties {
		notes := ch.DrumNotes(d)
		if len(notes) == 0 {
			continue
		}
		fills := ch.DrumFills(d)
		if len(fills) == 0 {
			t.Fatalf("%s: no fills made", d)
		}
		acts := 0
		for _, n := range notes {
			if n.Activator {
				acts++
			}
		}
		if acts != len(fills) {
			t.Fatalf("%s: %d fills, %d activators", d, len(fills), acts)
		}
		ticks := map[int]bool{}
		for _, n := range notes {
			ticks[n.Tick] = ticks[n.Tick] || n.Activator
		}
		for _, f := range fills {
			// A fill starts on a bar and ends on its activator note.
			if !measures[f.Tick] || !ticks[f.EndTick] || f.EndTick <= f.Tick {
				t.Fatalf("%s: fill %+v", d, f)
			}
			for _, s := range ch.gen.stars[d] {
				if s.a < f.EndTick && f.Tick < s.b && f.Tick >= s.a {
					t.Fatalf("%s: fill %+v starts in star power %v", d, f, s)
				}
			}
		}
		t.Logf("%s: %d charted fills, %d made", d, len(charted[d]), len(fills))
	}
}
