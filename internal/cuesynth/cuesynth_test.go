package cuesynth

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
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

func round3(v float64) float64 { return math.RoundToEven(v*1000) / 1000 }

func TestPythonRandom(t *testing.T) {
	// python -c "import random; r=random.Random(52); print([r.random() for _ in range(3)])"
	r := newPyRandom(52)
	for _, want := range []float64{0.9783548583709967, 0.05452265501045739, 0.7256617211743249} {
		if got := r.random(); got != want {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if got := newPyRandom(0).random(); got != 0.8444218515250481 {
		t.Fatalf("seed 0: %v", got)
	}
	if got := newPyRandom(1<<40 + 7).random(); got != 0.6137037779936511 {
		t.Fatalf("two-word seed: %v", got)
	}
}

func TestReadSettings(t *testing.T) {
	dir := t.TempDir()
	ud := filepath.Join(dir, "UserData")
	if err := os.Mkdir(ud, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[Other]\nLookAheadMs = \"999\"\n[YargAccess]\nLookAheadMs = \"400\"\nCueRepeats = true\n" +
		"PlayMode = \"hear\"\nCallLength = \"1\"\nCallLengthBars = true\nCueVolume = 50.0\nVolume_green = 20.0\n"
	if err := os.WriteFile(filepath.Join(ud, "MelonPreferences.cfg"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	s := ReadSettings(dir)
	if s.LookAhead != 0.4 || !s.CueRepeats || s.ListenRepeat || s.CallLength != 1 {
		t.Fatalf("%+v", s)
	}
	if s.Master != 0.5 || s.Volume("green") != 0.2 || s.Volume("red") != 1.0 {
		t.Fatalf("%+v", s)
	}
	d := ReadSettings(filepath.Join(dir, "missing"))
	if d.LookAhead != 0.3 || d.CueRepeats || !d.ListenRepeat || d.CallLength != 2 {
		t.Fatalf("%+v", d)
	}
}

func TestSchedulePlacesCuesAhead(t *testing.T) {
	ch := chartfile.ReadChart(testChart, map[string]string{})
	notes := ch.Notes("guitar", "expert")
	s := DefaultSettings()
	bank := NewBank(9, true, s.LookAhead, false)
	plays := Schedule(notes, ch.Tempo, s, bank, nil, nil, 0.0, 30.0)
	found := false
	for _, p := range plays {
		if near(p.Start, 0.7) && p.Looped && near(p.Stop, 1.875) {
			found = true
		}
	}
	if !found {
		t.Fatal("no hold from 0.7 to 1.875")
	}
	release, chord := false, false
	for _, p := range plays {
		release = release || p.Sound == bank.Release && near(p.Start, 1.875)
		chord = chord || p.Sound == bank.Chord
	}
	if !release || !chord {
		t.Fatalf("release %v chord %v", release, chord)
	}
	track := NewCueTrack(plays, 3.0)
	whole := track.Render(0, track.Frames)
	if len(whole) != 2*track.Frames {
		t.Fatal("length")
	}
	peak := 0.0
	for _, v := range whole {
		peak = math.Max(peak, math.Abs(float64(v)))
	}
	if peak <= 0 {
		t.Fatal("silent")
	}
	var chunks []float32
	for f := 0; f < track.Frames; f += 1000 {
		chunks = append(chunks, track.Render(f, 1000)...)
	}
	for i := range whole {
		if math.Abs(float64(chunks[i]-whole[i])) > 1e-6 {
			t.Fatalf("chunked render differs at %d", i)
		}
	}
	at := 0.075
	first := track.Render(int(at*44100), 1000)
	l, r := 0.0, 0.0
	for i := 0; i < len(first); i += 2 {
		l = math.Max(l, math.Abs(float64(first[i])))
		r = math.Max(r, math.Abs(float64(first[i+1])))
	}
	if l <= r {
		t.Fatalf("left %v right %v", l, r)
	}
}

func TestChartDrumsAndSounds(t *testing.T) {
	ch := chartfile.ReadChart(drumChart, map[string]string{})
	notes := ch.DrumNotes("expert")
	type ds struct {
		id  string
		pan float64
	}
	for _, c := range []struct {
		lane   int
		cymbal bool
		mode   string
		want   ds
	}{
		{2, true, chartfile.PRO, ds{"cymyellow", -0.25}},
		{2, true, chartfile.FOUR, ds{"padyellow", -0.25}},
		{4, false, chartfile.FIVE, ds{"cymgreen", 0.4}},
	} {
		id, pan := DrumSound(c.lane, c.cymbal, c.mode)
		if (ds{id, pan}) != c.want {
			t.Fatalf("DrumSound(%d, %v, %s) = %s %v", c.lane, c.cymbal, c.mode, id, pan)
		}
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.LookAhead, true)
	plays := ScheduleDrums(notes, ch.DrumMode, s, bank, nil, nil, nil)
	kick, repeat, ghost, whisper, accent, crack, marks := false, false, false, false, false, false, 0
	for _, p := range plays {
		kick = kick || p.Sound == bank.Pads["kick"] && near(p.Start, -0.2)
		repeat = repeat || p.Sound == bank.Repeat
		// The ghost (1.0 s) and the accent (1.5 s) play the Drums Look-ahead early, each with its mark
		// at the same time and pan.
		ghost = ghost || p.Sound == bank.Pads["padblue"] && near(p.Volume, 0.35) && near(p.Start, 0.8) && near(p.Pan, 0.25)
		whisper = whisper || p.Sound == bank.Ghost && near(p.Volume, 0.9) && near(p.Start, 0.8) && near(p.Pan, 0.25)
		accent = accent || p.Sound == bank.Pads["padgreen"] && near(p.Volume, 1.0) && near(p.Start, 1.3) && near(p.Pan, 0.75)
		crack = crack || p.Sound == bank.Accent && near(p.Volume, 0.9) && near(p.Start, 1.3) && near(p.Pan, 0.75)
		if p.Sound == bank.Ghost || p.Sound == bank.Accent {
			marks++
		}
		if p.Sound == bank.Pads["padyellow"] || p.Sound == bank.Pads["cymyellow"] || p.Sound == bank.Pads["padred"] {
			if !near(p.Volume, 0.75) {
				t.Fatalf("plain pad volume %v", p.Volume)
			}
		}
	}
	if !kick || repeat || !ghost || !whisper || !accent || !crack || marks != 2 {
		t.Fatalf("kick %v repeat %v ghost %v whisper %v accent %v crack %v marks %d", kick, repeat, ghost, whisper, accent, crack, marks)
	}
	beats := BeatTicks(ch.Tempo, s, bank, 2.1)
	want := []struct {
		t       float64
		measure bool
	}{{0.0, true}, {0.5, false}, {1.0, false}, {1.5, false}, {2.0, true}}
	for i, w := range want {
		if round3(beats[i].Start) != w.t || (beats[i].Sound == bank.Measure) != w.measure {
			t.Fatalf("beat %d: %v", i, beats[i])
		}
	}
}

// Every drum note plays its own pads: a repeating rhythm has no held tones, no
// "same again" tick, no hit bar riser or click.
func TestDrumRepeatsPlayEveryNote(t *testing.T) {
	H := chartfile.NewDrumHit
	var notes []*chartfile.DrumNote
	for i := 0; i < 8; i++ {
		notes = append(notes, &chartfile.DrumNote{Tick: i, Hits: []chartfile.DrumHit{H(0, false), H(1, false)},
			Time: 1.0 + 0.25*float64(i)})
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.LookAhead, true)
	for _, cue := range []bool{false, true} {
		s.CueRepeats = cue
		kicks := 0
		for _, p := range ScheduleDrums(notes, chartfile.PRO, s, bank, nil, nil, nil) {
			if p.Sound == bank.Pads["kick"] {
				kicks++
			}
			if p.Looped || p.Sound == bank.Riser || p.Sound == bank.Click || p.Sound == bank.Repeat {
				t.Fatalf("held, riser, click or repeat sound (Cue Repeated Notes %v)", cue)
			}
		}
		if kicks != 8 {
			t.Fatalf("%d kicks (Cue Repeated Notes %v)", kicks, cue)
		}
	}
}

func TestProkeysSchedule(t *testing.T) {
	notes := []*chartfile.KeyNote{
		{Tick: 0, Keys: []int{12, 16}, EndTicks: []int{480, 480}, Time: 1.0, End: 2.0, Safe: 1.9, Safes: []float64{1.9, 1.9}},
		{Tick: 480, Keys: []int{12, 16}, EndTicks: []int{480, 480}, Time: 2.1, End: 2.1, Safe: 2.1, Safes: []float64{2.1, 2.1}},
		{Tick: 960, Keys: []int{0}, EndTicks: []int{960}, Gliss: true, Time: 3.0, End: 3.0, Safe: 3.0, Safes: []float64{3.0}},
		{Tick: 990, Keys: []int{7}, EndTicks: []int{990}, Gliss: true, Time: 3.3, End: 3.3, Safe: 3.3, Safes: []float64{3.3}},
	}
	s := DefaultSettings()
	bank := NewBank(9, true, s.LookAhead, false)
	keys := NewKeyBank()
	plays := ScheduleProkeys(notes, s, bank, keys, nil, nil)
	var strikes []float64
	var holds []*Play
	at27 := 0
	for _, p := range plays {
		if p.Sound == keys.Strike(72) || p.Sound == keys.Strike(76) {
			strikes = append(strikes, round3(p.Start))
		}
		if p.Looped {
			holds = append(holds, p)
		}
		if near(p.Start, 2.7) && !p.Looped && p.Sound != bank.Riser {
			at27++
		}
	}
	if len(strikes) != 2 || strikes[0] != 0.7 || strikes[1] != 0.7 {
		t.Fatalf("strikes %v", strikes)
	}
	if len(holds) != 2 || !near(holds[0].Stop, 2.1) || !near(holds[1].Stop, 2.1) {
		t.Fatalf("holds %v", holds)
	}
	if !near(KeyPan(0), -0.9) || !near(KeyPan(24), 0.9) {
		t.Fatal("pans")
	}
	if at27 != 1 {
		t.Fatalf("%d plays at 2.7", at27)
	}
}
