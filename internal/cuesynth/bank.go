package cuesynth

import "math"

// Sound is one synthesised cue (mono, RATE Hz). Plays point at a Sound, so a test can tell which
// sound a Play uses by comparing pointers, and CueTrack converts each Sound to float32 only once.
type Sound struct {
	Samples []float64
}

func snd(d []float64) *Sound { return &Sound{d} }

// Fret ids (the cue volume names) and pans, open first, then Green..Orange.
var (
	FretIDs = []string{"open", "green", "red", "yellow", "blue", "orange"}
	Pans    = []float64{0.0, -0.9, -0.45, 0.0, 0.45, 0.9}
)

var (
	majorSteps = []int{0, 2, 4, 7, 9}
	minorSteps = []int{0, 3, 5, 7, 10}
)

// tapKnock is a tap's opening: two hard wooden knocks 50 ms apart, the second higher.
func tapKnock(f int) []float64 {
	low := Overlay(Glide(1100, 600, 0.05, "fall", 0.5), Noise(2600, 0.03, 0.0003, 0.006, 0.45, int64(71+f)), 0)
	high := Overlay(Glide(1500, 850, 0.05, "fall", 0.5), Noise(3400, 0.03, 0.0003, 0.006, 0.45, int64(77+f)), 0)
	return Overlay(low, high, 0.05)
}

// Bank is the preview's cue sounds for one key, with the riser for one Look-ahead. Every sound is in
// the key's pentatonic scale; the caller passes A minor when the key is unknown, as the mod does.
//
// A five-fret bank (drums false) has Strike, Hammer, Pull, Tap and Hold per fret (index 0 = open),
// Chord, Click and Release. A drums bank has Pads, Click, Ghost and Accent. Both have Riser, SPNote,
// SPStart, Beat, Measure and the Listen and Repeat swells, Listen and YourTurn.
type Bank struct {
	Strike, Hammer, Pull, Tap, Hold []*Sound
	Chord, Click, Release           *Sound

	Pads      map[string]*Sound
	Activator *Sound // drums: the star power hit, played with a fill's last note
	Ghost     *Sound // drums: the whisper played with a ghost note's pad
	Accent    *Sound // drums: the sharp crack played with an accent's pad
	padHz     map[string]float64
	rolls     map[string]*Sound
	fillRises map[int]*Sound
	top       float64

	Riser, SPNote, SPStart, Beat, Measure *Sound
	Listen, YourTurn                      *Sound
}

// NewBank builds every sound for a key: tonic 0..11 (C = 0), minor or major pentatonic.
func NewBank(tonic int, minor bool, riserSeconds float64, drums bool) *Bank {
	b := &Bank{padHz: map[string]float64{}, rolls: map[string]*Sound{}, fillRises: map[int]*Sound{}}
	steps := majorSteps
	if minor {
		steps = minorSteps
	}
	green := 69 + pyMod(tonic-9+12, 12)
	top := MidiToHz(float64(green + 12))
	b.top = top
	if !drums {
		for f := 0; f < 6; f++ {
			m := green - 12
			if f > 0 {
				m = green + steps[f-1]
			}
			hz := MidiToHz(float64(m))
			b.Strike = append(b.Strike, snd(Tone(hz, 0.30, 0.002, 0.10, 0.8, 0.5)))
			b.Hammer = append(b.Hammer, snd(Slur(hz/2, hz, 0.32, 0.10, 0.004, 0.10, 0.3, 0.5)))
			b.Pull = append(b.Pull, snd(Slur(hz*2, hz, 0.32, 0.10, 0.004, 0.10, 0.3, 0.5)))
			b.Tap = append(b.Tap, snd(Overlay(tapKnock(f), Tone(hz, 0.28, 0.004, 0.10, 0.5, 0.42), 0.05)))
			b.Hold = append(b.Hold, snd(Loop(hz, 0.5, 0.2)))
		}
		var chord []float64
		for k, c := range []float64{1500, 2200, 3200, 4600, 6500} {
			chord = Overlay(chord, Noise(c, 0.04, 0.0005, 0.01, 1.0, int64(51+k)), 0.012*float64(k))
		}
		b.Chord = snd(Level(chord, 0.55))
		b.Click = snd(Noise(1200, 0.04, 0.0005, 0.008, 0.5, 52))
		b.Release = snd(Sequence([]float64{top * 1.5, top}, 0.07, 0.16, 0.6, 0.5))
	} else {
		b.buildDrums(green, steps)
	}
	b.Riser = snd(Glide(top/4, top, riserSeconds, "rise", 0.3))
	b.SPNote = snd(Sequence([]float64{top * 2, top * 3}, 0.03, 0.10, 0, 0.22))
	b.SPStart = snd(Sequence([]float64{top, top * 1.5, top * 2, top * 3}, 0.045, 0.14, 0.3, 0.35))
	b.Beat = snd(Noise(650, 0.05, 0.0005, 0.012, 0.3, 63))
	b.Measure = snd(Overlay(Noise(650, 0.06, 0.0005, 0.015, 0.45, 64), Noise(1300, 0.03, 0.0005, 0.008, 0.2, 65), 0))
	b.Listen = snd(NoiseSlide(2400, 500, 0.4, 0.4, 0.05, 0.06, 0.35, 171))
	b.YourTurn = snd(NoiseRise(500, 3200, 0.4, 0.35, 172))
	return b
}

// buildDrums is CueBank.BuildDrums: pads on the pentatonic steps above the root, cymbals the same
// steps an octave up with a shimmer, the kick the root an octave down.
func (b *Bank) buildDrums(root int, steps []int) {
	pad := func(midi int, bright float64) []float64 {
		return Tone(MidiToHz(float64(midi)), 0.22, 0.002, 0.07, bright, 0.5)
	}
	b.Pads = map[string]*Sound{}
	b.Pads["kick"] = snd(pad(root-12, 0.9))
	b.padHz["kick"] = MidiToHz(float64(root - 12))
	for i, pid := range []string{"padred", "padyellow", "padblue", "padgreen", "padgreen5"} {
		b.Pads[pid] = snd(pad(root+steps[i], 0.8))
		b.padHz[pid] = MidiToHz(float64(root + steps[i]))
	}
	for i, cid := range []string{"cymyellow", "cymblue", "cymgreen"} {
		shimmer := Noise(7000, 0.12, 0.001, 0.04, 0.12, 110+int64(i))
		body := Tone(MidiToHz(float64(root+steps[i+1]+12)), 0.4, 0.002, 0.14, 0.4, 0.42)
		b.Pads[cid] = snd(Overlay(body, shimmer, 0))
		b.padHz[cid] = MidiToHz(float64(root + steps[i+1] + 12))
	}
	b.Click = snd(Noise(1200, 0.04, 0.0005, 0.008, 0.5, 52))
	top := MidiToHz(float64(root + 12))
	b.Activator = snd(Overlay(Sequence([]float64{top * 1.5, top * 2, top * 3}, 0.035, 0.12, 0.3, 0.35),
		Glide(top, top*4, 0.18, "rise", 0.25), 0.1))
	// Ghost note: a breathy whisper. Accent: a short bright crack on top of the pad.
	b.Ghost = snd(Level(Overlay(Noise(3200, 0.2, 0.03, 0.07, 0.5, 151),
		Noise(6500, 0.18, 0.03, 0.06, 0.35, 152), 0), 0.45))
	b.Accent = snd(Level(Overlay(Overlay(Noise(2400, 0.07, 0.0005, 0.018, 0.9, 153),
		Noise(7000, 0.05, 0.0005, 0.012, 0.7, 154), 0),
		Tone(1760, 0.06, 0.0005, 0.015, 0.9, 0.5), 0), 0.9))
}

// Roll is CueBank.RollLoop: half a second of a pad struck 12 times a second (a quick, quiet strike of
// the pad's note), to loop; late puts the strikes half a strike later, so two pads alternate (a swell).
// The same *Sound every time for one pad and offset.
func (b *Bank) Roll(padID string, late bool) *Sound {
	key := padID
	if late {
		key += "+"
	}
	if s, ok := b.rolls[key]; ok {
		return s
	}
	const period = 1.0 / 12
	n := RATE / 2
	d := make([]float64, n)
	hz, ok := b.padHz[padID]
	if !ok {
		hz = 440
	}
	strike := Tone(hz, 0.07, 0.001, 0.025, 0.6, 0.45)
	off := 0.0
	if late {
		off = 0.5
	}
	for k := 0; k < 6; k++ {
		at := int((float64(k) + off) * period * RATE)
		for i, v := range strike {
			d[(at+i)%n] += v
		}
	}
	s := snd(d)
	b.rolls[key] = s
	return s
}

// FillRise is CueBank.FillRise as CueBank.PlayFill makes it: a slow rise over seconds (0.5 to 6,
// to the millisecond) ending on the key's top note, with a little noise.
func (b *Bank) FillRise(seconds float64) *Sound {
	ms := int(math.RoundToEven(pmax(0.5, pmin(6.0, seconds)) * 1000))
	if s, ok := b.fillRises[ms]; ok {
		return s
	}
	sec := float64(ms) / 1000
	s := snd(Overlay(Glide(b.top/2, b.top, sec, "rise", 0.25), NoiseRise(800, 3000, sec, 0.08, 109), 0))
	b.fillRises[ms] = s
	return s
}

// KeyBank is each pro key's strike and held tone (CueBank.KeyStrike / KeyHold), made as they are
// needed and kept.
type KeyBank struct {
	strike, hold map[int]*Sound
}

// NewKeyBank is an empty KeyBank.
func NewKeyBank() *KeyBank {
	return &KeyBank{strike: map[int]*Sound{}, hold: map[int]*Sound{}}
}

// Strike is a key's struck tone; the same *Sound every time for one pitch.
func (k *KeyBank) Strike(midi int) *Sound {
	s, ok := k.strike[midi]
	if !ok {
		s = snd(Tone(MidiToHz(float64(midi)), 0.32, 0.002, 0.11, 0.7, 0.5))
		k.strike[midi] = s
	}
	return s
}

// Hold is a key's held tone; the same *Sound every time for one pitch.
func (k *KeyBank) Hold(midi int) *Sound {
	s, ok := k.hold[midi]
	if !ok {
		s = snd(Loop(MidiToHz(float64(midi)), 0.5, 0.18))
		k.hold[midi] = s
	}
	return s
}

// Gliss is CueBank.Gliss: the white keys from one note to the other, quick and light, over seconds.
func Gliss(fromMidi, toMidi int, seconds float64) []float64 {
	step := 1
	if toMidi < fromMidi {
		step = -1
	}
	var hz []float64
	for m := fromMidi; m != toMidi+step; m += step {
		switch pyMod(m, 12) {
		case 1, 3, 6, 8, 10:
			continue
		}
		hz = append(hz, MidiToHz(float64(m)))
	}
	if len(hz) == 0 {
		hz = []float64{MidiToHz(float64(fromMidi))}
	}
	spacing := 0.05
	if len(hz) > 1 {
		spacing = seconds / float64(len(hz)-1)
	}
	return Sequence(hz, spacing, 0.12, 0.6, 0.4)
}
