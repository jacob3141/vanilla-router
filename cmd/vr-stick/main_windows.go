// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"vanilla-router/internal/logo"
	"vanilla-router/internal/windefaults"
	"vanilla-router/internal/windisk"
)

func init() { runtime.LockOSThread() } // Fenster gehören zum Thread, der sie erzeugt hat.

const (
	idCreate = 1 // IDOK: Enter
	idClose  = 2 // IDCANCEL: Esc
	idList   = 100
	idFile   = 101

	timerPoll    = 1
	timerDevices = 2
)

type state int

const (
	stIdle state = iota
	stPreparing
	stRunning
)

// Farben im Stil des Logos (internal/logo). Texte und Knöpfe erfüllen den Kontrast 4,5:1.
var (
	colBody      = hex(0xFFFDF8) // warmes Weiß
	colHeader    = logo.Cream
	colLine      = hex(0xEBD3A8)
	colText      = logo.Outline
	colSub       = hex(0x8A5626)
	colPrimary   = hex(0x9C5F27)
	colPressed   = hex(0x7F4C1E)
	colDisabled  = hex(0xEFE4D3)
	colDisText   = hex(0x9C8B74)
	colSecBorder = hex(0xD9BF98)
	colTrack     = hex(0xF3E6CC)
	colBar       = logo.Caramel
	colWhite     = hex(0xFFFFFF)
)

const (
	W, H    = 560, 500 // Fenstergröße bei 96 DPI
	pad     = 20
	headerH = 92
	barY    = 432
)

type app struct {
	hwnd                                 uintptr
	intro, label, list, status           uintptr
	btnCreate, btnClose, btnFile, footer uintptr
	font, bold, title                    uintptr
	bodyBrush                            uintptr
	icon                                 uintptr // Logo in Kopfgröße (0: ohne Ressource, z. B. in Tests)
	progress                             int     // 0…1000
	dpi                                  int
	disks                                []windisk.Disk
	st                                   state
	j                                    *job
	target                               windisk.Disk
	src                                  *source
	local                                *image
	stickJSON                            []byte
	quitWhenDone                         bool
	prefix                               string // Hinweis vor dem Fortschritt (offline)
	shown                                string // zuletzt angezeigter Fortschrittstext
}

var theApp *app

func main() {
	if germanUI() {
		uiLang = langDE
	} else {
		uiLang = langEN
	}
	pCoInitializeEx.Call(0, coinitApartment)
	icc := initCommonControlsEx{Size: uint32(unsafe.Sizeof(initCommonControlsEx{})), ICC: iccStandard | iccProgress}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	a := &app{}
	theApp = a
	cache := ""
	if d, err := os.UserCacheDir(); err == nil {
		cache = filepath.Join(d, "vanilla-router", "stick")
	}
	a.src = newSource(manifestURL, cache, keys())
	loc, klid, zone := windowsLocale()
	if b, err := windefaults.Info(loc, klid, zone).Encode(); err == nil {
		a.stickJSON = b
	}
	if err := a.create(); err != nil {
		messageBox(0, err.Error(), tr("title"), mbOK|mbIconError)
		os.Exit(1)
	}
	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		// Enter auf einem anderen Knopf als „Stick erstellen“ löst diesen Knopf aus (selbst gezeichnete
		// Knöpfe sind für IsDialogMessage keine Standardknöpfe, Enter hieße sonst immer IDOK).
		if m.Message == wmKeyDown && m.WParam == vkReturn {
			if f, _, _ := pGetFocus.Call(); f == a.btnClose || f == a.btnFile {
				send(f, bmClick, 0, 0)
				continue
			}
		}
		if r, _, _ := pIsDialogMessageW.Call(a.hwnd, uintptr(unsafe.Pointer(&m))); r == 0 {
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}

func (a *app) s(v int) int32 { return int32(v * a.dpi / 96) }

func (a *app) create() error {
	inst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	dc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(dc, logPixelsY)
	pReleaseDC.Call(0, dc)
	a.dpi = int(dpi)
	if a.dpi < 96 {
		a.dpi = 96
	}

	// Logo aus der Ressource (Icon-Gruppe 1, internal/winres/mkres), sonst das Standard-Symbol.
	loadIcon := func(n int32) uintptr {
		h, _, _ := pLoadImageW.Call(inst, 1, imageIcon, uintptr(n), uintptr(n), 0)
		return h
	}
	small, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	icon, iconSm := loadIcon(a.s(32)), loadIcon(int32(small))
	a.icon = loadIcon(a.s(64))
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, idiApplication)
		iconSm = icon
	}
	a.bodyBrush = brush(colBody)
	cls := u16("vanilla-router-stick")
	wc := wndClassEx{WndProc: syscall.NewCallback(wndProc), Instance: inst, Icon: icon, IconSm: iconSm,
		Cursor: cursor, Background: a.bodyBrush, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
	rc := rect{0, 0, a.s(W), a.s(H)}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	ww, wh := rc.Right-rc.Left, rc.Bottom-rc.Top
	sw, _, _ := pGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := pGetSystemMetrics.Call(smCyScreen)
	hwnd, _, err := pCreateWindowExW.Call(wsExControlPar, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16(tr("title")))),
		style, uintptr((int32(sw)-ww)/2), uintptr((int32(sh)-wh)/2), uintptr(ww), uintptr(wh), 0, 0, inst, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx: %v", err)
	}
	a.hwnd = hwnd

	// Schriften wie im System (Meldungsschrift), Überschrift größer und fett.
	var ncm nonClientMetrics
	ncm.Size = uint32(unsafe.Sizeof(ncm))
	pSystemParametersInfoW.Call(spiGetNonClient, uintptr(ncm.Size), uintptr(unsafe.Pointer(&ncm)), 0)
	mkFont := func(lf logFont) uintptr {
		f, _, _ := pCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
		return f
	}
	a.font = mkFont(ncm.MessageFont)
	b := ncm.MessageFont
	b.Weight = 600
	a.bold = mkFont(b)
	t := ncm.MessageFont
	t.Height = t.Height * 17 / 10
	t.Weight = 700
	a.title = mkFont(t)

	ctl := func(class, text string, style uintptr, ex uintptr, id, x, y, w, h int, f uintptr) uintptr {
		c, _, _ := pCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
			wsChild|wsVisible|style, uintptr(a.s(x)), uintptr(a.s(y)), uintptr(a.s(w)), uintptr(a.s(h)), hwnd, uintptr(id), inst, 0)
		send(c, wmSetFont, f, 1)
		return c
	}
	// Kopf (Logo, Name, Überschrift) und Fortschrittsbalken zeichnet paint().
	a.intro = ctl("STATIC", tr("intro"), ssLeft|ssNoPrefix, 0, 0, pad, headerH+16, W-2*pad, 40, a.font)
	a.label = ctl("STATIC", tr("sticks"), ssLeft|ssNoPrefix, 0, 0, pad, headerH+62, W-2*pad, 38, a.bold)
	a.list = ctl("LISTBOX", "", wsTabStop|wsVScroll|lbsNotify|lbsNoIntegral, wsExClientEdge, idList, pad, headerH+102, W-2*pad, 88, a.font)
	a.btnFile = ctl("BUTTON", tr("btn_file"), wsTabStop|bsOwnerDraw, 0, idFile, pad, headerH+200, 230, 30, a.font)
	a.status = ctl("STATIC", tr("ready"), ssLeft|ssNoPrefix, 0, 0, pad, headerH+242, W-2*pad, 94, a.font)
	a.footer = ctl("STATIC", "Version "+version, ssLeft|ssNoPrefix, 0, 0, pad, H-36, 200, 20, a.font)
	a.btnCreate = ctl("BUTTON", tr("btn_create"), wsTabStop|bsOwnerDraw, 0, idCreate, W-pad-120-10-160, H-44, 160, 32, a.bold)
	a.btnClose = ctl("BUTTON", tr("btn_close"), wsTabStop|bsOwnerDraw, 0, idClose, W-pad-120, H-44, 120, 32, a.font)

	a.refreshDisks()
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	pSetFocus.Call(a.list)
	return nil
}

func wndProc(hwnd, m, w, l uintptr) uintptr {
	a := theApp
	if a == nil || a.hwnd == 0 {
		r, _, _ := pDefWindowProcW.Call(hwnd, m, w, l)
		return r
	}
	switch uint32(m) {
	case wmCommand:
		a.command(int(w&0xffff), int(w>>16&0xffff))
		return 0
	case wmTimer:
		switch w {
		case timerPoll:
			a.poll()
		case timerDevices:
			pKillTimer.Call(hwnd, timerDevices)
			if a.st == stIdle {
				a.refreshDisks()
			}
		}
		return 0
	case wmDeviceChange:
		if w == dbtArrival || w == dbtRemoveComplete || w == dbtDevNodesChange {
			// Volumes erscheinen etwas nach dem Datenträger, kurz warten.
			pSetTimer.Call(hwnd, timerDevices, 800, 0)
		}
		return 1
	case wmCtlColorStatic, wmCtlColorBtn:
		pSetBkMode.Call(w, transparent)
		pSetTextColor.Call(w, rgb(colText))
		return a.bodyBrush
	case wmPaint:
		a.paint()
		return 0
	case wmDrawItem:
		a.drawButton(*(**drawItemStruct)(unsafe.Pointer(&l))) // lParam zeigt auf DRAWITEMSTRUCT
		return 1
	case wmClose:
		if a.st != stIdle {
			if messageBox(hwnd, tr("confirm_cancel"), tr("title"), mbYesNo|mbIconWarning|mbDefButton2) == idYes && a.j != nil {
				a.quitWhenDone = true
				a.j.cancel()
			}
			return 0
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, w, l)
	return r
}

func (a *app) barRect() rect {
	return rect{a.s(pad), a.s(barY), a.s(W - pad), a.s(barY + 8)}
}

// setProgress setzt den Fortschritt (0…1000) und zeichnet nur den Balken neu.
func (a *app) setProgress(v int) {
	if v == a.progress {
		return
	}
	a.progress = v
	r := a.barRect()
	pInvalidateRect.Call(a.hwnd, uintptr(unsafe.Pointer(&r)), 0)
}

// paint zeichnet den Kopf mit Logo und den Fortschrittsbalken.
func (a *app) paint() {
	var ps paintStruct
	dc, _, _ := pBeginPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(a.hwnd, uintptr(unsafe.Pointer(&ps)))
	hb := brush(colHeader)
	fillRect(dc, rect{0, 0, a.s(W), a.s(headerH)}, hb)
	pDeleteObject.Call(hb)
	lb := brush(colLine)
	fillRect(dc, rect{0, a.s(headerH) - a.s(2), a.s(W), a.s(headerH)}, lb)
	pDeleteObject.Call(lb)
	x := pad
	if a.icon != 0 {
		pDrawIconEx.Call(dc, uintptr(a.s(pad-4)), uintptr(a.s(12)), a.icon, uintptr(a.s(64)), uintptr(a.s(64)), 0, 0, diNormal)
		x = pad + 72
	}
	drawText(dc, "vanilla-router", rect{a.s(x), a.s(16), a.s(W - pad), a.s(52)}, a.title, colText, dtSingleLine|dtVCenter)
	drawText(dc, tr("heading"), rect{a.s(x), a.s(52), a.s(W - pad), a.s(76)}, a.font, colSub, dtSingleLine|dtVCenter|dtEndEllipsis)

	r := a.barRect()
	fillRect(dc, r, a.bodyBrush)
	radius := r.Bottom - r.Top
	roundRect(dc, r, radius, colTrack, color.NRGBA{})
	if a.progress > 0 {
		f := r
		f.Right = f.Left + (r.Right-r.Left)*int32(a.progress)/1000
		if f.Right-f.Left < radius {
			f.Right = f.Left + radius
		}
		roundRect(dc, f, radius, colBar, color.NRGBA{})
	}
}

// drawButton zeichnet die Knöpfe: „Stick erstellen“ gefüllt in Vanilleschoten-Braun, die anderen hell
// mit Rand.
func (a *app) drawButton(d *drawItemStruct) {
	dc, r := d.Hdc, d.Rect
	fillRect(dc, r, a.bodyBrush)
	primary := d.CtlID == idCreate
	pressed := d.ItemState&odsSelected != 0
	disabled := d.ItemState&odsDisabled != 0
	fill, border, text, font := colWhite, colSecBorder, colText, a.font
	switch {
	case disabled:
		fill, border, text = colDisabled, color.NRGBA{}, colDisText
	case primary && pressed:
		fill, border, text = colPressed, color.NRGBA{}, colWhite
	case primary:
		fill, border, text = colPrimary, color.NRGBA{}, colWhite
	case pressed:
		fill = colHeader
	}
	if primary {
		font = a.bold
	}
	roundRect(dc, r, a.s(10), fill, border)
	drawText(dc, windowText(d.Item), r, font, text, dtCenter|dtVCenter|dtSingleLine)
	if d.ItemState&odsFocus != 0 && d.ItemState&odsNoFocusRect == 0 && !disabled {
		f := rect{r.Left + a.s(4), r.Top + a.s(4), r.Right - a.s(4), r.Bottom - a.s(4)}
		pSetTextColor.Call(dc, rgb(text))
		pDrawFocusRect.Call(dc, uintptr(unsafe.Pointer(&f)))
	}
}

func (a *app) command(id, code int) {
	switch id {
	case idList:
		if code == lbnSelChange {
			a.updateButtons()
		}
	case idCreate:
		if a.st == stIdle {
			a.start()
		}
	case idClose:
		if a.st != stIdle {
			if a.j != nil {
				a.j.cancel()
			}
			return
		}
		pDestroyWindow.Call(a.hwnd)
	case idFile:
		if a.st != stIdle {
			return
		}
		if a.local != nil {
			a.local = nil
			setText(a.btnFile, tr("btn_file"))
			setText(a.status, tr("ready"))
			return
		}
		a.chooseFile()
	}
}

func (a *app) selected() (windisk.Disk, bool) {
	i, _, _ := pSendMessageW.Call(a.list, lbGetCurSel, 0, 0)
	if int(int32(i)) < 0 || int(i) >= len(a.disks) {
		return windisk.Disk{}, false
	}
	return a.disks[i], true
}

func (a *app) refreshDisks() {
	prev, had := a.selected()
	disks, _ := windisk.List()
	a.disks = disks
	send(a.list, lbResetContent, 0, 0)
	sel := -1
	for i, d := range disks {
		send(a.list, lbAddString, 0, uintptr(unsafe.Pointer(u16(diskLabel(d)))))
		if had && d.Number == prev.Number && d.Size == prev.Size {
			sel = i
		}
	}
	if sel < 0 && len(disks) == 1 {
		sel = 0
	}
	if sel >= 0 {
		send(a.list, lbSetCurSel, uintptr(sel), 0)
	}
	if len(disks) == 0 {
		setText(a.label, tr("no_sticks"))
	} else {
		setText(a.label, tr("sticks"))
	}
	a.updateButtons()
}

func (a *app) updateButtons() {
	_, ok := a.selected()
	idle := a.st == stIdle
	enable(a.btnCreate, idle && ok)
	enable(a.list, idle)
	enable(a.btnFile, idle)
	if idle {
		setText(a.btnClose, tr("btn_close"))
	} else {
		setText(a.btnClose, tr("btn_cancel"))
	}
}

func (a *app) chooseFile() {
	buf := make([]uint16, 4096)
	ofn := openFileName{Owner: a.hwnd, File: &buf[0], MaxFile: uint32(len(buf)),
		Filter: u16z(tr("file_filter"), "*.img;*.img.gz;*.raw;*.gz", "*.*", "*.*"),
		Flags:  ofnFileMustExist | ofnPathMustExist | ofnHideReadOnly | ofnNoChangeDir | ofnExplorer}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	if r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return
	}
	path := syscall.UTF16ToString(buf)
	im, err := localImage(path, a.src.keys)
	if err != nil {
		messageBox(a.hwnd, errText(err, true, false), tr("err_title"), mbOK|mbIconError)
		return
	}
	a.local = im
	setText(a.btnFile, tr("btn_online"))
	setText(a.status, tr("ready_local", filepath.Base(path), im.m.Version))
}

// start: erst Manifest holen, dann Größe prüfen und warnen, dann schreiben.
func (a *app) start() {
	d, ok := a.selected()
	if !ok || a.stickJSON == nil {
		return
	}
	a.target = d
	a.prefix, a.shown = "", ""
	a.st = stPreparing
	a.updateButtons()
	a.setProgress(0)
	setText(a.status, tr("ph_manifest"))
	ctx, cancel := context.WithCancel(context.Background())
	a.j = &job{cancel: cancel}
	go prepare(ctx, a.j, a.src, a.local)
	pSetTimer.Call(a.hwnd, timerPoll, 100, 0)
}

func (a *app) poll() {
	s := a.j.state()
	if !s.over {
		a.show(s)
		return
	}
	pKillTimer.Call(a.hwnd, timerPoll)
	if a.quitWhenDone {
		pDestroyWindow.Call(a.hwnd)
		return
	}
	switch a.st {
	case stPreparing:
		a.st = stIdle
		if s.err != nil {
			a.fail(s, false)
			return
		}
		a.confirmAndRun(s)
	case stRunning:
		a.st = stIdle
		a.updateButtons()
		if s.err != nil {
			a.fail(s, s.wrote)
			return
		}
		a.setProgress(1000)
		setText(a.status, tr("done"))
		pMessageBeep.Call(mbIconInfo)
		pSetTimer.Call(a.hwnd, timerDevices, 1500, 0)
	}
}

func (a *app) fail(s jobState, wrote bool) {
	a.updateButtons()
	a.setProgress(0)
	txt := errText(s.err, a.local != nil, wrote)
	setText(a.status, txt)
	icon := uint32(mbIconError)
	if errors.Is(s.err, context.Canceled) {
		icon = mbIconInfo
	}
	messageBox(a.hwnd, txt, tr("err_title"), mbOK|icon)
	a.refreshDisks()
}

func (a *app) confirmAndRun(s jobState) {
	im := s.im
	d := a.target
	if d.Size < im.m.Image.Size {
		a.updateButtons()
		txt := tr("too_small", size(im.m.Image.Size), size(d.Size))
		setText(a.status, txt)
		messageBox(a.hwnd, txt, tr("err_title"), mbOK|mbIconError)
		return
	}
	if s.offline {
		a.prefix = tr("offline_note", im.m.Version) + "\r\n\r\n"
	}
	if messageBox(a.hwnd, tr("confirm", diskLabel(d)), tr("confirm_title"), mbYesNo|mbIconWarning|mbDefButton2) != idYes {
		a.updateButtons()
		setText(a.status, tr("ready"))
		return
	}
	if d.Size > windisk.LargeDisk &&
		messageBox(a.hwnd, tr("confirm_large", diskLabel(d)), tr("confirm_title"), mbYesNo|mbIconWarning|mbDefButton2) != idYes {
		a.updateButtons()
		setText(a.status, tr("ready"))
		return
	}
	a.st = stRunning
	a.shown = ""
	a.updateButtons()
	ctx, cancel := context.WithCancel(context.Background())
	a.j = &job{cancel: cancel}
	p := platform{
		open: func(d windisk.Disk) (device, error) {
			dev, err := windisk.Open(d)
			if err != nil {
				return nil, err
			}
			return dev, nil
		},
		disksOfPath: windisk.DisksOfPath,
	}
	go run(ctx, a.j, a.src, im, d, p, a.stickJSON)
	pSetTimer.Call(a.hwnd, timerPoll, 100, 0)
}

func (a *app) show(s jobState) {
	var txt string
	switch s.ph {
	case phManifest:
		txt = tr("ph_manifest")
	case phDownload:
		txt = tr("ph_download", size(s.done), size(s.total))
	case phCheck:
		txt = tr("ph_check")
	case phOpen:
		txt = tr("ph_open")
	case phWrite:
		txt = tr("ph_write")
	case phVerify:
		txt = tr("ph_verify")
	default:
		return
	}
	txt = a.prefix + txt
	if txt != a.shown {
		setText(a.status, txt)
		a.shown = txt
	}
	pos := 0
	if s.total > 0 {
		pos = int(s.done * 1000 / s.total)
	}
	a.setProgress(pos)
}
