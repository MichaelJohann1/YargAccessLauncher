package cuesynth

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/sng"
)

// eighthsChart is 24 single Green eighth notes from the second bar, 120 BPM.
func eighthsChart() string {
	var b strings.Builder
	b.WriteString("[Song]\n{\n  Resolution = 192\n}\n[SyncTrack]\n{\n  0 = TS 4\n  0 = B 120000\n}\n[ExpertSingle]\n{\n")
	for i := 0; i < 24; i++ {
		fmt.Fprintf(&b, "  %d = N 0 0\n", 768+96*i)
	}
	b.WriteString("}\n")
	return b.String()
}

// checkCalls checks what the mod promises about a cut part: each listen and answer starts on a bar
// line, each call is its note moved back by the listen's length, no call overlaps any answer, and
// groups come in call order.
func checkCalls(t *testing.T, c *Calls, tempo *chartfile.TempoMap, ticks []int, calls []*Call) {
	t.Helper()
	onBar := map[int]bool{}
	for _, b := range barTicks(tempo, 0, ticks[len(ticks)-1], 20) {
		onBar[b] = true
	}
	for i, call := range calls {
		g := call.Group
		shift := g.StartTick - g.CallStartTick
		if shift <= 0 || !onBar[g.StartTick] || !onBar[g.CallStartTick] {
			t.Fatalf("note %d: listen at tick %d or answer at %d is not on a bar line", i, g.CallStartTick, g.StartTick)
		}
		if math.Abs(call.Time-tempo.Time(ticks[i]-shift)) > 1e-9 {
			t.Fatalf("note %d: call at %v, want %v", i, call.Time, tempo.Time(ticks[i]-shift))
		}
	}
	for i, g := range c.Groups {
		if i > 0 && g.CallStart < c.Groups[i-1].CallStart {
			t.Fatal("groups out of call order")
		}
		if g.CallEnd > g.Start {
			t.Fatalf("group %d: call ends at %v, after its answer starts at %v", i, g.CallEnd, g.Start)
		}
		for j, h := range c.Groups {
			if g.CallStart < h.End && h.Start < g.CallEnd {
				t.Fatalf("call %d (%v..%v) overlaps answer %d (%v..%v)", i, g.CallStart, g.CallEnd, j, h.Start, h.End)
			}
		}
	}
}

func TestListenRepeatPlan(t *testing.T) {
	ch := chartfile.ReadChart(eighthsChart(), map[string]string{})
	notes := ch.Notes("guitar", "expert")
	kept, c := CutNotes(notes, ch.Tempo, 2)
	// Two-bar windows: the notes start in bar 1, which is in the first listen (bars 0 and 1) and taken
	// out; the answer is bars 2 and 3 (16 notes), its call two bars (4 s) earlier.
	if len(c.Groups) != 1 || len(kept) != 16 || c.Kept != 16 || c.Total != 24 {
		t.Fatalf("%d groups, %d kept", len(c.Groups), len(kept))
	}
	var ticks []int
	var calls []*Call
	for _, n := range kept {
		ticks = append(ticks, n.Tick)
		calls = append(calls, c.Of(n))
	}
	checkCalls(t, c, ch.Tempo, ticks, calls)
	for i, n := range kept {
		if !near(n.Time-c.Of(n).Time, 4) {
			t.Fatalf("note %d: call %v before its hit", i, n.Time-c.Of(n).Time)
		}
	}
	if c.Of(notes[0]) != nil {
		t.Fatal("the original notes have no calls; the kept copies do")
	}

	s := DefaultSettings()
	bank := NewBank(9, true, s.LookAhead, false)
	plays := Schedule(kept, ch.Tempo, s, bank, c, nil, 0, math.Inf(1))
	var strikes, clicks []float64
	for _, p := range plays {
		switch p.Sound {
		case bank.Strike[1]:
			strikes = append(strikes, round3(p.Start))
		case bank.Click:
			clicks = append(clicks, round3(p.Start))
		case bank.Riser:
			t.Fatal("a riser in Listen and Repeat")
		}
	}
	if len(strikes) != 16 || len(clicks) != 16 {
		t.Fatalf("%d strikes, %d clicks", len(strikes), len(clicks))
	}
	for i, n := range kept {
		if strikes[i] != round3(n.Time-4) || clicks[i] != round3(n.Time) {
			t.Fatalf("note %d: strike %v click %v, hit %v", i, strikes[i], clicks[i], n.Time)
		}
	}
	sw := c.Swells(s, bank)
	if len(sw) != 2 || !near(sw[0].Start+SwellLength, c.Groups[0].CallStart) || !near(sw[1].Start+SwellLength, c.Groups[0].Start) ||
		sw[0].Sound != bank.Listen || sw[1].Sound != bank.YourTurn {
		t.Fatalf("swells %v", sw)
	}

	// Play What You Hear is unchanged: every note, a Look-ahead early.
	all := Schedule(notes, ch.Tempo, s, bank, nil, nil, 0, math.Inf(1))
	n := 0
	for _, p := range all {
		if p.Sound == bank.Strike[1] {
			n++
		}
	}
	if n != 1 { // one run of the same fret: only its first note sounds
		t.Fatalf("%d strikes without calls", n)
	}
}

func TestListenRepeatStarPowerMoves(t *testing.T) {
	ch := chartfile.ReadChart(eighthsChart(), map[string]string{})
	notes := ch.Notes("guitar", "expert")
	// A phrase over notes 4..12: notes 0..7 (bar 1) are taken out, so its first kept note is 8.
	for i := 4; i <= 12; i++ {
		notes[i].Star = true
	}
	notes[4].StarStart = true
	kept, _ := CutNotes(notes, ch.Tempo, 2)
	if len(kept) == 0 {
		t.Fatal("nothing kept")
	}
	for _, n := range kept {
		want := n.Tick == notes[8].Tick
		if n.StarStart != want {
			t.Fatalf("tick %d: star start %v", n.Tick, n.StarStart)
		}
	}
	if notes[8].StarStart || !notes[4].StarStart {
		t.Fatal("the chart's own notes changed")
	}
}

func TestListenRepeatDrumsAndKeys(t *testing.T) {
	ch := chartfile.ReadChart(eighthsChart(), map[string]string{})
	H := chartfile.NewDrumHit
	var drums []*chartfile.DrumNote
	var keys []*chartfile.KeyNote
	for _, n := range ch.Notes("guitar", "expert") {
		drums = append(drums, &chartfile.DrumNote{Tick: n.Tick, Hits: []chartfile.DrumHit{H(0, false), H(1, false)}, Time: n.Time})
		keys = append(keys, &chartfile.KeyNote{Tick: n.Tick, Keys: []int{12}, EndTicks: []int{n.Tick}, Time: n.Time,
			End: n.Time, Safe: n.Time, Safes: []float64{n.Time}, Gliss: true})
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.DrumLookAhead, true)
	kd, cd := CutDrums(drums, ch.Tempo, 2)
	kicks := 0
	for _, p := range ScheduleDrums(kd, chartfile.PRO, s, bank, cd, nil, nil) {
		if p.Looped || p.Sound == bank.Repeat || p.Sound == bank.Riser || p.Sound == bank.Click {
			t.Fatal("a run, repeat tick or hit bar sound in drum calls")
		}
		if p.Sound == bank.Pads["kick"] {
			kicks++
		}
	}
	if kicks != len(kd) || len(kd) != 16 {
		t.Fatalf("%d kicks for %d notes", kicks, len(kd))
	}

	kb := NewKeyBank()
	kbank := NewBank(9, true, s.LookAhead, false)
	kk, ck := CutKeys(keys, ch.Tempo, 2)
	strikes, clicks := 0, 0
	for _, p := range ScheduleProkeys(kk, s, kbank, kb, ck, nil) {
		switch p.Sound {
		case kb.Strike(72):
			strikes++
		case kbank.Click:
			clicks++
		case kbank.Riser:
			t.Fatal("a riser in pro keys calls")
		}
	}
	// Glissando notes in a call play as plain notes.
	if strikes != 16 || clicks != 16 {
		t.Fatalf("%d strikes, %d clicks", strikes, clicks)
	}
}

// TestListenRepeatChartFile cuts a real chart and checks the calls. Set YARGACCESS_LR_SNG to an .sng
// (a setlist .yargsong decoded to .sng works); it logs the counts to compare with the mod's log.
func TestListenRepeatChartFile(t *testing.T) {
	path := os.Getenv("YARGACCESS_LR_SNG")
	if path == "" {
		t.Skip("YARGACCESS_LR_SNG not set")
	}
	pkg, err := sng.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, n := range []string{"notes.mid", "notes.chart"} {
		if _, ok := pkg.Files[n]; ok {
			if files[n], err = pkg.Read(n); err != nil {
				t.Fatal(err)
			}
		}
	}
	ch, err := chartfile.Load(files, pkg.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	s := DefaultSettings()
	for _, size := range []int{2, 4, 8} {
		for _, d := range []string{"easy", "medium", "hard", "expert"} {
			notes := ch.Notes("guitar", d)
			if len(notes) > 0 {
				kept, c := CutNotes(notes, ch.Tempo, size)
				var ticks []int
				var calls []*Call
				for _, n := range kept {
					ticks, calls = append(ticks, n.Tick), append(calls, c.Of(n))
				}
				checkCalls(t, c, ch.Tempo, ticks, calls)
				bank := NewBank(9, true, s.LookAhead, false)
				checkNoSoundInAnswers(t, Schedule(kept, ch.Tempo, s, bank, c, nil, 0, math.Inf(1)), c, bank)
				t.Logf("guitar %s, length %d: %d of %d notes kept in %d groups", d, size, c.Kept, c.Total, len(c.Groups))
			}
			drums := ch.DrumNotes(d)
			if len(drums) > 0 {
				kept, c := CutDrums(drums, ch.Tempo, size)
				var ticks []int
				var calls []*Call
				for _, n := range kept {
					ticks, calls = append(ticks, n.Tick), append(calls, c.Of(n))
				}
				checkCalls(t, c, ch.Tempo, ticks, calls)
				t.Logf("drums (%s) %s, length %d: %d of %d notes kept in %d groups", ch.DrumMode, d, size, c.Kept, c.Total, len(c.Groups))
			}
		}
	}
}

// checkNoSoundInAnswers checks that no note sound starts and no held tone lasts inside an answer.
func checkNoSoundInAnswers(t *testing.T, plays []*Play, c *Calls, bank *Bank) {
	t.Helper()
	note := map[*Sound]bool{}
	for _, list := range [][]*Sound{bank.Strike, bank.Hammer, bank.Pull, bank.Tap, bank.Hold} {
		for _, s := range list {
			note[s] = true
		}
	}
	note[bank.Chord] = true
	for _, p := range plays {
		if !note[p.Sound] {
			continue
		}
		end := p.Start
		if p.Looped {
			end = p.Stop
		}
		for _, g := range c.Groups {
			if p.Start <= g.End && end >= g.Start {
				t.Fatalf("note sound %v..%v inside answer %v..%v", p.Start, end, g.Start, g.End)
			}
		}
	}
}

func TestListenRepeatProkeysHold(t *testing.T) {
	ch := chartfile.ReadChart(eighthsChart(), map[string]string{})
	// One held key in bar 2, a beat (192 ticks, 0.5 s) long: with two-bar windows the call is two bars
	// early, its tone stops a quarter beat before the end, shifted back with the call.
	n := &chartfile.KeyNote{Tick: 1536, Keys: []int{12}, EndTicks: []int{1728}}
	n.Time, n.End = ch.Tempo.Time(1536), ch.Tempo.Time(1728)
	n.Safe = ch.Tempo.Time(1728 - 48)
	n.Safes = []float64{n.Safe}
	kept, c := CutKeys([]*chartfile.KeyNote{n}, ch.Tempo, 2)
	if len(kept) != 1 {
		t.Fatalf("%d kept", len(kept))
	}
	call := c.Of(kept[0]).Time
	s := DefaultSettings()
	kb := NewKeyBank()
	var holds []*Play
	for _, p := range ScheduleProkeys(kept, s, NewBank(9, true, s.LookAhead, false), kb, c, nil) {
		if p.Looped {
			holds = append(holds, p)
		}
	}
	if len(holds) != 1 || holds[0].Sound != kb.Hold(72) || !near(holds[0].Start, call) ||
		!near(holds[0].Stop, n.Safe-(n.Time-call)) || holds[0].Stop >= n.Time-0.05+1e-9 {
		t.Fatalf("call at %v, holds %+v", call, holds)
	}
}
