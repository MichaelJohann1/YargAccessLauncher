package ui

import (
	"strings"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"github.com/MichaelJohann1/YargAccessLauncher/internal/app"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/github"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/news"
	"github.com/MichaelJohann1/YargAccessLauncher/internal/selfupdate"
)

func unsafePointer(r *win.RECT) unsafe.Pointer { return unsafe.Pointer(r) }

// crlf makes line breaks show in a Windows edit control.
func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

// notesDialog is the "update available" prompt with the release notes. It returns true for OK.
func notesDialog(owner walk.Form, title, intro, notes, okLabel string) bool {
	dlg := must(walk.NewDialog(owner))
	dlg.SetTitle(title)
	dlg.SetLayout(vbox(8, 8))
	wrapLabel(dlg, intro)
	label(dlg, "What's new:")
	text := news.MarkdownToText(notes)
	if text == "" {
		text = "No release notes."
	}
	box := readOnlyText(dlg, "What's new")
	box.SetText(crlf(text))
	dlg.Layout().(*walk.BoxLayout).SetStretchFactor(box, 1)

	buttons := row(dlg)
	hspacer(buttons)
	ok := button(buttons, okLabel, func() { dlg.Accept() })
	later := button(buttons, "&Later", func() { dlg.Cancel() })
	dlg.SetDefaultButton(ok)
	dlg.SetCancelButton(later)
	enterActivates(dlg, dlg.DefaultButton)
	dlg.SetSize(walk.Size{Width: 640, Height: 480})
	dlg.Starting().Attach(func() {
		box.SetFocus()
		box.SetTextSelection(0, 0)
	})
	return dlg.Run() == walk.DlgCmdOK
}

func showUpdateDialog(owner walk.Form, u *selfupdate.Update, canInstall bool) bool {
	intro := "YARG Access launcher " + u.Version + " is available. You have " + app.Version + "."
	ok := "&Update now"
	if !canInstall {
		intro += " You are running the launcher from source, so update it with git pull."
		ok = "Open the release &page"
	}
	return notesDialog(owner, "Launcher update available", intro, u.Notes, ok)
}

func showModUpdateDialog(owner walk.Form, a *github.Asset) bool {
	intro := "Mod update available: version " + strings.TrimPrefix(a.Tag, "v") + ". Download and install it now?"
	return notesDialog(owner, "Mod update available", intro, a.Notes, "&Download and install")
}

// detailsDialog shows a DLC item's description and song list.
func detailsDialog(owner walk.Form, title, text string) {
	dlg := must(walk.NewDialog(owner))
	dlg.SetTitle(title)
	dlg.SetLayout(vbox(4, 4))
	label(dlg, "Details:")
	box := readOnlyText(dlg, "Details")
	box.SetText(crlf(text))
	dlg.Layout().(*walk.BoxLayout).SetStretchFactor(box, 1)
	buttons := row(dlg)
	hspacer(buttons)
	closeBtn := button(buttons, "&Close", func() { dlg.Cancel() })
	dlg.SetCancelButton(closeBtn)
	dlg.SetDefaultButton(closeBtn)
	enterActivates(dlg, dlg.DefaultButton)
	dlg.SetSize(walk.Size{Width: 600, Height: 500})
	dlg.Starting().Attach(func() { box.SetFocus() })
	dlg.Run()
}
