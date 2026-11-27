// Package preview is the chart preview, modelled on Chorus Encore's own player: the whole chart with
// the song, starting at the chart's preview point, with play / pause, seeking and a volume. On top of
// the song it plays YargAccess's note sounds (five-fret parts, drums and pro keys) and beat ticks,
// using the player's own YargAccess settings (Look-ahead, Drums Look-ahead, Cue Repeated Notes, Beat
// Ticks, Call Length and the volumes) from the game's MelonPreferences.cfg, in either Play Style:
// Listen and Repeat or Play What You Hear.
//
// The chart is downloaded once (the copy without a video) into %APPDATA%\YARG Access\preview, unless
// it is already in the song folder. Only the last few previews are kept.
package preview

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/bass"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/chartfile"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/chorus"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/cuesynth"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/sng"
)

var CacheDir = filepath.Join(config.Dir, "preview")

const keep = 5

var (
	audioExt  = map[string]bool{".opus": true, ".ogg": true, ".mp3": true, ".wav": true, ".flac": true}
	skipStems = map[string]bool{"preview": true, "crowd": true}
)

type Prepared struct {
	Track   *cuesynth.CueTrack
	Stems   [][]byte
	Delay   float64
	Start   float64
	Notes   int
	Summary string
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// PackagePath is the chart's .sng if it is already on disk, else "".
func PackagePath(c *chorus.Chart, songsDir string) string {
	for _, p := range []string{filepath.Join(songsDir, c.FileName()), filepath.Join(CacheDir, c.MD5+".sng")} {
		if isFile(p) {
			return p
		}
	}
	return ""
}

func Fetch(c *chorus.Chart, songsDir string, progress netx.Progress, cancel <-chan struct{}) (string, error) {
	if p := PackagePath(c, songsDir); p != "" {
		return p, nil
	}
	if err := os.MkdirAll(CacheDir, 0o755); err != nil {
		return "", err
	}
	old, _ := filepath.Glob(filepath.Join(CacheDir, "*.sng"))
	sort.Slice(old, func(i, j int) bool { return modTime(old[i]) < modTime(old[j]) })
	for _, p := range old[:max(0, len(old)-(keep-1))] {
		os.Remove(p)
	}
	result, err := chorus.Download(c, CacheDir, progress, cancel, true)
	if err != nil {
		return "", err
	}
	final := filepath.Join(CacheDir, c.MD5+".sng")
	os.Remove(final)
	if err := os.Rename(result, final); err != nil {
		return "", err
	}
	return final, nil
}

func modTime(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.ModTime().UnixNano()
}

func TimeText(seconds float64) string {
	s := int(math.Max(0, seconds))
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// seconds reads a song.ini millisecond value; ok is false when it is not a number.
func seconds(text string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return float64(int64(f)) / 1000, true
}

// Prepare loads a chart and makes its preview. listenRepeat picks the Play Style: true for Listen and
// Repeat (in calls of the game's Call Length), false for Play What You Hear.
func Prepare(path, part, difficulty, gameDir string, previewStartMs int, listenRepeat bool) (*Prepared, error) {
	pkg, err := sng.Open(path)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, n := range []string{"notes.mid", "notes.chart"} {
		if _, ok := pkg.Files[n]; ok {
			if files[n], err = pkg.Read(n); err != nil {
				return nil, err
			}
		}
	}
	chart, err := chartfile.Load(files, pkg.Metadata)
	if err != nil {
		return nil, err
	}
	drums, prokeys := part == "drums", part == "prokeys"
	var first, last, lastEnd float64
	var count int
	var drumNotes []*chartfile.DrumNote
	var keyNotes []*chartfile.KeyNote
	var notes []*chartfile.Note
	switch {
	case drums:
		drumNotes = chart.DrumNotes(difficulty)
		if count = len(drumNotes); count > 0 {
			first, last = drumNotes[0].Time, drumNotes[count-1].Time
		}
	case prokeys:
		keyNotes = chart.ProkeysNotes(difficulty)
		if count = len(keyNotes); count > 0 {
			first, last = keyNotes[0].Time, keyNotes[count-1].Time
			for _, n := range keyNotes {
				lastEnd = math.Max(lastEnd, n.End)
			}
		}
	default:
		notes = chart.Notes(part, difficulty)
		if count = len(notes); count > 0 {
			first, last = notes[0].Time, notes[count-1].Time
			lastEnd = math.Inf(-1)
			for _, n := range notes {
				lastEnd = math.Max(lastEnd, n.End)
			}
		}
	}
	if count == 0 {
		if prokeys && len(chart.PartsMap()["prokeys"]) == 0 {
			return nil, &chartfile.ChartError{Msg: "This chart has no pro keys part. Chorus only lists keys, so pro keys " +
				"can only be found once the chart is downloaded."}
		}
		return nil, &chartfile.ChartError{Msg: fmt.Sprintf("The chart has no %s notes for this part.", difficulty)}
	}
	settings := cuesynth.ReadSettings(gameDir)

	start := float64(previewStartMs) / 1000
	ok := previewStartMs > 0
	if !ok {
		start, ok = seconds(pkg.Metadata["preview_start_time"])
	}
	fromFirst := !ok || start <= 0 || start > last
	if fromFirst {
		start = math.Max(0, first-1.5)
	}

	// The song key's pentatonic scale, A minor when no key can be found (as the mod).
	tonic, minor := 9, true
	if chart.Key != nil {
		tonic, minor = chart.Key.Tonic, chart.Key.Minor
	}
	ahead := settings.LookAhead
	if drums {
		ahead = settings.DrumLookAhead
	}
	bank := cuesynth.NewBank(tonic, minor, ahead, drums)
	end := lastEnd + 2.0
	if drums {
		end = last + 2.0
	}
	var calls *cuesynth.Calls
	var plays []*cuesynth.Play
	switch {
	case drums:
		if listenRepeat {
			drumNotes, calls = cuesynth.CutDrums(drumNotes, chart.Tempo, settings.CallLength)
		}
		plays = cuesynth.ScheduleDrums(drumNotes, chart.DrumMode, settings, bank, calls, chart.DrumFills(difficulty),
			cuesynth.DrumStarSim(drumNotes, chart.Tempo))
	case prokeys:
		if listenRepeat {
			keyNotes, calls = cuesynth.CutKeys(keyNotes, chart.Tempo, settings.CallLength)
		}
		plays = cuesynth.ScheduleProkeys(keyNotes, settings, bank, cuesynth.NewKeyBank(), calls,
			cuesynth.KeyStarSim(keyNotes, chart.Tempo))
	default:
		if listenRepeat {
			notes, calls = cuesynth.CutNotes(notes, chart.Tempo, settings.CallLength)
		}
		plays = cuesynth.Schedule(notes, chart.Tempo, settings, bank, calls, cuesynth.NoteStarSim(notes, chart.Tempo),
			0.0, math.Inf(1))
	}
	plays = append(plays, calls.Swells(settings, bank)...)
	if fromFirst && calls != nil && len(calls.Groups) > 0 {
		// Start before the first listen swell, not the first note.
		start = math.Max(0, math.Min(start, calls.Groups[0].CallStart-cuesynth.SwellLength-1.5))
	}
	plays = append(plays, cuesynth.BeatTicks(chart.Tempo, settings, bank, end)...)
	if settings.Mono {
		for _, p := range plays {
			p.Pan = 0
		}
	}
	track := cuesynth.NewCueTrack(plays, end)

	var stems [][]byte
	for _, n := range pkg.Names {
		ext := filepath.Ext(n)
		if audioExt[ext] && !skipStems[strings.TrimSuffix(n, ext)] {
			data, err := pkg.Read(n)
			if err != nil {
				return nil, err
			}
			stems = append(stems, data)
		}
	}
	delay, _ := seconds(pkg.Metadata["delay"])

	var tones string
	switch {
	case prokeys:
		tones = "each key at its own pitch"
	case chart.Key != nil:
		tones = "tones in the song key"
	default:
		tones = "tones in A minor, because the song has no vocals or pro keys chart to take a key from"
	}
	kind := ""
	switch {
	case drums:
		kind = map[string]string{chartfile.FIVE: "five-lane drums, ", chartfile.PRO: "pro drums, ", chartfile.FOUR: "four-lane drums, "}[chart.DrumMode]
	case prokeys:
		kind = "pro keys, "
	}
	summary := fmt.Sprintf("%sPlay What You Hear, %d notes, %d millisecond look-ahead, %s", kind, count,
		int(math.RoundToEven(ahead*1000)), tones)
	if calls != nil {
		summary = fmt.Sprintf("%sListen and Repeat, %d patterns of up to %d notes, you play %d of %d notes, %s", kind,
			len(calls.Groups), calls.Size, calls.Kept, calls.Total, tones)
	}
	return &Prepared{track, stems, delay, start, count, summary}, nil
}

// trackAdapter feeds a CueTrack to BASS without allocating in the stream callback.
type trackAdapter struct{ t *cuesynth.CueTrack }

func (a trackAdapter) TotalFrames() int { return a.t.Frames }

func (a trackAdapter) Render(first, count int, out []float32) { a.t.RenderInto(out[:count*2], first) }

// Play starts the preview at its preview point and makes it the current one.
func Play(p *Prepared, gameDir string, songVolume float64) (*bass.Player, error) {
	player, err := bass.NewPlayer(gameDir, trackAdapter{p.Track}, p.Stems, p.Delay, songVolume)
	if err != nil {
		return nil, err
	}
	bass.SetCurrent(player)
	if err := player.Start(p.Start); err != nil {
		return nil, err
	}
	return player, nil
}
