// SPDX-License-Identifier: AGPL-3.0-or-later

// Package windisk finds USB sticks on Windows and opens them for raw writing (F-STICK-03,
// F-STICK-05, PLAN.md 3.12). Only disks on the USB bus, never the system drive and never the disk
// the program itself runs from.
package windisk

import "errors"

// Disk is a disk that may be written to.
type Disk struct {
	Number  int      // \\.\PhysicalDriveN
	Vendor  string   // manufacturer according to the device, can be empty
	Product string   // product name according to the device
	Size    int64    // Bytes
	Sector  int      // logical sector size
	Letters []string // drive letters of the volumes, e.g. "E:"
}

// Name is the display name, e.g. "SanDisk Ultra".
func (d Disk) Name() string {
	switch {
	case d.Vendor != "" && d.Product != "":
		return d.Vendor + " " + d.Product
	case d.Product != "":
		return d.Product
	case d.Vendor != "":
		return d.Vendor
	}
	return "USB drive"
}

// LargeDisk is the limit from which an additional confirmation is needed (F-STICK-03).
const LargeDisk = 256_000_000_000

// Errors for plain-language messages (F-STICK-06).
var (
	ErrWriteProtected = errors.New("the stick is write-protected")
	ErrRemoved        = errors.New("the stick was unplugged")
	ErrBusy           = errors.New("the stick is still being used by a program")
	ErrChanged        = errors.New("the stick has changed")
	ErrNotAdmin       = errors.New("no administrator rights")
)
