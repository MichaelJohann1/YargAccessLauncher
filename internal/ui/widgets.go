// Package ui is the launcher window, built from native Win32 controls through walk so a screen reader
// reads every control by its label.
//
// Every input gets a visible label created immediately before it. NVDA and JAWS take a control's
// name from the static text that precedes it in tab order, the same convention NVDA's own settings
// dialogs use; the name is also set on the control itself.
package ui

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func vbox(margin, spacing int) *walk.BoxLayout {
	l := walk.NewVBoxLayout()
	l.SetMargins(walk.Margins{HNear: margin, VNear: margin, HFar: margin, VFar: margin})
	l.SetSpacing(spacing)
	return l
}

func hbox(margin, spacing int) *walk.BoxLayout {
	l := walk.NewHBoxLayout()
	l.SetMargins(walk.Margins{HNear: margin, VNear: margin, HFar: margin, VFar: margin})
	l.SetSpacing(spacing)
	return l
}

// row is a horizontal strip inside parent.
func row(parent walk.Container) *walk.Composite {
	c := must(walk.NewComposite(parent))
	c.SetLayout(hbox(0, 8))
	return c
}

func label(parent walk.Container, text string) *walk.Label {
	l := must(walk.NewLabel(parent))
	l.SetText(text)
	return l
}

// wrapLabel is a label whose text wraps to the window's width, for help sentences.
func wrapLabel(parent walk.Container, text string) *walk.TextLabel {
	l := must(walk.NewTextLabel(parent))
	l.SetText(text)
	// walk sizes a TextLabel by its minimum width; without one it wants the whole text on one line.
	l.SetMinMaxSize(walk.Size{Width: 300}, walk.Size{})
	return l
}

func name(w walk.Window, text string) {
	w.Accessibility().SetName(trimColon(text))
}

var (
	accProps         *win.IAccPropServices
	user32           = syscall.NewLazyDLL("user32.dll")
	procFindWindowEx = user32.NewProc("FindWindowExW")
	procGetClassName = user32.NewProc("GetClassNameW")
)

// findChild is the first child window of parent with this class.
func findChild(parent win.HWND, class string) win.HWND {
	h, _, _ := procFindWindowEx.Call(uintptr(parent), 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(class))), 0)
	return win.HWND(h)
}

// nameChild names a child window walk does not expose, like the edit inside a NumberEdit.
func nameChild(parent walk.Window, class, text string) {
	h := findChild(parent.Handle(), class)
	if h == 0 {
		return
	}
	if accProps == nil {
		if win.FAILED(win.CoCreateInstance(&win.CLSID_AccPropServices, nil, win.CLSCTX_ALL,
			&win.IID_IAccPropServices, (*unsafe.Pointer)(unsafe.Pointer(&accProps)))) {
			accProps = nil
			return
		}
	}
	accProps.SetHwndPropStr(h, win.OBJID_CLIENT, win.CHILDID_SELF, &win.PROPID_ACC_NAME, trimColon(text))
}

// enterActivates makes Enter click the focused push button in form. The dialog manager sends a
// button's click to the form, and walk finds the button only among the form's direct children, so
// Enter did nothing on buttons inside rows. On anything else Enter goes to the focused control, or in
// a dialog to its default button.
func enterActivates(form interface{ ShortcutActions() *walk.ActionList }, defaultButton func() *walk.PushButton) {
	a := walk.NewAction()
	a.SetShortcut(walk.Shortcut{Key: walk.KeyReturn})
	a.Triggered().Attach(func() {
		focus := win.GetFocus()
		if focus == 0 {
			return
		}
		if className(focus) == "Button" {
			if style := win.GetWindowLong(focus, win.GWL_STYLE) & 0xF; style == win.BS_PUSHBUTTON || style == win.BS_DEFPUSHBUTTON {
				win.SendMessage(focus, win.BM_CLICK, 0, 0)
				return
			}
		}
		if defaultButton != nil {
			if b := defaultButton(); b != nil && b.Enabled() {
				win.SendMessage(b.Handle(), win.BM_CLICK, 0, 0)
			}
			return
		}
		win.SendMessage(focus, win.WM_KEYDOWN, win.VK_RETURN, 1)
		win.SendMessage(focus, win.WM_KEYUP, win.VK_RETURN, 0xC0000001)
	})
	form.ShortcutActions().Add(a)
}

func className(h win.HWND) string {
	buf := make([]uint16, 64)
	n, _, _ := procGetClassName.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// tabStripFirst puts a TabWidget's tab strip before its pages in the Tab order, as in a native
// property sheet: walk creates the pages above it in z-order, which made Tab skip from the strip
// past the page.
func tabStripFirst(tw *walk.TabWidget) {
	h := findChild(tw.Handle(), "SysTabControl32")
	if h != 0 {
		win.SetWindowPos(h, win.HWND_TOP, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	}
}

// focusTabStrip moves the keyboard focus to the tab strip after every tab switch, so the screen
// reader says the tab, arrow keys keep moving between tabs, and Tab goes into the page. walk moves
// the focus into the new page whenever it was anywhere in the tab widget (the strip included), or
// leaves it on the hidden page's control after Ctrl+Tab.
func focusTabStrip(tw *walk.TabWidget) {
	if h := findChild(tw.Handle(), "SysTabControl32"); h != 0 {
		win.SetFocus(h)
	}
}

func trimColon(s string) string {
	for len(s) > 0 && s[len(s)-1] == ':' {
		s = s[:len(s)-1]
	}
	return s
}

func button(parent walk.Container, text string, handler func()) *walk.PushButton {
	b := must(walk.NewPushButton(parent))
	b.SetText(text)
	b.Clicked().Attach(handler)
	return b
}

func dropDown(parent walk.Container, labelText string, choices []string) *walk.ComboBox {
	label(parent, labelText)
	c := must(walk.NewDropDownBox(parent))
	c.SetModel(choices)
	c.SetCurrentIndex(0)
	name(c, labelText)
	return c
}

// readOnlyText is a multi-line read-only text box a screen reader can read line by line.
func readOnlyText(parent walk.Container, accName string) *walk.TextEdit {
	t := must(walk.NewTextEditWithStyle(parent, win.WS_VSCROLL))
	t.SetReadOnly(true)
	name(t, accName)
	return t
}

func hspacer(parent walk.Container) { walk.NewHSpacer(parent) }

const (
	walkYes         = win.IDYES
	lvmGetItemCount = win.LVM_FIRST + 4
	spiGetWorkArea  = 0x0030
)

func errorBox(owner walk.Form, message, title string) {
	if title == "" {
		title = "Error"
	}
	walk.MsgBox(owner, title, message, walk.MsgBoxOK|walk.MsgBoxIconError)
}

func warningBox(owner walk.Form, message, title string) {
	walk.MsgBox(owner, title, message, walk.MsgBoxOK|walk.MsgBoxIconWarning)
}

// confirm asks a yes or no question with No as the default.
func confirm(owner walk.Form, message, title string, icon walk.MsgBoxStyle) bool {
	return walk.MsgBox(owner, title, message, walk.MsgBoxYesNo|walk.MsgBoxDefButton2|icon) == win.IDYES
}

// ask is yes, no or cancel with No as the default; nil means Cancel.
func ask(owner walk.Form, message, title string) *bool {
	r := walk.MsgBox(owner, title, message, walk.MsgBoxYesNoCancel|walk.MsgBoxDefButton2|walk.MsgBoxIconQuestion)
	if r == win.IDCANCEL {
		return nil
	}
	yes := r == win.IDYES
	return &yes
}

func bell() { win.MessageBeep(win.MB_ICONERROR) }

// ReportList is a plain report-style list view (SysListView32 with real items, not a virtual list),
// like wx's ListCtrl. NVDA reads each row with all its columns.
type ReportList struct {
	walk.WidgetBase
	columns   int
	activated func()
	selected  func()
	keyDown   func(key walk.Key) bool
}

type Column struct {
	Title string
	Width int
}

func NewReportList(parent walk.Container, columns []Column, accName string) *ReportList {
	lv := &ReportList{columns: len(columns)}
	style := uint32(win.WS_TABSTOP | win.WS_VISIBLE | win.LVS_REPORT | win.LVS_SINGLESEL | win.LVS_SHOWSELALWAYS)
	if err := walk.InitWidget(lv, parent, "SysListView32", style, win.WS_EX_CLIENTEDGE); err != nil {
		panic(err)
	}
	lv.SendMessage(win.LVM_SETEXTENDEDLISTVIEWSTYLE, 0, win.LVS_EX_FULLROWSELECT|win.LVS_EX_DOUBLEBUFFER)
	for i, c := range columns {
		text := syscall.StringToUTF16(c.Title)
		col := win.LVCOLUMN{
			Mask:     win.LVCF_TEXT | win.LVCF_WIDTH | win.LVCF_SUBITEM,
			Cx:       int32(lv.IntFrom96DPI(c.Width)),
			PszText:  &text[0],
			ISubItem: int32(i),
		}
		lv.SendMessage(win.LVM_INSERTCOLUMN, uintptr(i), uintptr(unsafe.Pointer(&col)))
	}
	name(lv, accName)
	return lv
}

func (lv *ReportList) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return walk.NewGreedyLayoutItem()
}

func (lv *ReportList) OnActivated(f func())            { lv.activated = f }
func (lv *ReportList) OnSelected(f func())             { lv.selected = f }
func (lv *ReportList) OnKeyDown(f func(walk.Key) bool) { lv.keyDown = f }

func (lv *ReportList) Count() int { return int(lv.SendMessage(lvmGetItemCount, 0, 0)) }

func (lv *ReportList) DeleteAll() { lv.SendMessage(win.LVM_DELETEALLITEMS, 0, 0) }

// Add appends a row; cells beyond the column count are ignored.
func (lv *ReportList) Add(cells ...string) int {
	text := syscall.StringToUTF16(first(cells))
	item := win.LVITEM{Mask: win.LVIF_TEXT, IItem: int32(lv.Count()), PszText: &text[0]}
	i := int(lv.SendMessage(win.LVM_INSERTITEM, 0, uintptr(unsafe.Pointer(&item))))
	for c := 1; c < len(cells) && c < lv.columns; c++ {
		lv.SetText(i, c, cells[c])
	}
	return i
}

func first(cells []string) string {
	if len(cells) == 0 {
		return ""
	}
	return cells[0]
}

func (lv *ReportList) SetText(row, col int, s string) {
	text := syscall.StringToUTF16(s)
	item := win.LVITEM{ISubItem: int32(col), PszText: &text[0]}
	lv.SendMessage(win.LVM_SETITEMTEXT, uintptr(row), uintptr(unsafe.Pointer(&item)))
}

// Selected is the selected row, or -1.
func (lv *ReportList) Selected() int {
	minusOne := -1
	return int(int32(lv.SendMessage(win.LVM_GETNEXTITEM, uintptr(minusOne), win.LVNI_SELECTED)))
}

// FocusRow selects and focuses a row and scrolls it into view.
func (lv *ReportList) FocusRow(i int) {
	if i < 0 || i >= lv.Count() {
		return
	}
	item := win.LVITEM{StateMask: win.LVIS_SELECTED | win.LVIS_FOCUSED, State: win.LVIS_SELECTED | win.LVIS_FOCUSED}
	lv.SendMessage(win.LVM_SETITEMSTATE, uintptr(i), uintptr(unsafe.Pointer(&item)))
	lv.SendMessage(win.LVM_ENSUREVISIBLE, uintptr(i), 0)
}

func (lv *ReportList) WndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_GETDLGCODE:
		if wParam == win.VK_RETURN {
			return win.DLGC_WANTALLKEYS
		}
	case win.WM_KEYDOWN:
		key := walk.Key(wParam)
		if key == walk.KeyReturn {
			if lv.activated != nil {
				lv.activated()
			}
			return 0
		}
		if lv.keyDown != nil && lv.keyDown(key) {
			return 0
		}
	case win.WM_NOTIFY:
		if hwnd == lv.Handle() {
			// The list's own header notifies the list itself.
			break
		}
		// The parent forwards the list's notifications here (walk's container does this).
		nmh := (*win.NMHDR)(unsafe.Pointer(lParam))
		switch nmh.Code {
		case win.NM_DBLCLK:
			if lv.activated != nil && lv.Selected() >= 0 {
				lv.activated()
			}
		case win.LVN_ITEMCHANGED:
			nml := (*win.NMLISTVIEW)(unsafe.Pointer(lParam))
			if nml.UChanged&win.LVIF_STATE != 0 && nml.UNewState&win.LVIS_SELECTED != 0 &&
				nml.UOldState&win.LVIS_SELECTED == 0 && lv.selected != nil {
				lv.selected()
			}
		}
		return 0
	}
	return lv.WidgetBase.WndProc(hwnd, msg, wParam, lParam)
}
