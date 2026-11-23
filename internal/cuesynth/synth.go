// Package cuesynth rebuilds the mod's gameplay sounds so the launcher can preview a chart before it
// is downloaded into the game. It is a port of the mod's Audio/Synth.cs and Audio/CueBank.cs (the
// sound recipes), Gameplay/HighwaySound.cs, Gameplay/DrumSound.cs and Gameplay/ProKeysSound.cs (when
// each sound plays), Gameplay/ListenRepeat.cs (the calls) and Gameplay/BeatTicks.cs, by way of the
// Python launcher's cuesynth.py. Change those and this together.
//
// Only the sounds a player hears before pressing anything are made: the note and pad sounds, held
// tones, the chord marker, the hit bar riser or click, the Let go chimes,
// the star power phrase start and sparkle, the Listen and Repeat swells, and the beat ticks.
// Hit, miss and star power level sounds depend on the playing, so the preview has none.
//
// CueTrack mixes them into a stereo float stream at 44100 Hz, a chunk at a time. Pans follow BASS for
// a mono sample: the far side gets quieter, the near side stays at full level.
package cuesynth

import "math"

// RATE is the sample rate of every sound and of CueTrack, in Hz.
const RATE = 44100

// MidiToHz is a MIDI note's frequency (A4 = 69 = 440 Hz).
func MidiToHz(midi float64) float64 {
	return 440.0 * math.Pow(2.0, (midi-69.0)/12.0)
}

func frames(seconds float64) int {
	return max(1, int(math.Trunc(RATE*seconds)))
}

func wave(phase, bright float64) float64 {
	return (math.Sin(phase) + bright*0.35*math.Sin(2*phase) + bright*0.12*math.Sin(3*phase)) / (1 + bright*0.47)
}

func normalize(d []float64, peak float64) {
	m := 0.0
	for _, v := range d {
		m = pmax(m, math.Abs(v))
	}
	if m > 0 {
		k := peak / m
		for i := range d {
			d[i] *= k
		}
	}
}

// finish scales to the peak and fades the last 5 ms out (at most half the sound).
func finish(d []float64, peak float64) []float64 {
	normalize(d, peak)
	fade := min(len(d)/2, RATE/200)
	for i := 0; i < fade; i++ {
		d[len(d)-1-i] *= float64(i) / float64(fade)
	}
	return d
}

// Tone is a decaying tone with a little second and third harmonic (bright 0..1).
func Tone(f, seconds, attack, decay, bright, peak float64) []float64 {
	n := frames(seconds)
	w := 2 * math.Pi * f
	d := make([]float64, n)
	for i := range d {
		t := float64(i) / RATE
		env := math.Exp(-t / decay)
		if attack > 0 && t < attack {
			env *= t / attack
		}
		d[i] = env * wave(w*t, bright)
	}
	return finish(d, peak)
}

// Loop is one second of a steady tone that loops without a click (its frequency rounded to whole Hz).
func Loop(f, bright, peak float64) []float64 {
	n := RATE
	k := max(1, int(math.RoundToEven(f)))
	d := make([]float64, n)
	for i := range d {
		d[i] = wave(2*math.Pi*float64(k)*float64(i)/float64(n), bright)
	}
	normalize(d, peak)
	return d
}

// BandPass is the RBJ cookbook band-pass (constant 0 dB peak gain).
type BandPass struct {
	x1, x2, y1, y2 float64
	b0, b2, a1, a2 float64
}

// NewBandPass is a band-pass at f Hz with quality q.
func NewBandPass(f, q float64) *BandPass {
	bp := &BandPass{}
	bp.Tune(f, q)
	return bp
}

// Tune moves the filter, keeping its state.
func (bp *BandPass) Tune(f, q float64) {
	w := 2 * math.Pi * pmin(f, RATE*0.45) / RATE
	alpha := math.Sin(w) / (2 * q)
	a0 := 1 + alpha
	bp.b0, bp.b2 = alpha/a0, -alpha/a0
	bp.a1, bp.a2 = -2*math.Cos(w)/a0, (1-alpha)/a0
}

// Next filters one sample.
func (bp *BandPass) Next(x float64) float64 {
	y := bp.b0*x + bp.b2*bp.x2 - bp.a1*bp.y1 - bp.a2*bp.y2
	bp.x2, bp.x1, bp.y2, bp.y1 = bp.x1, x, bp.y1, y
	return y
}

// Noise is band-passed white noise around centre Hz, decaying. seed fixes the noise.
func Noise(centre, seconds, attack, decay, peak float64, seed int64) []float64 {
	n := frames(seconds)
	bp := NewBandPass(centre, 3.0)
	rnd := newPyRandom(seed)
	d := make([]float64, n)
	for i := range d {
		t := float64(i) / RATE
		env := math.Exp(-t / decay)
		if attack > 0 && t < attack {
			env *= t / attack
		}
		d[i] = bp.Next(rnd.random()*2-1) * env
	}
	return finish(d, peak)
}

// Glide sweeps a sine from f0 to f1. shape: "rise" (swelling), "thud" (falling with a sub-octave),
// anything else falls away.
func Glide(f0, f1, seconds float64, shape string, peak float64) []float64 {
	n := frames(seconds)
	d := make([]float64, n)
	phase := 0.0
	for i := range d {
		x := float64(i) / float64(n)
		phase += 2 * math.Pi * f0 * math.Pow(f1/f0, x) / RATE
		var env float64
		if shape == "rise" {
			env = 0.15 + 0.85*x*x
		} else {
			env = math.Exp(-4.0 * x)
		}
		v := math.Sin(phase) * env
		if shape == "thud" {
			v += math.Sin(phase*0.5) * 0.5 * env
		}
		d[i] = v
	}
	return finish(d, peak)
}

// Slur is a tone that slides from f0 to f1 over glideS seconds, then holds f1 as it decays.
func Slur(f0, f1, seconds, glideS, attack, decay, bright, peak float64) []float64 {
	n := frames(seconds)
	d := make([]float64, n)
	phase := 0.0
	for i := range d {
		t := float64(i) / RATE
		f := f1
		if t < glideS {
			f = f0 * math.Pow(f1/f0, t/glideS)
		}
		phase += 2 * math.Pi * f / RATE
		env := math.Exp(-t / decay)
		if attack > 0 && t < attack {
			env *= t / attack
		}
		d[i] = env * wave(phase, bright)
	}
	return finish(d, peak)
}

// Overlay mixes b into a copy of a, offsetSeconds in, lengthening it as needed.
func Overlay(a, b []float64, offsetSeconds float64) []float64 {
	off := int(math.Trunc(offsetSeconds * RATE))
	d := make([]float64, len(a)+max(0, off+len(b)-len(a)))
	copy(d, a)
	for i, v := range b {
		d[off+i] += v
	}
	return d
}

// Sequence is a run of short tones, spacing seconds apart.
func Sequence(hz []float64, spacing, each, bright, peak float64) []float64 {
	var d []float64
	for i, f := range hz {
		d = Overlay(d, Tone(f, each, 0.002, each/3, bright, 1.0), spacing*float64(i))
	}
	return finish(d, peak)
}

// NoiseRise is noise sweeping its centre from c0 to c1 as it swells (the Listen and Repeat "your
// turn" swell).
func NoiseRise(c0, c1, seconds, peak float64, seed int64) []float64 {
	n := frames(seconds)
	rnd := newPyRandom(seed)
	bp := NewBandPass(c0, 4.0)
	d := make([]float64, n)
	for i := range d {
		x := float64(i) / float64(n)
		if i%64 == 0 {
			bp.Tune(c0*math.Pow(c1/c0, x), 4.0)
		}
		d[i] = bp.Next(rnd.random()*2-1) * (0.15 + 0.85*x*x)
	}
	return finish(d, peak)
}

// NoiseSlide is noise sweeping its brightness from c0 to c1, then dying away (the Listen and Repeat
// "listen" swell).
func NoiseSlide(c0, c1, seconds, glideS, attack, decay, peak float64, seed int64) []float64 {
	n := frames(seconds)
	rnd := newPyRandom(seed)
	bp := NewBandPass(c0, 6.0)
	d := make([]float64, n)
	for i := range d {
		t := float64(i) / RATE
		if i%32 == 0 {
			c := c1
			if t < glideS {
				c = c0 * math.Pow(c1/c0, t/glideS)
			}
			bp.Tune(c, 6.0)
		}
		env := 1.0
		if t >= glideS {
			env = math.Exp(-(t - glideS) / decay)
		}
		if attack > 0 && t < attack {
			env *= t / attack
		}
		d[i] = bp.Next(rnd.random()*2-1) * env
	}
	return finish(d, peak)
}

// Level is a copy of d scaled to the peak, with the 5 ms fade out.
func Level(d []float64, peak float64) []float64 {
	return finish(append([]float64(nil), d...), peak)
}
