// SPDX-License-Identifier: AGPL-3.0-or-later

package windisk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                         = syscall.NewLazyDLL("kernel32.dll")
	procFindFirstVolumeW             = kernel32.NewProc("FindFirstVolumeW")
	procFindNextVolumeW              = kernel32.NewProc("FindNextVolumeW")
	procFindVolumeClose              = kernel32.NewProc("FindVolumeClose")
	procGetVolumePathNamesForVolumeW = kernel32.NewProc("GetVolumePathNamesForVolumeNameW")
	procGetVolumePathNameW           = kernel32.NewProc("GetVolumePathNameW")
	procGetVolumeNameForMountPointW  = kernel32.NewProc("GetVolumeNameForVolumeMountPointW")
	procGetWindowsDirectoryW         = kernel32.NewProc("GetWindowsDirectoryW")
)

// Steuercodes aus winioctl.h.
const (
	ioctlStorageQueryProperty     = 0x002D1400
	ioctlDiskGetDriveGeometryEx   = 0x000700A0
	ioctlDiskIsWritable           = 0x00070024
	ioctlDiskUpdateProperties     = 0x00070140
	ioctlVolumeGetDiskExtents     = 0x00560000
	fsctlLockVolume               = 0x00090018
	fsctlUnlockVolume             = 0x0009001C
	fsctlDismountVolume           = 0x00090020
	busTypeUsb                    = 7
	fileFlagNoBuffering           = 0x20000000
	fileFlagWriteThrough          = 0x80000000
	errorWriteProtect             = syscall.Errno(19)
	errorNotReady                 = syscall.Errno(21)
	errorBadCommand               = syscall.Errno(22)
	errorGenFailure               = syscall.Errno(31)
	errorNoSuchDevice             = syscall.Errno(433)
	errorDeviceNotConnected       = syscall.Errno(1167)
	errorMediaChanged             = syscall.Errno(1110)
	maxDisks                      = 64
	shareRW                       = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE
	accessRW                      = syscall.GENERIC_READ | syscall.GENERIC_WRITE
	lockTries                     = 20
	lockWait                      = 250 * time.Millisecond
	volumeNameLen                 = 64
	volumePathsLen                = 1024
	storageDeviceDescriptorBuffer = 1024
)

func open(path string, access uint32, flags uint32) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	return syscall.CreateFile(p, access, shareRW, nil, syscall.OPEN_EXISTING, flags, 0)
}

func ioctl(h syscall.Handle, code uint32, in []byte, out []byte) (int, error) {
	var inp, outp *byte
	if len(in) > 0 {
		inp = &in[0]
	}
	if len(out) > 0 {
		outp = &out[0]
	}
	var n uint32
	err := syscall.DeviceIoControl(h, code, inp, uint32(len(in)), outp, uint32(len(out)), &n, nil)
	return int(n), err
}

func diskPath(n int) string { return fmt.Sprintf(`\\.\PhysicalDrive%d`, n) }

// describe liest Bus, Hersteller und Produkt (STORAGE_DEVICE_DESCRIPTOR).
func describe(h syscall.Handle) (bus uint32, vendor, product string, err error) {
	q := make([]byte, 12) // STORAGE_PROPERTY_QUERY: StorageDeviceProperty, PropertyStandardQuery
	out := make([]byte, storageDeviceDescriptorBuffer)
	n, err := ioctl(h, ioctlStorageQueryProperty, q, out)
	if err != nil {
		return 0, "", "", err
	}
	if n < 36 {
		return 0, "", "", errors.New("Antwort zu kurz")
	}
	out = out[:n]
	str := func(off uint32) string {
		if off == 0 || int(off) >= len(out) {
			return ""
		}
		s := out[off:]
		if i := strings.IndexByte(string(s), 0); i >= 0 {
			s = s[:i]
		}
		return strings.Join(strings.Fields(string(s)), " ")
	}
	le := binary.LittleEndian
	return le.Uint32(out[28:]), str(le.Uint32(out[12:])), str(le.Uint32(out[16:])), nil
}

// geometry liest Größe und Sektorgröße (DISK_GEOMETRY_EX).
func geometry(h syscall.Handle) (size int64, sector int, err error) {
	out := make([]byte, 256)
	n, err := ioctl(h, ioctlDiskGetDriveGeometryEx, nil, out)
	if err != nil {
		return 0, 0, err
	}
	if n < 32 {
		return 0, 0, errors.New("Antwort zu kurz")
	}
	le := binary.LittleEndian
	return int64(le.Uint64(out[24:])), int(le.Uint32(out[20:])), nil
}

// volume ist ein Volume mit den Datenträgern, auf denen es liegt.
type volume struct {
	name    string // \\?\Volume{…}\
	disks   []int
	letters []string
}

func utf16At(b []uint16) string { return syscall.UTF16ToString(b) }

// volumes zählt alle Volumes auf.
func volumes() ([]volume, error) {
	buf := make([]uint16, volumeNameLen)
	h, _, err := procFindFirstVolumeW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if syscall.Handle(h) == syscall.InvalidHandle {
		return nil, err
	}
	defer procFindVolumeClose.Call(h)
	var vs []volume
	for {
		name := utf16At(buf)
		v := volume{name: name, disks: volumeDisks(name), letters: volumeLetters(name)}
		vs = append(vs, v)
		r, _, _ := procFindNextVolumeW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if r == 0 {
			break
		}
	}
	return vs, nil
}

// volumeDisks liefert die Datenträger eines Volumes (VOLUME_DISK_EXTENTS).
func volumeDisks(name string) []int {
	h, err := open(strings.TrimSuffix(name, `\`), 0, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(h)
	out := make([]byte, 8+24*32)
	n, err := ioctl(h, ioctlVolumeGetDiskExtents, nil, out)
	if err != nil || n < 8 {
		return nil
	}
	le := binary.LittleEndian
	cnt := int(le.Uint32(out))
	var ds []int
	for i := 0; i < cnt && 8+24*(i+1) <= n; i++ {
		ds = append(ds, int(le.Uint32(out[8+24*i:])))
	}
	return ds
}

func volumeLetters(name string) []string {
	p, _ := syscall.UTF16PtrFromString(name)
	buf := make([]uint16, volumePathsLen)
	var n uint32
	r, _, _ := procGetVolumePathNamesForVolumeW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return nil
	}
	var ls []string
	for start := 0; start < len(buf) && buf[start] != 0; {
		end := start
		for end < len(buf) && buf[end] != 0 {
			end++
		}
		ls = append(ls, strings.TrimSuffix(syscall.UTF16ToString(buf[start:end]), `\`))
		start = end + 1
	}
	return ls
}

// DisksOfPath liefert die Datenträger, auf denen eine Datei liegt.
func DisksOfPath(path string) []int {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	p, _ := syscall.UTF16PtrFromString(abs)
	mount := make([]uint16, syscall.MAX_PATH+1)
	if r, _, _ := procGetVolumePathNameW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&mount[0])), uintptr(len(mount))); r == 0 {
		return nil
	}
	vol := make([]uint16, volumeNameLen)
	if r, _, _ := procGetVolumeNameForMountPointW.Call(uintptr(unsafe.Pointer(&mount[0])), uintptr(unsafe.Pointer(&vol[0])), uintptr(len(vol))); r == 0 {
		return nil
	}
	return volumeDisks(utf16At(vol))
}

// protectedDisks sind das Systemlaufwerk und der Datenträger, von dem das Programm läuft.
func protectedDisks() map[int]bool {
	prot := map[int]bool{}
	win := make([]uint16, syscall.MAX_PATH+1)
	if r, _, _ := procGetWindowsDirectoryW.Call(uintptr(unsafe.Pointer(&win[0])), uintptr(len(win))); r != 0 {
		for _, d := range DisksOfPath(utf16At(win)) {
			prot[d] = true
		}
	}
	if exe, err := os.Executable(); err == nil {
		for _, d := range DisksOfPath(exe) {
			prot[d] = true
		}
	}
	for _, env := range []string{"SystemDrive", "LOCALAPPDATA", "TEMP"} {
		if v := os.Getenv(env); v != "" {
			for _, d := range DisksOfPath(v + `\`) {
				prot[d] = true
			}
		}
	}
	return prot
}

// List liefert die USB-Datenträger, auf die geschrieben werden darf.
func List() ([]Disk, error) {
	prot := protectedDisks()
	vols, _ := volumes()
	var out []Disk
	for n := 0; n < maxDisks; n++ {
		h, err := open(diskPath(n), 0, 0)
		if err != nil {
			continue
		}
		bus, vendor, product, err := describe(h)
		if err != nil || bus != busTypeUsb || prot[n] {
			syscall.CloseHandle(h)
			continue
		}
		size, sector, err := geometry(h)
		syscall.CloseHandle(h)
		if err != nil || size <= 0 {
			// Kartenleser ohne Karte
			continue
		}
		d := Disk{Number: n, Vendor: vendor, Product: product, Size: size, Sector: sector}
		for _, v := range vols {
			if contains(v.disks, n) {
				d.Letters = append(d.Letters, v.letters...)
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func contains(s []int, n int) bool {
	for _, x := range s {
		if x == n {
			return true
		}
	}
	return false
}

// Device ist ein geöffneter, für das Schreiben vorbereiteter Stick (stickwrite.Device).
type Device struct {
	h      syscall.Handle
	vols   []syscall.Handle
	size   int64
	sector int
}

// Open sperrt alle Volumes des Sticks, hängt sie aus und öffnet ihn zum rohen Schreiben. want muss
// der Eintrag aus List sein; hat sich der Stick inzwischen geändert (anderer Stick, gleiche Nummer),
// gibt es ErrChanged.
func Open(want Disk) (*Device, error) {
	if protectedDisks()[want.Number] {
		return nil, ErrChanged
	}
	q, err := open(diskPath(want.Number), 0, 0)
	if err != nil {
		return nil, mapErr(err)
	}
	bus, vendor, product, err := describe(q)
	var size int64
	var sector int
	if err == nil {
		size, sector, err = geometry(q)
	}
	syscall.CloseHandle(q)
	if err != nil {
		return nil, mapErr(err)
	}
	if bus != busTypeUsb || vendor != want.Vendor || product != want.Product || size != want.Size || sector != want.Sector {
		return nil, ErrChanged
	}

	d := &Device{size: size, sector: sector}
	vols, err := volumes()
	if err != nil {
		return nil, err
	}
	for _, v := range vols {
		if !contains(v.disks, want.Number) {
			continue
		}
		h, err := lockVolume(strings.TrimSuffix(v.name, `\`))
		if err != nil {
			d.Close()
			return nil, err
		}
		d.vols = append(d.vols, h)
	}
	d.h, err = open(diskPath(want.Number), accessRW, fileFlagNoBuffering|fileFlagWriteThrough)
	if err != nil {
		d.Close()
		if err == syscall.ERROR_ACCESS_DENIED {
			return nil, ErrNotAdmin
		}
		return nil, mapErr(err)
	}
	if _, err := ioctl(d.h, ioctlDiskIsWritable, nil, nil); err == errorWriteProtect {
		d.Close()
		return nil, ErrWriteProtected
	}
	return d, nil
}

// lockVolume sperrt ein Volume und hängt es aus. Der Griff bleibt bis Close offen, solange gilt die Sperre.
func lockVolume(path string) (syscall.Handle, error) {
	h, err := open(path, accessRW, 0)
	if err != nil {
		if err == syscall.ERROR_ACCESS_DENIED {
			return syscall.InvalidHandle, ErrNotAdmin
		}
		return syscall.InvalidHandle, mapErr(err)
	}
	locked := false
	for i := 0; i < lockTries; i++ {
		if _, err := ioctl(h, fsctlLockVolume, nil, nil); err == nil {
			locked = true
			break
		}
		time.Sleep(lockWait)
	}
	if !locked {
		// Noch offene Dateien (z. B. im Explorer): Aushängen erzwingt das Schließen, dann erneut sperren.
		ioctl(h, fsctlDismountVolume, nil, nil)
		if _, err := ioctl(h, fsctlLockVolume, nil, nil); err != nil {
			syscall.CloseHandle(h)
			return syscall.InvalidHandle, ErrBusy
		}
	}
	if _, err := ioctl(h, fsctlDismountVolume, nil, nil); err != nil {
		ioctl(h, fsctlUnlockVolume, nil, nil)
		syscall.CloseHandle(h)
		return syscall.InvalidHandle, ErrBusy
	}
	return h, nil
}

func mapErr(err error) error {
	switch err {
	case nil:
		return nil
	case errorWriteProtect:
		return ErrWriteProtected
	case errorNotReady, errorNoSuchDevice, errorDeviceNotConnected, errorMediaChanged, errorBadCommand,
		errorGenFailure, syscall.ERROR_FILE_NOT_FOUND, syscall.ERROR_PATH_NOT_FOUND:
		return fmt.Errorf("%w (%v)", ErrRemoved, err)
	}
	return err
}

func (d *Device) Size() int64     { return d.size }
func (d *Device) SectorSize() int { return d.sector }

func (d *Device) rw(p []byte, off int64, write bool) (int, error) {
	done := 0
	for done < len(p) {
		var ov syscall.Overlapped
		pos := off + int64(done)
		ov.Offset = uint32(pos)
		ov.OffsetHigh = uint32(pos >> 32)
		var n uint32
		var err error
		if write {
			err = syscall.WriteFile(d.h, p[done:], &n, &ov)
		} else {
			err = syscall.ReadFile(d.h, p[done:], &n, &ov)
		}
		if err != nil {
			return done, mapErr(err)
		}
		if n == 0 {
			return done, ErrRemoved
		}
		done += int(n)
	}
	return done, nil
}

// WriteAt schreibt ganze Sektoren (stickwrite sorgt für Ausrichtung).
func (d *Device) WriteAt(p []byte, off int64) (int, error) { return d.rw(p, off, true) }

// ReadAt liest am Datenträger vorbei an jedem Zwischenspeicher (FILE_FLAG_NO_BUFFERING).
func (d *Device) ReadAt(p []byte, off int64) (int, error) { return d.rw(p, off, false) }

// Sync schreibt die Zwischenspeicher des Geräts.
func (d *Device) Sync() error { return mapErr(syscall.FlushFileBuffers(d.h)) }

// Close lässt Windows die neue Partitionstabelle einlesen und gibt die Volumes frei.
func (d *Device) Close() error {
	if d.h != 0 && d.h != syscall.InvalidHandle {
		ioctl(d.h, ioctlDiskUpdateProperties, nil, nil)
		syscall.CloseHandle(d.h)
		d.h = syscall.InvalidHandle
	}
	for _, h := range d.vols {
		ioctl(h, fsctlUnlockVolume, nil, nil)
		syscall.CloseHandle(h)
	}
	d.vols = nil
	return nil
}
