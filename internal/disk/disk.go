// SPDX-License-Identifier: AGPL-3.0-or-later

// Package disk findet Datenträger für die Installation und plant die Partitionen (PLAN.md 3.9.4).
package disk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MinSize ist die kleinste erlaubte Größe des Ziel-Datenträgers (NF-01: „16 GB“). Hersteller rechnen
// dezimal und ein 16-GB-Speicher hat oft etwas weniger, deshalb 15·10⁹ Byte.
const MinSize = 15_000_000_000

// Disk ist ein Datenträger.
type Disk struct {
	Name       string // sda, nvme0n1, mmcblk0
	Path       string // /dev/sda
	SizeBytes  int64
	Model      string
	Removable  bool
	Rotational bool
	Transport  string // nvme, usb, sata (soweit erkennbar)
	Boot       bool   // trägt das gerade laufende System (der Stick), nie als Ziel anbieten
}

// Label für Laien: „NVMe-SSD Samsung 970 (256 GB)“.
func (d Disk) Label() string {
	kind := "SSD"
	switch {
	case d.Transport == "nvme":
		kind = "NVMe-SSD"
	case d.Transport == "usb" || d.Removable:
		kind = "USB-Datenträger"
	case d.Rotational:
		kind = "Festplatte"
	case strings.HasPrefix(d.Name, "mmcblk"):
		kind = "eMMC-Speicher"
	}
	s := kind
	if d.Model != "" {
		s += " " + d.Model
	}
	return fmt.Sprintf("%s (%s)", s, HumanSize(d.SizeBytes))
}

// HumanSize schreibt Größen wie Hersteller (Dezimal-GB), damit sie zum Aufdruck passen.
func HumanSize(b int64) string {
	switch {
	case b >= 1e12:
		return fmt.Sprintf("%.1f TB", float64(b)/1e12)
	case b >= 1e9:
		return fmt.Sprintf("%.0f GB", float64(b)/1e9)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/1e6)
}

// List liest sysRoot/block. bootDisk (z. B. "sdb") wird als Boot markiert.
func List(sysRoot, bootDisk string) ([]Disk, error) {
	ents, err := os.ReadDir(filepath.Join(sysRoot, "block"))
	if err != nil {
		return nil, err
	}
	var out []Disk
	for _, e := range ents {
		n := e.Name()
		if skipName(n) {
			continue
		}
		dir := filepath.Join(sysRoot, "block", n)
		sectors, err := readInt(filepath.Join(dir, "size"))
		if err != nil || sectors == 0 {
			continue // leere Kartenleser u. ä.
		}
		d := Disk{Name: n, Path: "/dev/" + n, SizeBytes: sectors * 512, Boot: n == bootDisk}
		d.Removable = readStr(filepath.Join(dir, "removable")) == "1"
		d.Rotational = readStr(filepath.Join(dir, "queue", "rotational")) == "1"
		d.Model = strings.Join(strings.Fields(readStr(filepath.Join(dir, "device", "model"))), " ")
		if d.Model == "" {
			d.Model = readStr(filepath.Join(dir, "device", "name")) // mmc
		}
		if target, err := filepath.EvalSymlinks(dir); err == nil {
			switch {
			case strings.HasPrefix(n, "nvme"):
				d.Transport = "nvme"
			case strings.Contains(target, "/usb"):
				d.Transport = "usb"
			case strings.Contains(target, "/ata"):
				d.Transport = "sata"
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func skipName(n string) bool {
	for _, p := range []string{"loop", "ram", "zram", "sr", "dm-", "md", "fd", "nbd"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return strings.HasPrefix(n, "mmcblk") && strings.Contains(n, "boot") // eMMC-Bootpartitionen
}

// Candidates liefert die Datenträger, auf die installiert werden darf, größter interner zuerst.
func Candidates(ds []Disk) []Disk {
	var out []Disk
	for _, d := range ds {
		if !d.Boot && d.SizeBytes >= MinSize {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ui, uj := out[i].Transport == "usb", out[j].Transport == "usb"; ui != uj {
			return uj // interne zuerst
		}
		return out[i].SizeBytes > out[j].SizeBytes
	})
	return out
}

// BootDisk findet den Datenträger, auf dem mountpoint (normal "/" oder das Live-Medium) liegt.
// sysRoot und mountinfo sind für Tests austauschbar.
func BootDisk(sysRoot, mountinfo, mountpoint string) string {
	b, err := os.ReadFile(mountinfo)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[4] != mountpoint {
			continue
		}
		// Felder nach " - ": Typ, Quelle
		for i := 6; i < len(f)-2; i++ {
			if f[i] == "-" {
				src := f[i+2]
				if !strings.HasPrefix(src, "/dev/") {
					return ""
				}
				return ParentDisk(sysRoot, strings.TrimPrefix(src, "/dev/"))
			}
		}
	}
	return ""
}

// ParentDisk liefert zu einer Partition (sda1, nvme0n1p2) den Datenträger (sda, nvme0n1).
func ParentDisk(sysRoot, part string) string {
	link, err := filepath.EvalSymlinks(filepath.Join(sysRoot, "class", "block", part))
	if err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(link, "partition")); err == nil {
		return filepath.Base(filepath.Dir(link))
	}
	return part
}

// PartPath liefert den Gerätepfad der n-ten Partition: /dev/sda → /dev/sda1, /dev/nvme0n1 → /dev/nvme0n1p1.
func PartPath(dev string, n int) string {
	if last := dev[len(dev)-1]; last >= '0' && last <= '9' {
		return dev + "p" + strconv.Itoa(n)
	}
	return dev + strconv.Itoa(n)
}

// GPT-Typen: EFI-Systempartition und Linux-Root x86-64 nach der Discoverable Partitions Specification.
const (
	TypeESP      = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
	TypeRootX64  = "4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709"
	ESPSizeMiB   = 1024
	RootLabel    = "vanilla-router"
	ESPLabel     = "EFI"
	SubvolRoot   = "@"
	SubvolLog    = "@log"
	SubvolSnaps  = "@snapshots"
	MountOptions = "compress=zstd:1,noatime"
)

// SfdiskScript ist die Partitionstabelle: 1 GiB ESP, Rest btrfs (PLAN.md 3.9.4).
func SfdiskScript() string {
	return fmt.Sprintf("label: gpt\nsize=%dMiB, type=%s, name=\"%s\"\ntype=%s, name=\"%s\"\n",
		ESPSizeMiB, TypeESP, ESPLabel, TypeRootX64, RootLabel)
}

func readStr(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

func readInt(p string) (int64, error) {
	return strconv.ParseInt(readStr(p), 10, 64)
}
