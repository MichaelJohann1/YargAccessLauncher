package ui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/app"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/bass"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/catalog"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/installer"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/netx"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/selfupdate"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/speech"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yarc"
)

// Main window: Game, News, DLC, Download songs through Chorus, Documents and Settings tabs.
//
// Keyboard: Ctrl+Tab / Ctrl+Shift+Tab switch tabs. F1 lists shortcuts. F2 repeats the current
// progress. F3 turns spoken progress on or off. Escape cancels the running download (it never closes
// the window).

const helpText = "Keyboard shortcuts. Control Tab and Control Shift Tab switch between the Game, News, DLC, " +
	"Download songs through Chorus, Documents and Settings tabs, saying the name of each. F1 repeats this help. F2 reads the current progress. " +
	"F3 turns spoken progress updates on or off. Escape cancels the running download, or stops a " +
	"chart preview. " +
	"Alt F4 closes the launcher."

func megabytes(n int64) string { return fmt.Sprintf("%.0f", float64(n)/(1024*1024)) }

var updateResult = filepath.Join(config.Dir, "update-result.txt")

type page interface {
	OnShown()
}

// Work is a task run on a worker goroutine. Its result goes to the task's done callback; a non-empty
// string result is announced when the task finishes.
type Work func(inst *installer.Installer) (any, error)

type MainWindow struct {
	*walk.MainWindow
	cfg      *config.Config
	firstRun bool
	official *yarc.OfficialLauncher

	taskMu       sync.Mutex
	busy         bool
	taskDone     chan struct{}
	cancel       chan struct{}
	cancelOnce   *sync.Once
	taskLabel    string
	stageText    string
	progressDone atomic.Int64
	progressTot  atomic.Int64
	lastSpeech   time.Time
	exiting      bool

	notebook *walk.TabWidget
	pages    []page
	game     *GamePage
	news     *NewsPage
	packs    *PacksPage
	chorus   *ChorusPage
	readme   *ReadmePage
	settings *SettingsPage
	gauge    *walk.ProgressBar
	logBox   *walk.TextEdit
	status   *walk.StatusBarItem
}

// uiReporter bridges worker progress to the window. All UI work goes through Synchronize.
type uiReporter struct{ w *MainWindow }

func (r uiReporter) Stage(text string) {
	log.Print(text)
	r.w.Synchronize(func() { r.w.onStage(text) })
}

func (r uiReporter) Progress(done, total int64) {
	r.w.progressDone.Store(done)
	r.w.progressTot.Store(total)
	r.w.Synchronize(r.w.updateGauge)
}

func Run() error {
	w := &MainWindow{cfg: config.Load()}
	_, err := os.Stat(config.Path)
	w.firstRun = os.IsNotExist(err)
	w.official = yarc.Detect()
	if w.firstRun {
		w.adoptOfficialInstall()
	}

	w.MainWindow = must(walk.NewMainWindow())
	w.SetTitle(fmt.Sprintf("%s launcher %s", app.Name, app.Version))
	w.SetLayout(vbox(4, 4))

	w.notebook = must(walk.NewTabWidget(w))
	w.game = newGamePage(w)
	w.news = newNewsPage(w)
	w.packs = newPacksPage(w)
	w.chorus = newChorusPage(w)
	w.readme = newReadmePage(w)
	w.settings = newSettingsPage(w)
	w.pages = []page{w.game, w.news, w.packs, w.chorus, w.readme, w.settings}
	tabStripFirst(w.notebook)
	w.notebook.CurrentIndexChanged().Attach(func() {
		focusTabStrip(w.notebook)
		if i := w.notebook.CurrentIndex(); i >= 0 && i < len(w.pages) {
			// Say which tab is now shown: the screen reader does not always read the tab strip.
			speech.Speak(w.notebook.Pages().At(i).Title()+" tab", true)
			w.pages[i].OnShown()
		}
	})
	w.Layout().(*walk.BoxLayout).SetStretchFactor(w.notebook, 1)

	label(w, "Progress:")
	w.gauge = must(walk.NewProgressBar(w))
	w.gauge.SetRange(0, 1000)
	name(w.gauge, "Progress")
	label(w, "Activity log:")
	w.logBox = readOnlyText(w, "Activity log")
	w.logBox.SetMinMaxSize(walk.Size{Height: 120}, walk.Size{Height: 120})

	w.status = walk.NewStatusBarItem()
	w.StatusBar().Items().Add(w.status)
	w.StatusBar().SetVisible(true)
	w.status.SetText("Ready")

	w.addShortcut(walk.KeyF1, func() {
		speech.Speak(helpText, true)
		w.logLine(helpText)
	})
	w.addShortcut(walk.KeyF2, func() { speech.Speak(w.progressText(), true) })
	w.addShortcut(walk.KeyF3, func() {
		w.cfg.AnnounceProgress = !w.cfg.AnnounceProgress
		config.Save(w.cfg)
		if w.cfg.AnnounceProgress {
			speech.Speak("Progress updates on.", true)
		} else {
			speech.Speak("Progress updates off.", true)
		}
	})
	w.addShortcut(walk.KeyEscape, w.onEscape)
	// Alt+F4 closes the launcher wherever focus is, even in a control that keeps the key to itself.
	altF4 := walk.NewAction()
	altF4.SetShortcut(walk.Shortcut{Modifiers: walk.ModAlt, Key: walk.KeyF4})
	altF4.Triggered().Attach(func() { w.Close() })
	w.ShortcutActions().Add(altF4)
	enterActivates(w, nil)

	w.Closing().Attach(w.onClosing)

	w.SetSize(walk.Size{Width: 900, Height: 680})
	centre(w.MainWindow)
	w.logLine(fmt.Sprintf("%s launcher %s started. Press F1 for keyboard shortcuts.", app.Name, app.Version))

	go w.timerLoop()
	w.Synchronize(func() {
		w.game.RefreshStatus()
		w.game.FocusFirst()
		w.reportFinishedUpdate()
	})
	time.AfterFunc(3*time.Second, func() { w.Synchronize(func() { w.checkLauncherUpdate(false) }) })

	w.Show()
	w.Run()
	return nil
}

func centre(f *walk.MainWindow) {
	var r win.RECT
	win.SystemParametersInfo(spiGetWorkArea, 0, unsafePointer(&r), 0)
	b := f.BoundsPixels()
	b.X = int(r.Left) + (int(r.Right-r.Left)-b.Width)/2
	b.Y = int(r.Top) + (int(r.Bottom-r.Top)-b.Height)/2
	f.SetBoundsPixels(b)
}

func (w *MainWindow) addShortcut(key walk.Key, handler func()) {
	a := walk.NewAction()
	a.SetShortcut(walk.Shortcut{Key: key})
	a.Triggered().Attach(handler)
	w.ShortcutActions().Add(a)
}

// newPage adds a tab and returns its page.
func (w *MainWindow) newPage(title string) *walk.TabPage {
	p := must(walk.NewTabPage())
	p.SetTitle(title)
	p.SetLayout(vbox(4, 4))
	w.notebook.Pages().Add(p)
	return p
}

// adoptOfficialInstall uses the YARC launcher's folders when it is already set up, so nothing is
// installed twice.
func (w *MainWindow) adoptOfficialInstall() {
	if w.official == nil {
		return
	}
	w.cfg.ContentRoot = w.official.DownloadLocation
	// Each YARC install goes to the build it is: Stable to the Stable folder, Nightly to Nightly's.
	for _, g := range w.official.Games() {
		switch g.MarketplaceUUID {
		case catalog.YargStable:
			w.cfg.GameDir = g.Installation()
		case catalog.YargNightly:
			w.cfg.NightlyGameDir = g.Installation()
		}
	}
	config.Save(w.cfg)
}

func (w *MainWindow) logLine(text string) {
	stamp := time.Now().Format("15:04:05")
	w.logBox.AppendText(stamp + "  " + text + "\r\n")
	w.status.SetText(text)
}

func (w *MainWindow) announce(text string, interrupt bool) {
	w.logLine(text)
	speech.Speak(text, interrupt)
}

func (w *MainWindow) onStage(text string) {
	w.stageText = text
	w.progressDone.Store(0)
	w.progressTot.Store(0)
	w.gauge.SetMarqueeMode(false)
	w.gauge.SetValue(0)
	w.lastSpeech = time.Now()
	w.announce(text, false)
}

func (w *MainWindow) updateGauge() {
	done, total := w.progressDone.Load(), w.progressTot.Load()
	if total > 0 {
		w.gauge.SetMarqueeMode(false)
		w.gauge.SetValue(int(1000 * done / total))
	} else if !w.gauge.MarqueeMode() {
		w.gauge.SetMarqueeMode(true)
	}
}

func (w *MainWindow) progressText() string {
	if !w.IsBusy() {
		return "Nothing is running."
	}
	text := w.stageText
	if text == "" {
		text = w.taskLabel
	}
	done, total := w.progressDone.Load(), w.progressTot.Load()
	if total > 0 {
		return fmt.Sprintf("%s: %d percent, %s of %s megabytes", text, 100*done/total, megabytes(done), megabytes(total))
	}
	if done > 0 {
		return fmt.Sprintf("%s: %s megabytes", text, megabytes(done))
	}
	return text
}

func (w *MainWindow) timerLoop() {
	for range time.Tick(time.Second) {
		w.Synchronize(w.onTimer)
	}
}

func (w *MainWindow) onTimer() {
	if !w.IsBusy() || !w.cfg.AnnounceProgress || w.progressDone.Load() <= 0 {
		return
	}
	if win.GetForegroundWindow() != w.Handle() {
		return
	}
	interval := w.cfg.ProgressInterval
	if interval < 1 {
		interval = 1
	}
	if time.Since(w.lastSpeech) >= time.Duration(interval)*time.Second {
		w.lastSpeech = time.Now()
		speech.Speak(w.progressText(), false)
	}
}

func (w *MainWindow) IsBusy() bool {
	w.taskMu.Lock()
	defer w.taskMu.Unlock()
	return w.busy
}

// RunTask runs work on a worker goroutine. onDone(result) runs on the UI thread; result is nil when
// the task failed or was cancelled.
func (w *MainWindow) RunTask(label string, work Work, onDone func(any)) bool {
	if w.IsBusy() {
		w.announce(fmt.Sprintf("Please wait, %s is still running. Press Escape to cancel it.", w.taskLabel), false)
		return false
	}
	w.taskMu.Lock()
	w.busy = true
	w.cancel = make(chan struct{})
	w.cancelOnce = new(sync.Once)
	w.taskDone = make(chan struct{})
	w.taskMu.Unlock()
	w.taskLabel = label
	w.stageText = label
	w.progressDone.Store(0)
	w.progressTot.Store(0)
	inst := installer.New(w.cfg, uiReporter{w}, w.cancel)
	w.announce(label+" started.", false)
	done := w.taskDone
	go func() {
		defer close(done)
		result, err := safeRun(work, inst)
		w.Synchronize(func() { w.taskFinished(label, result, err, onDone) })
	}()
	return true
}

func safeRun(work Work, inst *installer.Installer) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("task failed: %v", r)
			err = fmt.Errorf("%v", r)
		}
	}()
	return work(inst)
}

func (w *MainWindow) taskFinished(label string, result any, err error, onDone func(any)) {
	w.taskMu.Lock()
	w.busy = false
	w.taskMu.Unlock()
	w.gauge.SetMarqueeMode(false)
	w.gauge.SetValue(0)
	var ue *installer.Error
	switch {
	case errors.Is(err, netx.ErrCancelled):
		w.announce(label+" cancelled.", true)
	case err != nil:
		msg := err.Error()
		if !errors.As(err, &ue) {
			log.Printf("task %s failed: %v", label, err)
		}
		w.logLine(fmt.Sprintf("%s failed: %s", label, msg))
		bell()
		errorBox(w, fmt.Sprintf("%s failed.\n\n%s", label, msg), "Error")
	default:
		if s, ok := result.(string); ok && s != "" {
			w.announce(s, true)
		} else {
			w.announce(label+" finished.", true)
		}
	}
	if onDone != nil {
		if err != nil {
			result = nil
		}
		onDone(result)
	}
	if !w.exiting {
		w.game.RefreshStatus()
	}
}

func (w *MainWindow) CancelTask() {
	if w.IsBusy() {
		w.cancelOnce.Do(func() { close(w.cancel) })
		w.announce("Cancelling.", true)
	}
}

// onEscape cancels the running task, else stops the preview, else closes the open article or
// document on the focused tab.
func (w *MainWindow) onEscape() {
	switch {
	case w.IsBusy():
		w.CancelTask()
	case bass.Current() != nil:
		w.chorus.StopPreview(false)
	default:
		focus := win.GetFocus()
		if w.news.article.Handle() == focus && w.news.article.Text() != "" {
			w.news.CloseArticle()
		} else if w.readme.text != nil && w.readme.text.Handle() == focus && w.readme.text.Text() != "" {
			w.readme.CloseDocument()
		}
	}
}

func (w *MainWindow) onClosing(canceled *bool, reason walk.CloseReason) {
	if w.exiting {
		return
	}
	if w.IsBusy() {
		if !confirm(w, fmt.Sprintf("%s is still running. Cancel it and close?", w.taskLabel), "Close launcher", walk.MsgBoxIconQuestion) {
			*canceled = true
			return
		}
		w.cancelOnce.Do(func() { close(w.cancel) })
		select {
		case <-w.taskDone:
		case <-time.After(5 * time.Second):
		}
	}
	w.exiting = true
	w.chorus.StopPreview(true)
}

// OpenFolder opens path in Explorer.
func (w *MainWindow) OpenFolder(path string) {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		exec.Command("explorer", filepath.Clean(path)).Start()
	} else {
		w.announce("The folder does not exist yet: "+path, false)
	}
}

func (w *MainWindow) ConfigChanged() {
	config.Save(w.cfg)
	w.game.RefreshStatus()
	w.packs.MarkStale()
}

// reportFinishedUpdate says, after a self-update restart, whether it worked.
func (w *MainWindow) reportFinishedUpdate() {
	b, err := os.ReadFile(updateResult)
	if err != nil {
		return
	}
	os.Remove(updateResult)
	result := strings.TrimSpace(strings.TrimPrefix(string(b), "\xef\xbb\xbf"))
	if result == "updated" {
		w.announce(fmt.Sprintf("The launcher was updated to version %s.", app.Version), false)
	} else {
		errorBox(w, "The launcher update did not finish. The previous version is still installed.\n\n"+result, "Launcher update")
	}
}

// checkLauncherUpdate looks for a newer launcher release in the background. It is silent unless one
// exists, or when the player asked.
func (w *MainWindow) checkLauncherUpdate(manual bool) {
	go func() {
		update, err := selfupdate.Check()
		w.Synchronize(func() {
			switch {
			case err != nil:
				log.Printf("launcher update check failed: %v", err)
				if manual {
					w.announce(fmt.Sprintf("Could not check for launcher updates: %v", err), true)
				} else {
					w.checkModUpdate(false)
				}
			case update == nil:
				if manual {
					w.announce(fmt.Sprintf("The launcher is up to date, version %s.", app.Version), true)
				} else {
					w.checkModUpdate(false)
				}
			default:
				w.offerLauncherUpdate(update)
			}
		})
	}()
}

func (w *MainWindow) offerLauncherUpdate(update *selfupdate.Update) {
	if w.IsBusy() {
		return
	}
	canInstall := selfupdate.IsFrozen()
	speech.Speak(fmt.Sprintf("Launcher update available, version %s.", update.Version), true)
	if !showUpdateDialog(w, update, canInstall) {
		w.announce("Update postponed. The launcher asks again next time it starts.", false)
		w.checkModUpdate(false)
		return
	}
	if !canInstall {
		openURL(update.Page)
		w.checkModUpdate(false)
		return
	}
	w.RunTask("Launcher update", func(inst *installer.Installer) (any, error) {
		inst.R.Stage("Downloading launcher " + update.Version)
		dir, err := selfupdate.Download(update, inst.R.Progress, inst.Cancel)
		if err != nil {
			return nil, err
		}
		return []string{dir}, nil
	}, func(result any) {
		dirs, _ := result.([]string)
		if len(dirs) == 0 {
			return
		}
		w.announce("Installing the update. The launcher restarts in a moment.", true)
		if err := selfupdate.ApplyAndRestart(dirs[0], updateResult); err != nil {
			errorBox(w, err.Error(), "Launcher update")
			return
		}
		time.AfterFunc(1500*time.Millisecond, func() { w.Synchronize(w.exitForUpdate) })
	})
}

// checkModUpdate looks for a newer release of the mod in the background (at start, after the
// launcher's own check, and from the Game tab). It is silent unless one exists, or when the player asked.
func (w *MainWindow) checkModUpdate(manual bool) {
	cfg := *w.cfg
	go func() {
		asset, err := installer.New(&cfg, nil, nil).CheckModUpdate()
		w.Synchronize(func() {
			switch {
			case err != nil:
				log.Printf("mod update check failed: %v", err)
				if manual {
					w.announce(fmt.Sprintf("Could not check for accessibility mod updates: %v", err), true)
				}
			case asset == nil:
				if manual {
					w.announce("The accessibility mod is up to date.", true)
				}
			default:
				w.offerModUpdate(asset)
			}
		})
	}()
}

func (w *MainWindow) offerModUpdate(asset *github.Asset) {
	if w.IsBusy() {
		return
	}
	speech.Speak("Mod update available.", true)
	if !showModUpdateDialog(w, asset) {
		w.announce("Mod update postponed. The launcher asks again next time it starts.", false)
		return
	}
	w.RunTask("Mod update", func(inst *installer.Installer) (any, error) { return inst.UpdateMod() }, nil)
}

func (w *MainWindow) exitForUpdate() {
	w.exiting = true
	w.Close()
}

func openURL(url string) {
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
