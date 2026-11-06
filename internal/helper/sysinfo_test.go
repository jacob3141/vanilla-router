// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"
)

func TestNVMeCmdSize(t *testing.T) {
	// Must match struct nvme_admin_cmd (72 bytes) exactly, otherwise the ioctl number does not fit
	if s := unsafe.Sizeof(nvmeAdminCmd{}); s != 72 {
		t.Fatalf("nvmeAdminCmd is %d bytes, expected 72", s)
	}
}

func TestParseSMARTLog(t *testing.T) {
	d := make([]byte, 512)
	d[0] = 0
	binary.LittleEndian.PutUint16(d[1:3], 273+38) // 38 °C
	d[3], d[4], d[5] = 100, 10, 3
	binary.LittleEndian.PutUint64(d[32:40], 2_000_000) // 2 million units × 512000 bytes = 1.024 TB
	binary.LittleEndian.PutUint64(d[48:56], 4_000_000)
	binary.LittleEndian.PutUint64(d[112:120], 57)
	binary.LittleEndian.PutUint64(d[128:136], 9123)
	binary.LittleEndian.PutUint64(d[144:152], 12)
	h := parseSMARTLog(d)
	if h.TemperatureC != 38 || h.PercentUsed != 3 || h.AvailableSpare != 100 || h.PowerOnHours != 9123 || h.PowerCycles != 57 || h.UnsafeShutdowns != 12 {
		t.Errorf("read wrongly: %+v", h)
	}
	if math.Abs(h.DataReadTB-1.024) > 1e-9 || math.Abs(h.DataWrittenTB-2.048) > 1e-9 {
		t.Errorf("data amount wrong: %v / %v", h.DataReadTB, h.DataWrittenTB)
	}
}

func TestAptHistoryRegex(t *testing.T) {
	m := pkgListRe.FindAllStringSubmatch("libc6:amd64 (2.42-0ubuntu3, 2.42-0ubuntu3.1), openssh-server:amd64 (1:10.0p1-5ubuntu5, 1:10.0p1-5ubuntu5.1), linux-image-7.0.0-43-generic:amd64 (7.0.0-43.43, automatic)", -1)
	if len(m) != 3 || m[0][1] != "libc6" || m[1][1] != "openssh-server" || m[2][1] != "linux-image-7.0.0-43-generic" {
		t.Errorf("package names wrong: %v", m)
	}
}
