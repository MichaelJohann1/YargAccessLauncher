package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/bass"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/chorus"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/cuesynth"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/installer"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/preview"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/speech"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yargsettings"
)

// Download songs through Chorus tab: find charts on Chorus Encore and download them into the song folder.

const chorusHelp = "Enter on a result downloads it. Play or pause preview plays the whole " +
	"chosen part and difficulty from the chart's preview point, with the song and " +
	"YargAccess's note sounds made from your YargAccess settings. Preview play style " +
	"picks Listen and Repeat or Play What You Hear for each chart; it starts on the " +
	"Play Style saved in the game. The position slider seeks; Escape stops. Five-fret, drums and pro keys have sounds; vocals do not. " +
	"The newest charts are listed when this tab first opens, so you can browse without searching. " +
	"Browse all charts clears the search boxes and lists every chart in the chosen sort order, with " +
	"the instrument and difficulty filters still applied. Type a number in Go to page and press Enter " +
	"to jump to that page. Charts come from " +
	"Chorus Encore, enchor.us, a community site. Check each chart's licence before " +
	"sharing it."

type previewKey struct {
	md5, part, diff string
	listen          bool
}

// Preview play style choices, in the order of the Preview play style list.
var playStyles = []string{"Listen and Repeat", "Play What You Hear"}

type ChorusPage struct {
	w         *MainWindow
	result    *chorus.SearchResult
	query     string
	filters   *chorus.Filters
	searching bool
	browsed   bool // the newest charts were loaded when the tab first opened

	pageBox *walk.LineEdit

	searchBox    *walk.LineEdit
	instrument   *walk.ComboBox
	difficulty   *walk.ComboBox
	sort         *walk.ComboBox
	charterBox   *walk.LineEdit
	summary      *walk.Label
	list         *ReportList
	partChoice   *walk.ComboBox
	diffChoice   *walk.ComboBox
	styleChoice  *walk.ComboBox
	styles       map[string]int // chart MD5 -> the play style chosen for it
	settingStyle bool
	position     *walk.Slider
	volume       *walk.Slider
	withVideo    *walk.CheckBox
	previewParts []chorus.Part
	previewDiffs []string
	previewKey   *previewKey
	timerStop    chan struct{}
	settingPos   bool
}

func optionLabels(opts []chorus.Option) []string {
	var out []string
	for _, o := range opts {
		out = append(out, o.Label)
	}
	return out
}

func newChorusPage(w *MainWindow) *ChorusPage {
	p := &ChorusPage{w: w, styles: map[string]int{}}
	tab := w.newPage("Download songs through Chorus")

	r := row(tab)
	label(r, "Search for song, album or artist:")
	p.searchBox = must(walk.NewLineEdit(r))
	name(p.searchBox, "Search for song, album or artist")
	p.searchBox.KeyDown().Attach(p.enterSearches)
	r.Layout().(*walk.BoxLayout).SetStretchFactor(p.searchBox, 1)

	r = row(tab)
	p.instrument = dropDown(r, "Instrument:", optionLabels(chorus.Instruments))
	p.difficulty = dropDown(r, "Difficulty:", optionLabels(chorus.DifficultyFilters))
	var sorts []string
	for _, s := range chorus.Sorts {
		sorts = append(sorts, s.Label)
	}
	p.sort = dropDown(r, "Sort by:", sorts)
	hspacer(r)

	r = row(tab)
	label(r, "Search for charter:")
	p.charterBox = must(walk.NewLineEdit(r))
	p.charterBox.SetMinMaxSize(walk.Size{Width: 200}, walk.Size{Width: 200})
	name(p.charterBox, "Search for charter")
	p.charterBox.KeyDown().Attach(p.enterSearches)
	button(r, "&Search", func() { p.Search(1, nil) })
	button(r, "&Browse all charts", p.browseAll)
	hspacer(r)

	p.summary = label(tab, "Loading the newest charts.")

	label(tab, "Results:")
	p.list = NewReportList(tab, []Column{{"Song", 220}, {"Artist", 160}, {"Length", 60}, {"Charter", 140},
		{"Instruments and difficulties", 360}, {"Details", 600}}, "Results")
	p.list.OnActivated(p.onDownload)
	p.list.OnSelected(p.fillPreviewParts)
	tab.Layout().(*walk.BoxLayout).SetStretchFactor(p.list, 1)

	r = row(tab)
	button(r, "&Download selected", p.onDownload)
	button(r, "&Previous page", func() { p.page(-1) })
	button(r, "&Next page", func() { p.page(1) })
	label(r, "Go to page:")
	p.pageBox = must(walk.NewLineEdit(r))
	p.pageBox.SetMinMaxSize(walk.Size{Width: 70}, walk.Size{Width: 70})
	name(p.pageBox, "Go to page")
	p.pageBox.KeyDown().Attach(p.enterGoesToPage)
	button(r, "Open song &folder", func() { w.OpenFolder(p.songsDir()) })
	hspacer(r)

	r = row(tab)
	p.partChoice = dropDown(r, "Preview part:", []string{"No part with sounds"})
	p.partChoice.CurrentIndexChanged().Attach(p.fillDifficulties)
	p.diffChoice = dropDown(r, "Preview difficulty:", []string{"None"})
	p.styleChoice = dropDown(r, "Preview play style:", playStyles)
	p.styleChoice.CurrentIndexChanged().Attach(p.onStyleChosen)
	button(r, "Pla&y or pause preview", p.onPlayPause)
	button(r, "S&top preview", func() { p.StopPreview(false) })
	hspacer(r)

	r = row(tab)
	label(r, "Preview position, seconds:")
	p.position = must(walk.NewSlider(r))
	p.position.SetRange(0, 1)
	p.position.SetMinMaxSize(walk.Size{Width: 300}, walk.Size{})
	p.position.SendMessage(win.TBM_SETLINESIZE, 0, 5)
	p.position.SendMessage(win.TBM_SETPAGESIZE, 0, 30)
	name(p.position, "Preview position, seconds")
	p.position.ValueChanged().Attach(p.onSeek)
	p.position.SetEnabled(false)
	r.Layout().(*walk.BoxLayout).SetStretchFactor(p.position, 1)
	label(r, "Song volume:")
	p.volume = must(walk.NewSlider(r))
	p.volume.SetRange(0, 100)
	p.volume.SetValue(60)
	p.volume.SetMinMaxSize(walk.Size{Width: 150}, walk.Size{Width: 150})
	name(p.volume, "Song volume")
	p.volume.ValueChanged().Attach(p.onVolume)

	p.withVideo = must(walk.NewCheckBox(tab))
	p.withVideo.SetText("Download video backgrounds (much bigger files, only visual)")
	wrapLabel(tab, chorusHelp)
	return p
}

func (p *ChorusPage) enterSearches(key walk.Key) {
	if key == walk.KeyReturn {
		p.Search(1, nil)
	}
}

func (p *ChorusPage) songsDir() string { return p.w.cfg.ResolvedSongsDir() }

// OnShown lists the newest charts the first time the tab opens, so there is something to browse
// before any search. It is quiet: the summary label holds the count, and focus stays where it is.
func (p *ChorusPage) OnShown() {
	if p.browsed || p.result != nil {
		return
	}
	p.browsed = true
	p.run(1, &chorus.Filters{Sort: chorus.Sorts[1]}, true)
}

// browseAll lists every chart: the search boxes are cleared and the sort, instrument and difficulty
// choices are kept. Best match means nothing without text, so it becomes Newest first.
func (p *ChorusPage) browseAll() {
	p.searchBox.SetText("")
	p.charterBox.SetText("")
	if p.sort.CurrentIndex() <= 0 {
		p.sort.SetCurrentIndex(1)
	}
	p.Search(1, nil)
}

func (p *ChorusPage) enterGoesToPage(key walk.Key) {
	if key != walk.KeyReturn {
		return
	}
	if p.result == nil {
		speech.Speak("Search or browse first.", true)
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(p.pageBox.Text()))
	last := pageCount(p.result.Found)
	if err != nil || n < 1 || n > last {
		speech.Speak(fmt.Sprintf("Type a page number from 1 to %d.", last), true)
		return
	}
	p.Search(n, p.filters)
}

func pageCount(found int) int {
	return max(1, (found+chorus.PageSize-1)/chorus.PageSize)
}

// Search runs a search. filters: reuse these (paging); nil reads the search box and the filter controls.
func (p *ChorusPage) Search(page int, filters *chorus.Filters) {
	if filters == nil {
		filters = &chorus.Filters{
			Text:       strings.TrimSpace(p.searchBox.Text()),
			Instrument: chorus.Instruments[max(0, p.instrument.CurrentIndex())].Value,
			Difficulty: chorus.DifficultyFilters[max(0, p.difficulty.CurrentIndex())].Value,
			Sort:       chorus.Sorts[max(0, p.sort.CurrentIndex())],
			Charter:    strings.TrimSpace(p.charterBox.Text()),
		}
	}
	p.run(page, filters, false)
}

// run fetches a page. quiet: no speech and no focus change (the list loaded when the tab opens).
func (p *ChorusPage) run(page int, filters *chorus.Filters, quiet bool) {
	if p.searching {
		return
	}
	p.searching = true
	var said []string
	if filters.Text != "" {
		said = append(said, filters.Text)
	}
	if filters.Charter != "" {
		said = append(said, "charter "+filters.Charter)
	}
	p.query = strings.Join(said, ", ")
	if p.query == "" {
		p.query = "these filters"
	}
	if !quiet {
		speech.Speak("Searching.", true)
	}
	go func() {
		res, err := chorus.Search(*filters, page)
		p.w.Synchronize(func() {
			p.searching = false
			if err != nil {
				if quiet {
					p.summary.SetText("The newest charts could not be loaded. Search or choose Browse all charts to try again.")
					return
				}
				errorBox(p.w, fmt.Sprintf("The Chorus search failed.\n\n%v", err), "Error")
				return
			}
			p.show(res, filters, quiet)
		})
	}()
}

func (p *ChorusPage) page(delta int) {
	if p.result == nil {
		speech.Speak("Search or browse first.", true)
		return
	}
	n := p.result.Page + delta
	if n < 1 || n > pageCount(p.result.Found) {
		speech.Speak("No more pages.", true)
		return
	}
	p.Search(n, p.filters)
}

func (p *ChorusPage) show(res *chorus.SearchResult, filters *chorus.Filters, quiet bool) {
	p.result = res
	p.filters = filters
	p.list.DeleteAll()
	for _, c := range res.Charts {
		p.list.Add(c.Name, c.Artist, c.LengthText(), c.Charter, c.InstrumentsText(), c.Details)
	}
	var text string
	if res.Found == 0 {
		text = fmt.Sprintf("No charts found for %s.", p.query)
	} else {
		// The search's total, then the charts loaded on this page.
		word := "charts"
		if res.Found == 1 {
			word = "chart"
		}
		verb := "Found"
		if filters.Text == "" && filters.Charter == "" {
			verb = "Browsing"
		}
		text = fmt.Sprintf("%s %d %s. %d on page %d of %d.", verb, res.Found, word, len(res.Charts), res.Page, pageCount(res.Found))
	}
	p.summary.SetText(text)
	p.pageBox.SetText(strconv.Itoa(res.Page))
	if quiet {
		p.w.logLine(text)
	} else {
		p.w.announce(text, true)
	}
	if !quiet && len(res.Charts) > 0 {
		p.list.FocusRow(0)
		p.list.SetFocus()
	}
	p.fillPreviewParts()
}

func (p *ChorusPage) selectedChart() *chorus.Chart {
	i := p.list.Selected()
	if p.result == nil || i < 0 || i >= len(p.result.Charts) {
		return nil
	}
	return p.result.Charts[i]
}

func (p *ChorusPage) onDownload() {
	chart := p.selectedChart()
	if chart == nil {
		speech.Speak("Select a chart first.", true)
		return
	}
	folder := p.songsDir()
	skipVideo := !p.withVideo.Checked()
	addToYarg := p.w.cfg.AddSongsDirToYarg
	p.w.RunTask("Download "+chart.Name, func(inst *installer.Installer) (any, error) {
		inst.R.Stage(fmt.Sprintf("Downloading %s by %s", chart.Name, chart.Artist))
		path, err := chorus.Download(chart, folder, inst.R.Progress, inst.Cancel, skipVideo)
		if err != nil {
			return nil, err
		}
		msg := fmt.Sprintf("Downloaded %s.", chart.Name)
		if addToYarg && !yargsettings.HasSongFolder(folder) {
			switch {
			case installer.IsGameRunning(""):
				msg += " YARG is running, so the song folder could not be added to its settings. " +
					"Close YARG and download again, or add the folder in YARG's Songs settings."
			case len(yargsettings.AddSongFolder(folder)) > 0:
				msg += " The song folder was added to YARG's settings."
			default:
				msg += " YARG has no settings file yet. Start YARG once, then download again, or add " +
					"the folder " + folder + " in YARG's Songs settings."
			}
		} else {
			msg += " Rescan songs in YARG to see it."
		}
		if !isFile(path) {
			return nil, errors.New("The download did not produce a file.")
		}
		return msg, nil
	}, nil)
}

// fillPreviewParts fills the Part choice: the selected chart's parts with YargAccess sounds.
func (p *ChorusPage) fillPreviewParts() {
	p.previewParts = nil
	if c := p.selectedChart(); c != nil {
		p.previewParts = c.PreviewParts()
	}
	var names []string
	for _, part := range p.previewParts {
		names = append(names, part.Name)
	}
	if len(names) == 0 {
		names = []string{"No part with sounds"}
	}
	p.partChoice.SetModel(names)
	p.partChoice.SetCurrentIndex(0)
	p.fillDifficulties()
	p.fillStyle()
}

// savedStyle is the Play Style saved in the game's YargAccess settings, as an index into playStyles.
func (p *ChorusPage) savedStyle() int {
	if cuesynth.ReadSettings(p.w.cfg.ResolvedGameDir()).ListenRepeat {
		return 0
	}
	return 1
}

// fillStyle shows the selected chart's play style: the one chosen for it, else the game's.
func (p *ChorusPage) fillStyle() {
	i := -1
	if c := p.selectedChart(); c != nil {
		if v, ok := p.styles[c.MD5]; ok {
			i = v
		}
	}
	if i < 0 {
		i = p.savedStyle()
	}
	p.settingStyle = true
	p.styleChoice.SetCurrentIndex(i)
	p.settingStyle = false
}

func (p *ChorusPage) onStyleChosen() {
	if p.settingStyle {
		return
	}
	if c := p.selectedChart(); c != nil {
		p.styles[c.MD5] = max(0, p.styleChoice.CurrentIndex())
	}
}

// fillDifficulties fills the Difficulty choice: the difficulties the chosen part has notes on, hardest first.
func (p *ChorusPage) fillDifficulties() {
	p.previewDiffs = nil
	if part := p.chosenPart(); part != nil {
		for i := len(part.Difficulties) - 1; i >= 0; i-- {
			p.previewDiffs = append(p.previewDiffs, part.Difficulties[i])
		}
	}
	var names []string
	for _, d := range p.previewDiffs {
		names = append(names, chorus.Capitalize(d))
	}
	if len(names) == 0 {
		names = []string{"None"}
	}
	p.diffChoice.SetModel(names)
	p.diffChoice.SetCurrentIndex(0)
}

func (p *ChorusPage) chosenPart() *chorus.Part {
	i := p.partChoice.CurrentIndex()
	if i >= 0 && i < len(p.previewParts) {
		return &p.previewParts[i]
	}
	return nil
}

func (p *ChorusPage) onPlayPause() {
	chart := p.selectedChart()
	if chart == nil {
		speech.Speak("Select a chart first.", true)
		return
	}
	part := p.chosenPart()
	if part == nil || len(p.previewDiffs) == 0 {
		speech.Speak("This chart has no five-fret guitar, bass, rhythm, keys or drums part, so it has no "+
			"YargAccess sounds to preview.", true)
		return
	}
	diff := p.previewDiffs[max(0, p.diffChoice.CurrentIndex())]
	listen := p.styleChoice.CurrentIndex() <= 0
	key := previewKey{chart.MD5, part.Key, diff, listen}
	if player := bass.Current(); player != nil && p.previewKey != nil && *p.previewKey == key {
		word := "Paused at"
		if player.Toggle() == "playing" {
			word = "Playing from"
		}
		p.w.announce(fmt.Sprintf("%s %s.", word, preview.TimeText(player.Position())), true)
		return
	}
	p.startPreview(chart, *part, diff, listen)
}

func (p *ChorusPage) startPreview(chart *chorus.Chart, part chorus.Part, diff string, listen bool) {
	w := p.w
	label := part.Name + ", " + chorus.Capitalize(diff)
	if w.IsBusy() {
		w.announce(fmt.Sprintf("Please wait, %s is still running.", w.taskLabel), true)
		return
	}
	p.StopPreview(true)
	songsDir := p.songsDir()
	gameDir := w.cfg.ResolvedGameDir()
	w.RunTask("Preview of "+chart.Name, func(inst *installer.Installer) (any, error) {
		if preview.PackagePath(chart, songsDir) == "" {
			inst.R.Stage(fmt.Sprintf("Downloading %s for the preview", chart.Name))
		}
		path, err := preview.Fetch(chart, songsDir, inst.R.Progress, inst.Cancel)
		if err != nil {
			return nil, err
		}
		inst.R.Stage("Making the preview sounds")
		return preview.Prepare(path, part.Key, diff, gameDir, chart.PreviewStartMs, listen)
	}, func(result any) {
		prepared, ok := result.(*preview.Prepared)
		if !ok || prepared == nil {
			return
		}
		player, err := preview.Play(prepared, gameDir, float64(p.volume.Value())/100)
		if err != nil {
			errorBox(w, fmt.Sprintf("The preview could not play.\n\n%v", err), "Error")
			return
		}
		p.previewKey = &previewKey{chart.MD5, part.Key, diff, listen}
		p.position.SetRange(0, max(1, int(player.Length)))
		p.setPosition(int(prepared.Start))
		p.position.SetEnabled(true)
		p.startTimer()
		song := ""
		if player.StemCount() == 0 {
			song = " The song's audio could not be played, so only the sounds play."
		}
		w.announce(fmt.Sprintf("Playing %s from %s of %s: %s. Escape stops.%s", label,
			preview.TimeText(prepared.Start), preview.TimeText(player.Length), prepared.Summary, song), true)
	})
}

// setPosition moves the slider without seeking.
func (p *ChorusPage) setPosition(v int) {
	p.settingPos = true
	p.position.SetValue(v)
	p.settingPos = false
}

func (p *ChorusPage) onSeek() {
	player := bass.Current()
	if player == nil || p.settingPos {
		return
	}
	t := p.position.Value()
	player.Seek(float64(t))
	speech.Speak(preview.TimeText(float64(t)), true)
}

func (p *ChorusPage) onVolume() {
	if player := bass.Current(); player != nil {
		player.SetSongVolume(float64(p.volume.Value()) / 100)
	}
}

func (p *ChorusPage) startTimer() {
	p.stopTimer()
	stop := make(chan struct{})
	p.timerStop = stop
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.w.Synchronize(p.onPreviewTimer)
			}
		}
	}()
}

func (p *ChorusPage) stopTimer() {
	if p.timerStop != nil {
		close(p.timerStop)
		p.timerStop = nil
	}
}

func (p *ChorusPage) onPreviewTimer() {
	player := bass.Current()
	if player == nil {
		p.stopTimer()
		return
	}
	if player.State() == "ended" {
		p.StopPreview(true)
		p.w.announce("Preview finished.", false)
		return
	}
	if win.GetFocus() != p.position.Handle() {
		p.setPosition(int(player.Position()))
	}
}

// StopPreview stops a playing or paused preview and reports whether there was one.
func (p *ChorusPage) StopPreview(quiet bool) bool {
	p.stopTimer()
	p.previewKey = nil
	p.setPosition(0)
	p.position.SetEnabled(false)
	stopped := bass.StopCurrent()
	if !quiet {
		if stopped {
			p.w.announce("Preview stopped.", true)
		} else {
			p.w.announce("No preview is playing.", true)
		}
	}
	return stopped
}
