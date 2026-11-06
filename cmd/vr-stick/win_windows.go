// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"image/color"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Win32 calls via syscall, without third-party libraries (D-01, PLAN.md 3.12).
var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pDestroyWindow          = user32.NewProc("DestroyWindow")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW       = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pSendMessageW           = user32.NewProc("SendMessageW")
	pShowWindow             = user32.NewProc("ShowWindow")
	pUpdateWindow           = user32.NewProc("UpdateWindow")
	pMessageBoxW            = user32.NewProc("MessageBoxW")
	pMessageBeep            = user32.NewProc("MessageBeep")
	pSetWindowTextW         = user32.NewProc("SetWindowTextW")
	pEnableWindow           = user32.NewProc("EnableWindow")
	pSetTimer               = user32.NewProc("SetTimer")
	pKillTimer              = user32.NewProc("KillTimer")
	pLoadCursorW            = user32.NewProc("LoadCursorW")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pGetDC                  = user32.NewProc("GetDC")
	pReleaseDC              = user32.NewProc("ReleaseDC")
	pSystemParametersInfoW  = user32.NewProc("SystemParametersInfoW")
	pGetKeyboardLayoutNameW = user32.NewProc("GetKeyboardLayoutNameW")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pGetSysColorBrush       = user32.NewProc("GetSysColorBrush")
	pAdjustWindowRectEx     = user32.NewProc("AdjustWindowRectEx")
	pSetFocus               = user32.NewProc("SetFocus")
	pBeginPaint             = user32.NewProc("BeginPaint")
	pEndPaint               = user32.NewProc("EndPaint")
	pFillRect               = user32.NewProc("FillRect")
	pDrawTextW              = user32.NewProc("DrawTextW")
	pInvalidateRect         = user32.NewProc("InvalidateRect")
	pLoadImageW             = user32.NewProc("LoadImageW")
	pDrawIconEx             = user32.NewProc("DrawIconEx")
	pGetWindowTextW         = user32.NewProc("GetWindowTextW")
	pGetFocus               = user32.NewProc("GetFocus")
	pDrawFocusRect          = user32.NewProc("DrawFocusRect")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pCreatePen              = gdi32.NewProc("CreatePen")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pGetStockObject         = gdi32.NewProc("GetStockObject")
	pGetUserDefaultLocale   = kernel32.NewProc("GetUserDefaultLocaleName")
	pGetUserDefaultUILang   = kernel32.NewProc("GetUserDefaultUILanguage")
	pGetDynamicTimeZoneInfo = kernel32.NewProc("GetDynamicTimeZoneInformation")
	pCreateFontIndirectW    = gdi32.NewProc("CreateFontIndirectW")
	pGetDeviceCaps          = gdi32.NewProc("GetDeviceCaps")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pInitCommonControlsEx   = comctl32.NewProc("InitCommonControlsEx")
	pGetOpenFileNameW       = comdlg32.NewProc("GetOpenFileNameW")
	pCoInitializeEx         = ole32.NewProc("CoInitializeEx")
)

const (
	wsOverlapped    = 0x00000000
	wsCaption       = 0x00C00000
	wsSysMenu       = 0x00080000
	wsMinimizeBox   = 0x00020000
	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsTabStop       = 0x00010000
	wsVScroll       = 0x00200000
	wsExClientEdge  = 0x00000200
	wsExControlPar  = 0x00010000
	bsPushButton    = 0x0
	bsDefPushButton = 0x1
	lbsNotify       = 0x1
	lbsNoIntegral   = 0x100
	ssLeft          = 0x0
	ssNoPrefix      = 0x80
	swShow          = 5

	bsOwnerDraw = 0xB

	wmDestroy        = 0x0002
	wmPaint          = 0x000F
	wmClose          = 0x0010
	wmSetFont        = 0x0030
	wmDrawItem       = 0x002B
	wmKeyDown        = 0x0100
	bmClick          = 0x00F5
	vkReturn         = 0x0D
	wmCommand        = 0x0111
	wmTimer          = 0x0113
	wmCtlColorBtn    = 0x0135
	wmCtlColorStatic = 0x0138
	wmDeviceChange   = 0x0219

	lbAddString    = 0x0180
	lbResetContent = 0x0184
	lbSetCurSel    = 0x0186
	lbGetCurSel    = 0x0188
	lbnSelChange   = 1
	pbmSetPos      = 0x0402
	pbmSetRange32  = 0x0406

	mbOK          = 0x0
	mbYesNo       = 0x4
	mbIconError   = 0x10
	mbIconWarning = 0x30
	mbIconInfo    = 0x40
	mbDefButton2  = 0x100
	idYes         = 6

	colorWindow       = 5
	transparent       = 1
	logPixelsY        = 90
	spiGetNonClient   = 0x0029
	smCxScreen        = 0
	smCyScreen        = 1
	iccStandard       = 0x4000
	iccProgress       = 0x0020
	idcArrow          = 32512
	idiApplication    = 32512
	ofnFileMustExist  = 0x1000
	ofnPathMustExist  = 0x0800
	ofnHideReadOnly   = 0x0004
	ofnNoChangeDir    = 0x0008
	ofnExplorer       = 0x80000
	coinitApartment   = 0x2
	dbtDevNodesChange = 0x0007
	dbtArrival        = 0x8000
	dbtRemoveComplete = 0x8004
	smCxSmIcon        = 49
	imageIcon         = 1
	diNormal          = 3
	nullPen           = 8
	psSolid           = 0
	dtCenter          = 0x1
	dtVCenter         = 0x4
	dtSingleLine      = 0x20
	dtNoPrefix        = 0x800
	dtEndEllipsis     = 0x8000
	odsSelected       = 0x1
	odsDisabled       = 0x4
	odsFocus          = 0x10
	odsNoFocusRect    = 0x200
)

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type drawItemStruct struct {
	CtlType, CtlID, ItemID, ItemAction, ItemState uint32
	Item, Hdc                                     uintptr
	Rect                                          rect
	ItemData                                      uintptr
}

// rgb turns a color into a COLORREF (0x00BBGGRR).
func rgb(c color.NRGBA) uintptr { return uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16 }

func hex(v uint32) color.NRGBA { return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xFF} }

func brush(c color.NRGBA) uintptr {
	b, _, _ := pCreateSolidBrush.Call(rgb(c))
	return b
}

func fillRect(dc uintptr, r rect, br uintptr) { pFillRect.Call(dc, uintptr(unsafe.Pointer(&r)), br) }

// roundRect fills a rectangle with round corners, with a border in border (border.A == 0: no border).
func roundRect(dc uintptr, r rect, radius int32, fill, border color.NRGBA) {
	br := brush(fill)
	var pen uintptr
	if border.A != 0 {
		pen, _, _ = pCreatePen.Call(psSolid, 1, rgb(border))
	} else {
		pen, _, _ = pGetStockObject.Call(nullPen)
	}
	oldB, _, _ := pSelectObject.Call(dc, br)
	oldP, _, _ := pSelectObject.Call(dc, pen)
	right, bottom := r.Right, r.Bottom
	if border.A == 0 { // without a pen RoundRect draws one pixel smaller
		right, bottom = right+1, bottom+1
	}
	pRoundRect.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(right), uintptr(bottom), uintptr(radius), uintptr(radius))
	pSelectObject.Call(dc, oldB)
	pSelectObject.Call(dc, oldP)
	pDeleteObject.Call(br)
	if border.A != 0 {
		pDeleteObject.Call(pen)
	}
}

func drawText(dc uintptr, s string, r rect, font uintptr, c color.NRGBA, flags uintptr) {
	old, _, _ := pSelectObject.Call(dc, font)
	pSetTextColor.Call(dc, rgb(c))
	pSetBkMode.Call(dc, transparent)
	u := utf16.Encode([]rune(s))
	if len(u) > 0 {
		pDrawTextW.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&r)), flags|dtNoPrefix)
	}
	pSelectObject.Call(dc, old)
}

func windowText(h uintptr) string {
	buf := make([]uint16, 256)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct{ X, Y int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type rect struct{ Left, Top, Right, Bottom int32 }

type logFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          byte
	OutPrecision, ClipPrecision, Quality, Pitch    byte
	FaceName                                       [32]uint16
}

type nonClientMetrics struct {
	Size                                   uint32
	BorderWidth, ScrollWidth, ScrollHeight int32
	CaptionWidth, CaptionHeight            int32
	CaptionFont                            logFont
	SmCaptionWidth, SmCaptionHeight        int32
	SmCaptionFont                          logFont
	MenuWidth, MenuHeight                  int32
	MenuFont, StatusFont, MessageFont      logFont
	PaddedBorderWidth                      int32
}

type initCommonControlsEx struct{ Size, ICC uint32 }

type openFileName struct {
	StructSize      uint32
	Owner           uintptr
	Instance        uintptr
	Filter          *uint16
	CustomFilter    *uint16
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            *uint16
	MaxFile         uint32
	FileTitle       *uint16
	MaxFileTitle    uint32
	InitialDir      *uint16
	Title           *uint16
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          *uint16
	CustData        uintptr
	FnHook          uintptr
	TemplateName    *uint16
	Reserved        uintptr
	Reserved2       uint32
	FlagsEx         uint32
}

func u16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("?")
	}
	return p
}

// u16z creates a list of null-terminated texts with a double null at the end (file filter).
func u16z(parts ...string) *uint16 {
	var b []uint16
	for _, p := range parts {
		b = append(b, utf16.Encode([]rune(p))...)
		b = append(b, 0)
	}
	b = append(b, 0)
	return &b[0]
}

func send(h uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(h, uintptr(m), w, l)
	return r
}

func setText(h uintptr, s string) { pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(u16(s)))) }

func enable(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	pEnableWindow.Call(h, v)
}

func messageBox(owner uintptr, text, title string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), uintptr(flags))
	return int(r)
}

// windowsLocale returns the locale (de-AT), keyboard KLID (00000407) and time zone key
// (W. Europe Standard Time) for vr/stick.json (F-STICK-07).
func windowsLocale() (locale, klid, zone string) {
	buf := make([]uint16, 85)
	if r, _, _ := pGetUserDefaultLocale.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r != 0 {
		locale = syscall.UTF16ToString(buf)
	}
	kb := make([]uint16, 9)
	if r, _, _ := pGetKeyboardLayoutNameW.Call(uintptr(unsafe.Pointer(&kb[0]))); r != 0 {
		klid = syscall.UTF16ToString(kb)
	}
	// DYNAMIC_TIME_ZONE_INFORMATION: TimeZoneKeyName starts at byte 172.
	var tz [216]uint16
	if r, _, _ := pGetDynamicTimeZoneInfo.Call(uintptr(unsafe.Pointer(&tz[0]))); r != 0xFFFFFFFF {
		zone = syscall.UTF16ToString(tz[86 : 86+128])
	}
	return
}

// germanUI: is the Windows display language German (F-STICK-09)?
func germanUI() bool {
	r, _, _ := pGetUserDefaultUILang.Call()
	return r&0x3ff == 0x07
}
