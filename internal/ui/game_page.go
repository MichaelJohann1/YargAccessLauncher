package ui

import (
	"fmt"
	"path/filepath"

	"github.com/lxn/walk"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/catalog"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/installer"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yarc"
)

// Game tab: install, update, repair and launch YARG with the accessibility mod.

const nightlyWarning = "Nightly builds are YARG's newest test versions. They change almost every day and can have bugs.\n\n" +
	"The accessibility mod is made and tested for the Stable version. On Nightly, some menus may not be " +
	"spoken correctly or at all.\n\n" +
	"Nightly is installed separately, so your Stable install is not changed. Both use the same music packs " +
	"and venues, but YARG keeps separate settings, profiles and scores for Nightly.\n\n" +
	"Switch to Nightly anyway?"

var channels = []struct{ id, label string }{
	{"stable", "Stable (recommended)"},
	{"nightly", "Nightly (test version, the accessibility mod may not work)"},
}

type GamePage struct {
	w       *MainWindow
	channel *walk.ComboBox
	status  *walk.TextEdit
}

func newGamePage(w *MainWindow) *GamePage {
	p := &GamePage{w: w}
	tab := w.newPage("Game")

	r := row(tab)
	var labels []string
	for _, c := range channels {
		labels = append(labels, c.label)
	}
	p.channel = dropDown(r, "YARG &build:", labels)
	name(p.channel, "YARG build")
	if w.cfg.Channel == "nightly" {
		p.channel.SetCurrentIndex(1)
	}
	p.channel.CurrentIndexChanged().Attach(p.onChannel)
	hspacer(r)

	label(tab, "Game status:")
	p.status = readOnlyText(tab, "Game status")
	p.status.SetMinMaxSize(walk.Size{Height: 110}, walk.Size{Height: 110})

	for _, b := range []struct {
		text    string
		handler func()
	}{
		{"&Play YARG", p.onPlay},
		{"&Install or update YARG and the accessibility mod", p.onInstall},
		{"&Check for updates", p.onCheck},
		{"&Repair the accessibility mod (reinstall MelonLoader, mod and speech files)", p.onRepair},
		{"Reinstall YARG from &scratch (keeps your mods and settings)", p.onReinstall},
		{"&Open the game folder", func() { w.OpenFolder(w.cfg.ResolvedGameDir()) }},
		// The mod's log is MelonLoader\Latest.log; MelonLoader\Logs keeps the logs of earlier runs.
		{"Open the &logs folder (the mod's log is Latest.log)", func() {
			w.OpenFolder(filepath.Join(w.cfg.ResolvedGameDir(), "MelonLoader"))
		}},
		{"&Uninstall YARG", p.onUninstall},
	} {
		r := row(tab)
		button(r, b.text, b.handler)
		hspacer(r)
	}
	walk.NewVSpacer(tab)
	return p
}

func (p *GamePage) onChannel() {
	i := p.channel.CurrentIndex()
	if i < 0 {
		return
	}
	channel := channels[i].id
	w := p.w
	if channel == w.cfg.Channel {
		return
	}
	if w.IsBusy() {
		if w.cfg.Channel == "stable" {
			p.channel.SetCurrentIndex(0)
		} else {
			p.channel.SetCurrentIndex(1)
		}
		w.announce("Wait until the current task finishes before switching builds.", true)
		return
	}
	if channel == "nightly" && !w.cfg.NightlyWarned {
		if !confirm(w, nightlyWarning, "Switch to Nightly", walk.MsgBoxIconWarning) {
			p.channel.SetCurrentIndex(0)
			w.announce("Staying on the Stable build.", false)
			return
		}
		w.cfg.NightlyWarned = true
	}
	w.cfg.Channel = channel
	w.ConfigChanged()
	if w.settings != nil {
		w.settings.ChannelChanged()
	}
	nameText := "Stable"
	if channel == "nightly" {
		nameText = "Nightly"
	}
	w.announce(fmt.Sprintf("Managing the %s build. %s", nameText, p.status.Text()), false)
}

func (p *GamePage) FocusFirst() { p.status.SetFocus() }

func (p *GamePage) RefreshStatus() {
	st := installer.New(p.w.cfg, nil, nil).Status()
	text := st.Summary()
	if !st.GameInstalled {
		text += " Choose Install to download it."
	} else if !(st.MelonLoader && st.Mod) || len(st.SpeechDLLMissing) > 0 {
		text += " Choose Repair to fix the accessibility mod."
	}
	p.status.SetText(text)
}

func (p *GamePage) OnShown() { p.RefreshStatus() }

func (p *GamePage) onPlay() {
	w := p.w
	if w.IsBusy() {
		w.announce(fmt.Sprintf("Please wait, %s is still running.", w.taskLabel), true)
		return
	}
	inst := installer.New(w.cfg, nil, nil)
	if !inst.Status().GameInstalled {
		w.announce("YARG is not installed yet. Choose Install first.", true)
		return
	}
	if installer.IsGameRunning("") {
		w.announce("YARG is already running.", true)
		return
	}
	if err := inst.Launch(); err != nil {
		errorBox(w, err.Error(), "Could not start YARG")
		return
	}
	w.announce("Starting YARG.", true)
}

func (p *GamePage) onInstall() {
	wasInstalled := installer.New(p.w.cfg, nil, nil).Status().GameInstalled
	p.w.RunTask("Install or update", func(inst *installer.Installer) (any, error) {
		return inst.InstallEverything(false)
	}, func(result any) { p.offerOfficialSetlist(result, wasInstalled) })
}

// offerOfficialSetlist offers the official setlist after a fresh install, since YARG ships without songs.
func (p *GamePage) offerOfficialSetlist(result any, wasInstalled bool) {
	if result == nil || wasInstalled {
		return
	}
	w := p.w
	entry := catalog.OfficialSetlistEntry()
	inst := installer.New(w.cfg, nil, nil)
	folder := inst.PackageFolder(entry, yarc.Detect())
	if inst.PackageTag(folder) != "" {
		return
	}
	if walk.MsgBox(w, "Download songs", "YARG doesn't come with any songs. To play, you need to download a music pack "+
		"from the DLC tab.\n\nDo you want to download the YARG Official Setlist now?",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walkYes {
		w.announce("You can download music packs later on the DLC tab.", false)
		return
	}
	w.RunTask("Install "+entry.Name, func(i *installer.Installer) (any, error) {
		return i.InstallPackage(entry, folder)
	}, func(any) { w.packs.MarkStale() })
}

func (p *GamePage) onReinstall() {
	if confirm(p.w, "Download YARG again and replace the game files? Your mods, MelonLoader settings "+
		"and YARG settings are kept.", "Reinstall YARG", walk.MsgBoxIconQuestion) {
		p.w.RunTask("Reinstall", func(inst *installer.Installer) (any, error) {
			return inst.InstallEverything(true)
		}, nil)
	}
}

func (p *GamePage) onUninstall() {
	w := p.w
	inst := installer.New(w.cfg, nil, nil)
	if !inst.Status().GameInstalled {
		w.announce("YARG is not installed.", true)
		return
	}
	if !confirm(w, "Uninstall YARG? This deletes the game folder, including MelonLoader, the "+
		"accessibility mod and the speech files:\n"+inst.GameDir()+"\n\n"+
		"You are asked next about DLC, your YARG profiles and settings, "+
		"and Chorus songs.", "Uninstall YARG", walk.MsgBoxIconWarning) {
		return
	}
	var removePackages, removePlayerData, removeSongs bool
	if count := len(inst.InstalledPackages()); count > 0 {
		a := ask(w, fmt.Sprintf("Also remove the %d installed DLC setlists and venues?", count), "Uninstall YARG")
		if a == nil {
			return
		}
		removePackages = *a
	}
	if installer.PlayerDataExists() {
		a := ask(w, "Also delete your YARG player data? This is your profiles (player names, instruments, "+
			"difficulty and controller bindings), settings, scores, playlists and custom presets, in\n"+
			installer.PlayerDataDir()+"\n\nThis cannot be undone.", "Uninstall YARG")
		if a == nil {
			return
		}
		removePlayerData = *a
	}
	if songs := len(inst.ChorusSongs()); songs > 0 {
		a := ask(w, fmt.Sprintf("Also delete the %d songs you downloaded through Chorus, in\n%s?",
			songs, w.cfg.ResolvedSongsDir()), "Uninstall YARG")
		if a == nil {
			return
		}
		removeSongs = *a
	}
	w.RunTask("Uninstall", func(i *installer.Installer) (any, error) {
		return i.UninstallGame(removePackages, removePlayerData, removeSongs)
	}, func(any) { w.packs.MarkStale() })
}

func (p *GamePage) onRepair() {
	p.w.RunTask("Repair mod", func(inst *installer.Installer) (any, error) { return inst.RepairMod() }, nil)
}

func (p *GamePage) onCheck() {
	w := p.w
	w.RunTask("Update check", func(inst *installer.Installer) (any, error) {
		rel, err := inst.LatestGame()
		if err != nil {
			return nil, err
		}
		st := inst.Status()
		switch {
		case !st.GameInstalled:
			return fmt.Sprintf("YARG %s is available. It is not installed yet.", rel.Tag), nil
		case st.GameTag == rel.Tag:
			return fmt.Sprintf("YARG is up to date, version %s.", rel.Tag), nil
		}
		have := st.GameTag
		if have == "" {
			have = "an unknown version"
		}
		return fmt.Sprintf("An update is available: YARG %s. You have %s.", rel.Tag, have), nil
	}, func(r any) {
		if r != nil {
			w.checkModUpdate(true)
		}
	})
}
