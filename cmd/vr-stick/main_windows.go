// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

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

type app struct {
	hwnd                                     uintptr
	heading, intro, label, list, status, bar uintptr
	btnCreate, btnClose, btnFile, footer     uintptr
	dpi                                      int
	disks                                    []windisk.Disk
	st                                       state
	j                                        *job
	target                                   windisk.Disk
	src                                      *source
	local                                    *image
	stickJSON                                []byte
	quitWhenDone                             bool
	prefix                                   string // Hinweis vor dem Fortschritt (offline)
	shown                                    string // zuletzt angezeigter Fortschrittstext
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
	icon, _, _ := pLoadIconW.Call(0, idiApplication)
	bg, _, _ := pGetSysColorBrush.Call(colorWindow)
	cls := u16("vanilla-router-stick")
	wc := wndClassEx{WndProc: syscall.NewCallback(wndProc), Instance: inst, Icon: icon, IconSm: icon,
		Cursor: cursor, Background: bg, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}

	dc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(dc, logPixelsY)
	pReleaseDC.Call(0, dc)
	a.dpi = int(dpi)
	if a.dpi < 96 {
		a.dpi = 96
	}

	const W, H = 560, 452
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
	font, _, _ := pCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&ncm.MessageFont)))
	big := ncm.MessageFont
	big.Height = big.Height * 3 / 2
	big.Weight = 600
	bigFont, _, _ := pCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&big)))

	const pad = 20
	ctl := func(class, text string, style uintptr, ex uintptr, id, x, y, w, h int, f uintptr) uintptr {
		c, _, _ := pCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
			wsChild|wsVisible|style, uintptr(a.s(x)), uintptr(a.s(y)), uintptr(a.s(w)), uintptr(a.s(h)), hwnd, uintptr(id), inst, 0)
		send(c, wmSetFont, f, 1)
		return c
	}
	a.heading = ctl("STATIC", tr("heading"), ssLeft|ssNoPrefix, 0, 0, pad, 16, W-2*pad, 34, bigFont)
	a.intro = ctl("STATIC", tr("intro"), ssLeft|ssNoPrefix, 0, 0, pad, 56, W-2*pad, 40, font)
	a.label = ctl("STATIC", tr("sticks"), ssLeft|ssNoPrefix, 0, 0, pad, 104, W-2*pad, 20, font)
	a.list = ctl("LISTBOX", "", wsTabStop|wsVScroll|lbsNotify|lbsNoIntegral, wsExClientEdge, idList, pad, 126, W-2*pad, 96, font)
	a.btnFile = ctl("BUTTON", tr("btn_file"), wsTabStop|bsPushButton, 0, idFile, pad, 230, 220, 28, font)
	a.status = ctl("STATIC", tr("ready"), ssLeft|ssNoPrefix, 0, 0, pad, 272, W-2*pad, 104, font)
	a.bar = ctl("msctls_progress32", "", 0, 0, 0, pad, 380, W-2*pad, 18, font)
	send(a.bar, pbmSetRange32, 0, 1000)
	a.footer = ctl("STATIC", "Version "+version, ssLeft|ssNoPrefix, 0, 0, pad, 418, 200, 20, font)
	a.btnCreate = ctl("BUTTON", tr("btn_create"), wsTabStop|bsDefPushButton, 0, idCreate, W-pad-120-10-150, 410, 150, 30, font)
	a.btnClose = ctl("BUTTON", tr("btn_close"), wsTabStop|bsPushButton, 0, idClose, W-pad-120, 410, 120, 30, font)

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
		bg, _, _ := pGetSysColorBrush.Call(colorWindow)
		return bg
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
	send(a.bar, pbmSetPos, 0, 0)
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
		send(a.bar, pbmSetPos, 1000, 0)
		setText(a.status, tr("done"))
		pMessageBeep.Call(mbIconInfo)
		pSetTimer.Call(a.hwnd, timerDevices, 1500, 0)
	}
}

func (a *app) fail(s jobState, wrote bool) {
	a.updateButtons()
	send(a.bar, pbmSetPos, 0, 0)
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
	pos := uintptr(0)
	if s.total > 0 {
		pos = uintptr(s.done * 1000 / s.total)
	}
	send(a.bar, pbmSetPos, pos, 0)
}
