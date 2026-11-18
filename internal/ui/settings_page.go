package ui

import (
	"os"
	"strings"

	"github.com/lxn/walk"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yarc"
)

// Settings tab: folders, where the mod comes from, speech options. (Stable or Nightly is on the Game tab.)
type SettingsPage struct {
	w           *MainWindow
	tab         *walk.TabPage
	contentRoot *walk.LineEdit
	gameChoice  *walk.ComboBox
	gameDirs    []string
	gameDir     *walk.LineEdit
	language    *walk.ComboBox
	modRepo     *walk.LineEdit
	modAsset    *walk.LineEdit
	modLocal    *walk.LineEdit
	songsDir    *walk.LineEdit
	addToYarg   *walk.CheckBox
	announce    *walk.CheckBox
	interval    *walk.NumberEdit
}

func newSettingsPage(w *MainWindow) *SettingsPage {
	p := &SettingsPage{w: w}
	cfg := w.cfg
	tab := w.newPage("Settings")
	p.tab = tab

	p.contentRoot = p.pathRow("Content folder (YARG installs, setlists and venues):", cfg.ContentRoot, false)
	r := row(tab)
	p.gameChoice = dropDown(r, "Game install to manage:", nil)
	r.Layout().(*walk.BoxLayout).SetStretchFactor(p.gameChoice, 1)
	p.fillGames()
	p.gameDir = p.pathRow("Game folder for this build (empty means a new install in the content folder):", cfg.GameDirSetting(), false)

	r = row(tab)
	langNames := make([]string, len(config.Languages))
	for i, l := range config.Languages {
		langNames[i] = l.Name
	}
	p.language = dropDown(r, "YARG language (YargAccess speaks it too):", langNames)
	r.Layout().(*walk.BoxLayout).SetStretchFactor(p.language, 1)
	p.language.SetCurrentIndex(0)
	for i, l := range config.Languages {
		if l.Code == cfg.Language {
			p.language.SetCurrentIndex(i)
		}
	}

	p.modRepo = p.textRow("Mod GitHub repository (owner/name):", cfg.ModRepo)
	p.modAsset = p.textRow("Mod release file name pattern:", cfg.ModAssetPattern)
	p.modLocal = p.pathRow("Local mod file instead of downloading (.zip or .dll):", cfg.ModLocalPath, true)

	p.songsDir = p.pathRow("Folder for songs downloaded from Chorus:", cfg.ResolvedSongsDir(), false)
	p.addToYarg = must(walk.NewCheckBox(tab))
	p.addToYarg.SetText("Add the song folder to YARG's song folders automatically")
	p.addToYarg.SetChecked(cfg.AddSongsDirToYarg)

	p.announce = must(walk.NewCheckBox(tab))
	p.announce.SetText("Speak download progress (F3 toggles)")
	p.announce.SetChecked(cfg.AnnounceProgress)

	r = row(tab)
	label(r, "Seconds between progress updates:")
	p.interval = must(walk.NewNumberEdit(r))
	p.interval.SetDecimals(0)
	p.interval.SetRange(1, 60)
	p.interval.SetValue(float64(cfg.ProgressInterval))
	p.interval.SetSpinButtonsVisible(true)
	name(p.interval, "Seconds between progress updates")
	nameChild(p.interval, "Edit", "Seconds between progress updates")
	r.Layout().(*walk.BoxLayout).SetStretchFactor(p.interval, 1)

	r = row(tab)
	button(r, "&Save settings", p.onSave)
	hspacer(r)
	r = row(tab)
	button(r, "Check for launcher &updates", func() { w.checkLauncherUpdate(true) })
	hspacer(r)
	walk.NewVSpacer(tab)
	return p
}

func (p *SettingsPage) textRow(labelText, value string) *walk.LineEdit {
	r := row(p.tab)
	label(r, labelText)
	e := must(walk.NewLineEdit(r))
	e.SetText(value)
	name(e, labelText)
	r.Layout().(*walk.BoxLayout).SetStretchFactor(e, 1)
	return e
}

func (p *SettingsPage) pathRow(labelText, value string, file bool) *walk.LineEdit {
	r := row(p.tab)
	label(r, labelText)
	e := must(walk.NewLineEdit(r))
	e.SetText(value)
	name(e, labelText)
	r.Layout().(*walk.BoxLayout).SetStretchFactor(e, 1)
	b := button(r, "Browse...", func() { p.browse(e, labelText, file) })
	b.Accessibility().SetName("Browse for " + strings.ToLower(trimColon(labelText)))
	return e
}

func (p *SettingsPage) browse(e *walk.LineEdit, labelText string, file bool) {
	dlg := &walk.FileDialog{Title: labelText}
	var ok bool
	var err error
	if file {
		dlg.Filter = "Mod package (*.zip;*.dll)|*.zip;*.dll"
		ok, err = dlg.ShowOpen(p.w)
	} else {
		dlg.InitialDirPath = e.Text()
		ok, err = dlg.ShowBrowseFolder(p.w)
	}
	if err == nil && ok {
		e.SetText(dlg.FilePath)
	}
	e.SetFocus()
}

func (p *SettingsPage) fillGames() {
	p.gameDirs = []string{""}
	labels := []string{"New install made by this launcher"}
	if official := yarc.Detect(); official != nil {
		for _, g := range official.Games() {
			p.gameDirs = append(p.gameDirs, g.Installation())
			labels = append(labels, g.Name+" "+g.Tag()+" from the YARC launcher")
		}
	}
	p.gameChoice.SetModel(labels)
	sel := 0
	for i, d := range p.gameDirs {
		if d == p.w.cfg.GameDirSetting() {
			sel = i
		}
	}
	p.gameChoice.SetCurrentIndex(sel)
	p.gameChoice.CurrentIndexChanged().Attach(func() {
		if i := p.gameChoice.CurrentIndex(); i >= 0 && i < len(p.gameDirs) && p.gameDir != nil {
			p.gameDir.SetText(p.gameDirs[i])
		}
	})
}

func (p *SettingsPage) OnShown() {}

// ChannelChanged shows the game folder of the build just switched to.
func (p *SettingsPage) ChannelChanged() {
	if p.gameDir != nil {
		p.gameDir.SetText(p.w.cfg.GameDirSetting())
	}
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func (p *SettingsPage) onSave() {
	w := p.w
	// A running task reads these settings while it works, so they change only between tasks.
	if w.IsBusy() {
		w.announce("Wait until the current task finishes before saving settings.", true)
		return
	}
	cfg := w.cfg
	repoText := strings.TrimSpace(p.modRepo.Text())
	repo := ""
	if repoText != "" {
		if repo = github.ParseRepo(repoText); repo == "" {
			warningBox(w, "The mod repository should look like owner/name.", "Settings")
			p.modRepo.SetFocus()
			return
		}
	}
	local := strings.TrimSpace(p.modLocal.Text())
	if local != "" && !isFile(local) {
		warningBox(w, "The local mod file does not exist.", "Settings")
		p.modLocal.SetFocus()
		return
	}
	cfg.ContentRoot = strings.TrimSpace(p.contentRoot.Text())
	if cfg.ContentRoot == "" {
		cfg.ContentRoot = config.DefaultContentRoot
	}
	cfg.SetGameDirSetting(strings.TrimSpace(p.gameDir.Text()))
	cfg.ModRepo = repo
	cfg.ModAssetPattern = strings.TrimSpace(p.modAsset.Text())
	if cfg.ModAssetPattern == "" {
		cfg.ModAssetPattern = config.DefaultModAsset
	}
	cfg.ModLocalPath = local
	songs := strings.TrimSpace(p.songsDir.Text())
	defaults := config.Config{ContentRoot: cfg.ContentRoot}
	if songs == defaults.ResolvedSongsDir() {
		songs = ""
	}
	cfg.SongsDir = songs
	cfg.AddSongsDirToYarg = p.addToYarg.Checked()
	cfg.AnnounceProgress = p.announce.Checked()
	cfg.ProgressInterval = int(p.interval.Value())
	cfg.Language = ""
	if i := p.language.CurrentIndex(); i > 0 && i < len(config.Languages) {
		cfg.Language = config.Languages[i].Code
	}
	w.ConfigChanged()
	w.announce("Settings saved.", true)
}
