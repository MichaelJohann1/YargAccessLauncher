package cuesynth

import (
	"math"
	"sort"
)

type trackItem struct {
	s0, end int // frames [s0, end) on the song timeline
	data    []float32
	looped  bool
	fin     int // fade-in frames, 0 for none
	hasStop bool
	stopI   int // frame (from s0) the fade out starts
	fout    int
	gainL   float64
	gainR   float64
}

// CueTrack is the whole chart's sounds as a stereo stream, mixed a chunk at a time as it plays (a
// full song rendered at once would be hundreds of megabytes). Frame f is song time f / RATE. Render
// is safe to call from another goroutine once built (it only reads the track).
type CueTrack struct {
	Frames  int
	items   []trackItem
	starts  []int
	longest int
}

// NewCueTrack places the plays on a timeline seconds long. Silent plays are dropped.
func NewCueTrack(plays []*Play, seconds float64) *CueTrack {
	t := &CueTrack{Frames: int(math.Trunc(seconds * RATE))}
	arrays := map[*Sound][]float32{}
	for _, p := range plays {
		if p.Volume <= 0 {
			continue
		}
		data, ok := arrays[p.Sound]
		if !ok {
			data = make([]float32, len(p.Sound.Samples))
			for i, v := range p.Sound.Samples {
				data[i] = float32(v)
			}
			arrays[p.Sound] = data
		}
		s0 := int(math.RoundToEven(p.Start * RATE))
		var length int
		if p.Looped {
			stop := p.Start + 1
			if p.HasStop {
				stop = p.Stop
			}
			length = int(math.Trunc((stop - p.Start + p.FadeOut) * RATE))
		} else {
			length = len(data)
			if p.HasStop {
				length = min(length, int(math.Trunc((p.Stop-p.Start+p.FadeOut)*RATE)))
			}
		}
		if length <= 0 || len(data) == 0 {
			continue
		}
		it := trackItem{s0: s0, end: s0 + length, data: data, looped: p.Looped}
		if p.FadeIn > 0 {
			it.fin = int(math.Trunc(p.FadeIn * RATE))
		}
		if p.HasStop {
			it.hasStop = true
			it.stopI = int(math.Trunc((p.Stop - p.Start) * RATE))
			it.fout = max(1, int(math.Trunc(p.FadeOut*RATE)))
		}
		g := p.Volume
		it.gainL, it.gainR = g, g
		if p.Pan > 0 {
			it.gainL = g * (1.0 - p.Pan)
		}
		if p.Pan < 0 {
			it.gainR = g * (1.0 + p.Pan)
		}
		t.items = append(t.items, it)
	}
	sort.SliceStable(t.items, func(i, j int) bool { return t.items[i].s0 < t.items[j].s0 })
	t.starts = make([]int, len(t.items))
	for i, it := range t.items {
		t.starts[i] = it.s0
		t.longest = max(t.longest, it.end-it.s0)
	}
	return t
}

// Render is frames first .. first + count as interleaved float32 (left, right, left, right ...).
func (t *CueTrack) Render(first, count int) []float32 {
	out := make([]float32, 2*count)
	t.RenderInto(out, first)
	return out
}

// RenderInto mixes len(dst) / 2 frames from first into dst (interleaved stereo), overwriting it. It
// does not allocate, so it can run in an audio callback. The result is the same whether a stretch is
// rendered whole or in chunks.
func (t *CueTrack) RenderInto(dst []float32, first int) {
	count := len(dst) / 2
	for i := range dst {
		dst[i] = 0
	}
	last := first + count
	lo := sort.SearchInts(t.starts, first-t.longest)
	hi := sort.SearchInts(t.starts, last)
	for idx := lo; idx < hi; idx++ {
		it := &t.items[idx]
		if it.end <= first {
			continue
		}
		a, b := max(first, it.s0), min(last, it.end)
		n := len(it.data)
		k := a - it.s0
		di := k
		if it.looped {
			di = k % n
		}
		o := 2 * (a - first)
		for ; k < b-it.s0; k++ {
			v := float64(it.data[di])
			if it.fin > 0 {
				v *= math.Min(1.0, float64(k)/float64(it.fin))
			}
			if it.hasStop {
				f := 1.0 - float64(k-it.stopI)/float64(it.fout)
				if f < 0 {
					f = 0
				} else if f > 1 {
					f = 1
				}
				v *= f
			}
			dst[o] += float32(v * it.gainL)
			dst[o+1] += float32(v * it.gainR)
			o += 2
			di++
			if it.looped && di == n {
				di = 0
			}
		}
	}
}
