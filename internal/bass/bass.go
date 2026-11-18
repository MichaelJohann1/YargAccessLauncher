// Package bass plays the chart preview through the BASS library YARG itself ships.
//
// The launcher has no audio library of its own. YARG's install has bass.dll and bassopus.dll (charts
// are mostly Opus) in YARG_Data\Plugins\x86_64, so the preview loads those.
//
// A preview is the song's stems (from memory) plus the cue track, a stream BASS asks for a chunk at a
// time (CueTrack.Render). The cue stream is linked to the stems, so they start, pause and stop
// together. Seeking pauses everything, moves each stem, and makes a fresh cue stream at the new time.
package bass

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	bassUnicode      = 0x80000000
	bassSampleFloat  = 0x100
	bassSample8Bits  = 1
	streamProcEnd    = 0x80000000
	bassPosByte      = 0
	bassAttribVol    = 2
	bassErrorAlready = 14

	activeStopped = 0
	activePaused  = 3

	Rate = 44100
)

// Track is the cue stream's source: Frames frames of interleaved stereo float32.
type Track interface {
	TotalFrames() int
	Render(first, count int, out []float32)
}

var (
	loadMu sync.Mutex
	loaded bool
	procs  = map[string]*windows.LazyProc{}
)

func plugin(folder string) string { return filepath.Join(folder, "YARG_Data", "Plugins", "x86_64") }

func call(name string, args ...uintptr) uintptr {
	r, _, _ := procs[name].Call(args...)
	return r
}

func errorCode() int { return int(int32(call("BASS_ErrorGetCode"))) }

func load(gameDir string) error {
	loadMu.Lock()
	defer loadMu.Unlock()
	if loaded {
		return nil
	}
	folder := plugin(gameDir)
	path := filepath.Join(folder, "bass.dll")
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return errors.New("The preview plays through YARG's audio library, and it was not found. " +
			"Install YARG on the Game tab first.")
	}
	windows.SetDllDirectory(folder)
	dll := windows.NewLazyDLL(path)
	for _, n := range []string{"BASS_Init", "BASS_ErrorGetCode", "BASS_PluginLoad", "BASS_StreamCreateFile",
		"BASS_StreamCreate", "BASS_StreamFree", "BASS_ChannelPlay", "BASS_ChannelPause", "BASS_ChannelStop",
		"BASS_ChannelSetLink", "BASS_ChannelSetAttribute", "BASS_ChannelSeconds2Bytes", "BASS_ChannelGetInfo",
		"BASS_ChannelSetPosition", "BASS_ChannelGetPosition", "BASS_ChannelGetLength", "BASS_ChannelIsActive"} {
		p := dll.NewProc(n)
		if err := p.Find(); err != nil {
			return err
		}
		procs[n] = p
	}
	minusOne := int32(-1)
	if call("BASS_Init", uintptr(uint32(minusOne)), Rate, 0, 0, 0) == 0 && errorCode() != bassErrorAlready {
		return fmt.Errorf("Could not open the sound device (BASS error %d).", errorCode())
	}
	for _, name := range []string{"bassopus.dll", "bassflac.dll"} {
		p := filepath.Join(folder, name)
		if _, err := os.Stat(p); err == nil {
			w, _ := syscall.UTF16PtrFromString(p)
			call("BASS_PluginLoad", uintptr(unsafe.Pointer(w)), bassUnicode)
		}
	}
	loaded = true
	return nil
}

// channelInfo mirrors BASS_CHANNELINFO.
type channelInfo struct {
	freq, chans, flags, ctype, origres, plugin, sample uint32
	_                                                  uint32
	filename                                           uintptr
}

// bytesToSeconds is BASS_ChannelBytes2Seconds, which returns a double that a syscall cannot read.
func bytesToSeconds(h uint32, bytes uint64) float64 {
	var info channelInfo
	if call("BASS_ChannelGetInfo", uintptr(h), uintptr(unsafe.Pointer(&info))) == 0 || info.freq == 0 || info.chans == 0 {
		return 0
	}
	size := 2.0
	if info.flags&bassSampleFloat != 0 {
		size = 4
	} else if info.flags&bassSample8Bits != 0 {
		size = 1
	}
	return float64(bytes) / (float64(info.freq) * float64(info.chans) * size)
}

func seconds2Bytes(h uint32, s float64) uint64 {
	return uint64(call("BASS_ChannelSeconds2Bytes", uintptr(h), uintptr(math.Float64bits(s))))
}

func setVolume(h uint32, v float64) {
	call("BASS_ChannelSetAttribute", uintptr(h), bassAttribVol, uintptr(math.Float32bits(float32(v))))
}

// Player is one chart preview. Times are chart seconds; chart time t is stem position t + delay.
type Player struct {
	track      Track
	delay      float64
	SongVolume float64
	Length     float64

	mu     sync.Mutex
	stems  []uint32
	pinner runtime.Pinner
	bufs   [][]byte
	cue    uint32
	base   float64
	frame  int
	paused bool
}

// current is the player the shared stream callback fills.
var (
	current   *Player
	currentMu sync.Mutex
	proc      = syscall.NewCallback(fill)
)

func NewPlayer(gameDir string, track Track, stems [][]byte, delay, songVolume float64) (*Player, error) {
	if err := load(gameDir); err != nil {
		return nil, err
	}
	p := &Player{track: track, delay: delay, SongVolume: songVolume}
	audio := 0.0
	for _, data := range stems {
		if len(data) == 0 {
			continue
		}
		p.pinner.Pin(&data[0])
		h := uint32(call("BASS_StreamCreateFile", 1, uintptr(unsafe.Pointer(&data[0])), 0, uintptr(len(data)), bassSampleFloat))
		if h == 0 {
			continue
		}
		p.bufs = append(p.bufs, data)
		p.stems = append(p.stems, h)
		setVolume(h, songVolume)
		audio = math.Max(audio, bytesToSeconds(h, uint64(call("BASS_ChannelGetLength", uintptr(h), bassPosByte))))
	}
	p.Length = math.Max(float64(track.TotalFrames())/Rate, audio-delay)
	return p, nil
}

func (p *Player) StemCount() int { return len(p.stems) }

func fill(handle, buffer, length, user uintptr) uintptr {
	currentMu.Lock()
	p := current
	currentMu.Unlock()
	if p == nil {
		return streamProcEnd
	}
	frames := int(length) / 8
	end := int(p.Length * Rate)
	count := frames
	if end-p.frame < count {
		count = end - p.frame
	}
	if count < 0 {
		count = 0
	}
	if count > 0 {
		out := unsafe.Slice((*float32)(*(*unsafe.Pointer)(unsafe.Pointer(&buffer))), count*2)
		p.track.Render(p.frame, count, out)
	}
	p.frame += count
	r := uintptr(count * 8)
	if count < frames {
		r |= streamProcEnd
	}
	return r
}

// startAt puts every stream at chart time t (paused), then plays if asked.
func (p *Player) startAt(t float64, play bool) error {
	t = math.Max(0, math.Min(t, p.Length))
	if p.cue != 0 {
		call("BASS_ChannelPause", uintptr(p.cue))
		call("BASS_StreamFree", uintptr(p.cue))
		p.cue = 0
	}
	for _, h := range p.stems {
		call("BASS_ChannelPause", uintptr(h))
		pos := math.Max(0, t+p.delay)
		call("BASS_ChannelSetPosition", uintptr(h), uintptr(seconds2Bytes(h, pos)), bassPosByte)
	}
	p.base, p.frame = t, int(t*Rate)
	p.cue = uint32(call("BASS_StreamCreate", Rate, 2, bassSampleFloat, proc, 0))
	if p.cue == 0 {
		return fmt.Errorf("The sound device could not play the preview (BASS error %d).", errorCode())
	}
	for _, h := range p.stems {
		call("BASS_ChannelSetLink", uintptr(p.cue), uintptr(h))
	}
	p.paused = !play
	if play {
		call("BASS_ChannelPlay", uintptr(p.cue), 0)
	}
	return nil
}

func (p *Player) Start(t float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startAt(t, true)
}

func (p *Player) Seek(t float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.startAt(t, p.state() == "playing")
}

func (p *Player) Position() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cue == 0 {
		return 0
	}
	played := bytesToSeconds(p.cue, uint64(call("BASS_ChannelGetPosition", uintptr(p.cue), bassPosByte)))
	return math.Min(p.Length, p.base+math.Max(0, played))
}

func (p *Player) state() string {
	if p.cue == 0 {
		return "ended"
	}
	if p.paused {
		return "paused"
	}
	switch call("BASS_ChannelIsActive", uintptr(p.cue)) {
	case activePaused:
		return "paused"
	case activeStopped:
		return "ended"
	}
	return "playing"
}

// State is playing, paused or ended.
func (p *Player) State() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state()
}

// Toggle pauses or resumes and returns the new state.
func (p *Player) Toggle() string {
	p.mu.Lock()
	switch p.state() {
	case "playing":
		call("BASS_ChannelPause", uintptr(p.cue))
		p.paused = true
		p.mu.Unlock()
		return "paused"
	case "paused":
		call("BASS_ChannelPlay", uintptr(p.cue), 0)
		p.paused = false
		p.mu.Unlock()
		return "playing"
	}
	p.mu.Unlock()
	p.Start(0)
	return "playing"
}

func (p *Player) SetSongVolume(v float64) {
	p.SongVolume = v
	for _, h := range p.stems {
		setVolume(h, v)
	}
}

func (p *Player) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, h := range append([]uint32{p.cue}, p.stems...) {
		if h != 0 {
			call("BASS_ChannelStop", uintptr(h))
			call("BASS_StreamFree", uintptr(h))
		}
	}
	p.cue, p.stems, p.bufs = 0, nil, nil
	p.pinner.Unpin()
}

func Current() *Player {
	currentMu.Lock()
	defer currentMu.Unlock()
	return current
}

// StopCurrent stops the preview that is playing or paused and reports whether there was one.
func StopCurrent() bool {
	currentMu.Lock()
	p := current
	current = nil
	currentMu.Unlock()
	if p == nil {
		return false
	}
	p.stop()
	return true
}

func SetCurrent(p *Player) {
	StopCurrent()
	currentMu.Lock()
	current = p
	currentMu.Unlock()
}
