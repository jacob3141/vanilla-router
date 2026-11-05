// SPDX-License-Identifier: AGPL-3.0-or-later

// Package windisk findet USB-Sticks unter Windows und öffnet sie zum rohen Schreiben (F-STICK-03,
// F-STICK-05, PLAN.md 3.12). Nur Datenträger am USB-Bus, nie das Systemlaufwerk und nie der Datenträger,
// von dem das Programm selbst läuft.
package windisk

import "errors"

// Disk ist ein Datenträger, auf den geschrieben werden darf.
type Disk struct {
	Number  int      // \\.\PhysicalDriveN
	Vendor  string   // Hersteller laut Gerät, kann leer sein
	Product string   // Produktname laut Gerät
	Size    int64    // Bytes
	Sector  int      // logische Sektorgröße
	Letters []string // Laufwerksbuchstaben der Volumes, z. B. "E:"
}

// Name ist der Anzeigename, z. B. „SanDisk Ultra“.
func (d Disk) Name() string {
	switch {
	case d.Vendor != "" && d.Product != "":
		return d.Vendor + " " + d.Product
	case d.Product != "":
		return d.Product
	case d.Vendor != "":
		return d.Vendor
	}
	return "USB-Datenträger"
}

// LargeDisk ist die Grenze, ab der eine zusätzliche Bestätigung nötig ist (F-STICK-03).
const LargeDisk = 256_000_000_000

// Fehler für Laien-Meldungen (F-STICK-06).
var (
	ErrWriteProtected = errors.New("der Stick ist schreibgeschützt")
	ErrRemoved        = errors.New("der Stick wurde abgezogen")
	ErrBusy           = errors.New("der Stick wird noch von einem Programm benutzt")
	ErrChanged        = errors.New("der Stick hat sich geändert")
	ErrNotAdmin       = errors.New("keine Administratorrechte")
)
