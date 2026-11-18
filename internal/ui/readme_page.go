package ui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jchv/go-webview2/pkg/edge"
	"github.com/jchv/go-webview2/webviewloader"
	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/bass"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/config"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/docpages"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/speech"
)

// Documents tab: both changelogs and the mod's and launcher's licences. Like the News tab, Enter on a
// document in the list opens it below and Escape goes back to the list. Documents show as web pages
// (Edge WebView2), which a screen reader reads in browse mode; links open in the default browser.
// Without WebView2 the text is shown as plain text instead.
type ReadmePage struct {
	w               *MainWindow
	list            *ReportList
	openIndex       int
	focusWhenLoaded bool
	host            *walk.Composite
	web             *edge.Chromium
	text            *walk.TextEdit
}

func newReadmePage(w *MainWindow) *ReadmePage {
	p := &ReadmePage{w: w, openIndex: -1}
	tab := w.newPage("Documents")
	label(tab, "Documents:")
	p.list = NewReportList(tab, []Column{{"Document", 560}}, "Documents")
	for _, d := range docpages.Documents {
		p.list.Add(d.Name)
	}
	p.list.OnActivated(p.OpenDocument)

	r := row(tab)
	button(r, "&Open document", p.OpenDocument)
	button(r, "Open in &browser", p.onBrowser)
	hspacer(r)

	label(tab, "Document text:")
	var view walk.Widget
	if _, err := webviewloader.GetInstalledVersion(); err == nil {
		p.host = must(walk.NewComposite(tab))
		name(p.host, "Document text")
		view = p.host
	} else {
		log.Printf("WebView2 unavailable, documents show as text: %v", err)
		p.text = readOnlyText(tab, "Document text")
		view = p.text
	}
	wrapLabel(tab, "Enter on a document opens it below. Escape closes the document and goes back to the list.")
	l := tab.Layout().(*walk.BoxLayout)
	l.SetStretchFactor(p.list, 1)
	l.SetStretchFactor(view, 3)
	p.list.FocusRow(0)
	return p
}

// embed starts WebView2 in the host the first time a document opens, so the launcher starts fast.
func (p *ReadmePage) embed() bool {
	if p.web != nil {
		return true
	}
	web := edge.NewChromium()
	web.DataPath = filepath.Join(config.Dir, "WebView2")
	web.MessageCallback = func(msg string) {
		p.w.Synchronize(func() {
			switch {
			case strings.HasPrefix(msg, "open:"):
				if u := strings.TrimPrefix(msg, "open:"); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
					openURL(u)
				}
			}
		})
	}
	// WebView2 always treats Escape as an accelerator, so it arrives here even while the page has focus.
	// Alt+F4 arrives here too and WebView2 would swallow it, so it closes the launcher from here.
	web.AcceleratorKeyCallback = func(vk uint) bool {
		if vk == win.VK_F4 && win.GetKeyState(win.VK_MENU) < 0 {
			p.w.Synchronize(func() { p.w.Close() })
			return true
		}
		if vk != win.VK_ESCAPE {
			return false
		}
		p.w.Synchronize(func() {
			switch {
			case p.w.IsBusy():
				p.w.CancelTask()
			case bass.Current() != nil:
				p.w.chorus.StopPreview(false)
			case p.openIndex >= 0:
				p.CloseDocument()
			}
		})
		return true
	}
	web.NavigationCompletedCallback = func(*edge.ICoreWebView2, *edge.ICoreWebView2NavigationCompletedEventArgs) {
		p.w.Synchronize(func() {
			if p.focusWhenLoaded {
				p.focusWhenLoaded = false
				p.web.Focus()
			}
		})
	}
	if !web.Embed(uintptr(p.host.Handle())) {
		return false
	}
	web.Resize()
	p.host.SizeChanged().Attach(web.Resize)
	p.web = web
	return true
}

func (p *ReadmePage) OnShown() {}

func (p *ReadmePage) OpenDocument() {
	i := p.list.Selected()
	if i < 0 || i >= len(docpages.Documents) {
		speech.Speak("Select a document first.", true)
		return
	}
	p.openIndex = i
	speech.Speak(fmt.Sprintf("Opening %s.", docpages.Documents[i].Name), true)
	if p.host != nil && p.embed() {
		p.focusWhenLoaded = true
		p.web.NavigateToString(docpages.DocumentPage(i, true))
		return
	}
	if p.text == nil {
		errorBox(p.w, "The document viewer (Edge WebView2) could not start. Use Open in browser.", "Error")
		return
	}
	d := docpages.Documents[i]
	p.text.SetText(crlf(docpages.Read(d.FileName, d.Folder)))
	p.text.SetTextSelection(0, 0)
	p.text.SetFocus()
}

func (p *ReadmePage) CloseDocument() {
	p.openIndex = -1
	p.focusWhenLoaded = false
	if p.web != nil {
		p.web.NavigateToString(docpages.Page("Documents", "", false))
	} else if p.text != nil {
		p.text.SetText("")
	}
	i := p.list.Selected()
	if i < 0 {
		i = 0
	}
	p.list.FocusRow(i)
	p.list.SetFocus()
	speech.Speak("Document closed.", true)
}

// onBrowser writes the selected document to the temp folder and opens it in the default browser.
func (p *ReadmePage) onBrowser() {
	i := p.list.Selected()
	if i < 0 || i >= len(docpages.Documents) {
		i = p.openIndex
	}
	if i < 0 || i >= len(docpages.Documents) {
		speech.Speak("Select a document first.", true)
		return
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("yarg-access-document-%d.html", i))
	if err := os.WriteFile(path, []byte(docpages.DocumentPage(i, false)), 0o644); err != nil {
		errorBox(p.w, err.Error(), "Error")
		return
	}
	openURL("file:///" + filepath.ToSlash(path))
}
