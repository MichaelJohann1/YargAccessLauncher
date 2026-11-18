package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/lxn/walk"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/catalog"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/installer"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/speech"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/yarc"
)

// DLC tab: official setlists, YARN setlists and venues from the YARG release server.

var packFilters = []struct {
	label string
	pred  func(*catalog.Entry) bool // nil: decided by install state
}{
	{"All DLC", func(*catalog.Entry) bool { return true }},
	{"Official setlists", func(e *catalog.Entry) bool { return e.Type == "setlist" && e.Category != "yarn_setlist" }},
	{"YARN community setlists", func(e *catalog.Entry) bool { return e.Category == "yarn_setlist" }},
	{"Venues", func(e *catalog.Entry) bool { return e.Type == "venue" }},
	{"Installed", nil},
	{"Updates available", nil},
}

var (
	markdownMarks = regexp.MustCompile("[*_#`>]+")
	whitespace    = regexp.MustCompile(`\s+`)
)

// oneLine flattens a description, which can hold line breaks and Markdown, to one plain line.
func oneLine(text string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(markdownMarks.ReplaceAllString(text, ""), " "))
}

type PacksPage struct {
	w            *MainWindow
	entries      []*catalog.Entry
	latest       map[string]*string // nil value: checking; "": unknown
	descriptions map[string]string
	visible      []*catalog.Entry
	loaded       bool
	official     *yarc.OfficialLauncher
	filter       *walk.ComboBox
	list         *ReportList
}

func newPacksPage(w *MainWindow) *PacksPage {
	p := &PacksPage{w: w, latest: map[string]*string{}, descriptions: map[string]string{}}
	tab := w.newPage("DLC")

	r := row(tab)
	var labels []string
	for _, f := range packFilters {
		labels = append(labels, f.label)
	}
	p.filter = dropDown(r, "Show:", labels)
	p.filter.CurrentIndexChanged().Attach(p.populate)
	hspacer(r)

	label(tab, "DLC:")
	p.list = NewReportList(tab, []Column{{"Name", 220}, {"Type", 120}, {"Status", 130}, {"Installed", 150},
		{"Latest", 150}, {"Description", 600}}, "DLC")
	p.list.OnActivated(p.onInstall)
	p.list.OnKeyDown(func(key walk.Key) bool {
		if key == walk.KeyDelete {
			p.onRemove()
			return true
		}
		return false
	})
	tab.Layout().(*walk.BoxLayout).SetStretchFactor(p.list, 1)

	buttons := row(tab)
	button(buttons, "&Install or update selected", p.onInstall)
	button(buttons, "Update &all installed", p.onUpdateAll)
	button(buttons, "&Remove selected", p.onRemove)
	button(buttons, "&Details and song list", p.onDetails)
	button(buttons, "Re&load list", p.Load)
	hspacer(buttons)
	wrapLabel(tab, "Enter on an item installs or updates it. Delete removes it.")
	return p
}

func (p *PacksPage) OnShown() {
	if !p.loaded {
		p.Load()
	}
}

func (p *PacksPage) MarkStale() { p.loaded = false }

func (p *PacksPage) Load() {
	p.loaded = true
	w := p.w
	w.announce("Loading the DLC list.", false)
	go func() {
		all, err := catalog.FetchCatalog()
		if err != nil {
			w.Synchronize(func() {
				p.loaded = false
				errorBox(w, fmt.Sprintf("Could not load the DLC list.\n\n%v", err), "Error")
			})
			return
		}
		var entries []*catalog.Entry
		for _, e := range all {
			if e.Type == "setlist" || e.Type == "venue" {
				entries = append(entries, e)
			}
		}
		w.Synchronize(func() { p.show(entries) })
		installer.FetchProfiles(entries, func(e *catalog.Entry, info *catalog.ProfileInfo) {
			w.Synchronize(func() { p.gotProfile(e.UUID, info) })
		}, 8)
		w.Synchronize(func() { w.announce("Finished checking DLC for updates.", false) })
	}()
}

func (p *PacksPage) show(entries []*catalog.Entry) {
	p.entries = entries
	p.official = yarc.Detect()
	p.populate()
	p.w.announce(fmt.Sprintf("%d DLC items. Checking for updates.", len(entries)), false)
}

func (p *PacksPage) gotProfile(uuid string, info *catalog.ProfileInfo) {
	tag := ""
	if info != nil {
		tag = info.Release.Tag
		p.descriptions[uuid] = oneLine(info.Description)
	}
	p.latest[uuid] = &tag
	for i, e := range p.visible {
		if e.UUID == uuid {
			p.fillRow(i, e)
			break
		}
	}
}

func (p *PacksPage) installer() *installer.Installer { return installer.New(p.w.cfg, nil, nil) }

type packState struct {
	folder, installed string
	latest            *string
	status            string
}

func (p *PacksPage) state(e *catalog.Entry) packState {
	inst := p.installer()
	folder := inst.PackageFolder(e, p.official)
	installed := inst.PackageTag(folder)
	latest := p.latest[e.UUID]
	var status string
	switch {
	case installed == "":
		status = "Not installed"
	case latest == nil:
		status = "Checking"
	case *latest == "":
		status = "Unknown"
	case *latest == installed:
		status = "Up to date"
	default:
		status = "Update available"
	}
	return packState{folder, installed, latest, status}
}

func (p *PacksPage) visibleEntries() []*catalog.Entry {
	f := packFilters[max(0, p.filter.CurrentIndex())]
	var out []*catalog.Entry
	for _, e := range p.entries {
		if f.pred != nil {
			if f.pred(e) {
				out = append(out, e)
			}
			continue
		}
		st := p.state(e)
		if f.label == "Installed" && st.installed != "" || f.label == "Updates available" && st.status == "Update available" {
			out = append(out, e)
		}
	}
	return out
}

func (p *PacksPage) fillRow(i int, e *catalog.Entry) {
	st := p.state(e)
	installed := st.installed
	if installed == "" {
		installed = "no"
	}
	latest := "checking"
	if st.latest != nil {
		latest = *st.latest
		if latest == "" {
			latest = "unknown"
		}
	}
	p.list.SetText(i, 1, e.KindLabel())
	p.list.SetText(i, 2, st.status)
	p.list.SetText(i, 3, installed)
	p.list.SetText(i, 4, latest)
	p.list.SetText(i, 5, p.descriptions[e.UUID])
}

func (p *PacksPage) populate() {
	keep := p.current()
	p.visible = p.visibleEntries()
	p.list.DeleteAll()
	for i, e := range p.visible {
		p.list.Add(e.Name)
		p.fillRow(i, e)
	}
	if len(p.visible) > 0 {
		idx := 0
		for i, e := range p.visible {
			if e == keep {
				idx = i
			}
		}
		p.list.FocusRow(idx)
	}
}

func (p *PacksPage) current() *catalog.Entry {
	i := p.list.Selected()
	if i >= 0 && i < len(p.visible) {
		return p.visible[i]
	}
	return nil
}

func (p *PacksPage) refreshRow(e *catalog.Entry) {
	for i, v := range p.visible {
		if v == e {
			p.fillRow(i, e)
		}
	}
}

func (p *PacksPage) onInstall() {
	e := p.current()
	if e == nil {
		speech.Speak("Select an item first.", true)
		return
	}
	st := p.state(e)
	if st.status == "Up to date" &&
		!confirm(p.w, e.Name+" is up to date. Download it again?", "Reinstall", walk.MsgBoxIconQuestion) {
		return
	}
	verb := "Install"
	if st.installed != "" {
		verb = "Update"
	}
	p.w.RunTask(verb+" "+e.Name, func(inst *installer.Installer) (any, error) {
		return inst.InstallPackage(e, st.folder)
	}, func(any) { p.refreshRow(e) })
}

func (p *PacksPage) onUpdateAll() {
	type job struct {
		e      *catalog.Entry
		folder string
	}
	var todo []job
	for _, e := range p.entries {
		if st := p.state(e); st.status == "Update available" {
			todo = append(todo, job{e, st.folder})
		}
	}
	if len(todo) == 0 {
		p.w.announce("No updates available for installed DLC.", true)
		return
	}
	p.w.RunTask("Update all DLC", func(inst *installer.Installer) (any, error) {
		for _, j := range todo {
			if _, err := inst.InstallPackage(j.e, j.folder); err != nil {
				return nil, err
			}
		}
		return fmt.Sprintf("Updated %d DLC items.", len(todo)), nil
	}, func(any) { p.populate() })
}

func (p *PacksPage) onRemove() {
	e := p.current()
	if e == nil {
		return
	}
	st := p.state(e)
	if st.installed == "" {
		p.w.announce(e.Name+" is not installed.", true)
		return
	}
	if !confirm(p.w, fmt.Sprintf("Remove %s? Its files are deleted from\n%s", e.Name, st.folder), "Remove DLC", walk.MsgBoxIconQuestion) {
		return
	}
	p.w.RunTask("Remove "+e.Name, func(inst *installer.Installer) (any, error) {
		return inst.UninstallPackage(e, st.folder)
	}, func(any) { p.refreshRow(e) })
}

func (p *PacksPage) onDetails() {
	e := p.current()
	if e == nil {
		return
	}
	p.w.RunTask("Details for "+e.Name, func(*installer.Installer) (any, error) {
		return catalog.FetchProfile(e.URL)
	}, func(result any) {
		info, ok := result.(*catalog.ProfileInfo)
		if !ok || info == nil {
			return
		}
		lines := []string{info.Name, "", info.Description, ""}
		if len(info.Songs) > 0 {
			lines = append(lines, fmt.Sprintf("%d songs:", len(info.Songs)))
			for _, s := range info.Songs {
				secs := songLength(s.Length) / 1000
				lines = append(lines, fmt.Sprintf("%s by %s, %d:%02d", or(s.Title, "?"), or(s.Artist, "?"), secs/60, secs%60))
			}
		}
		detailsDialog(p.w, e.Name, strings.Join(lines, "\n"))
	})
}

func songLength(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		var i int
		fmt.Sscan(n, &i)
		return i
	}
	return 0
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
