// SPDX-License-Identifier: AGPL-3.0-or-later

package install

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vanilla-router/internal/config"
)

type fakeRunner struct {
	calls []string
	stdin map[string]string
	fail  string
}

func (f *fakeRunner) Run(env []string, stdin []byte, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	if stdin != nil {
		f.stdin[name] = string(stdin)
	}
	if f.fail != "" && strings.HasPrefix(call, f.fail) {
		return []byte("kaputt"), errors.New("exit status 1")
	}
	if name == "blkid" {
		if strings.HasSuffix(call, "1") {
			return []byte("ABCD-1234\n"), nil
		}
		return []byte("11111111-2222-3333-4444-555555555555\n"), nil
	}
	return nil, nil
}

func plan(t *testing.T) (Plan, string) {
	t.Helper()
	c, err := config.Default(config.DefaultInput{WANMAC: "02:00:00:00:00:01", LANMACs: []string{"02:00:00:00:00:02"},
		Rand: bytes.NewReader([]byte{1, 2, 3, 4, 5})})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	rootfs := filepath.Join(dir, "rootfs.tar.zst")
	os.WriteFile(rootfs, []byte("x"), 0o644)
	T := filepath.Join(dir, "target")
	return Plan{Disk: "/dev/nvme0n1", Rootfs: rootfs, Config: c, SetupCode: "ABCD-EFGH", Target: T}, T
}

func TestInstallAblauf(t *testing.T) {
	p, T := plan(t)
	r := &fakeRunner{stdin: map[string]string{}}
	var progress []string
	in := &Installer{Run: r, Progress: func(n, total int, s string) { progress = append(progress, s) }}
	if err := in.Install(p); err != nil {
		t.Fatal(err)
	}
	if len(progress) != len(Steps) {
		t.Errorf("Fortschritt %v", progress)
	}
	order := []string{
		"wipefs --all --force /dev/nvme0n1",
		"sfdisk --wipe always",
		"mkfs.vfat -F 32 -n EFI /dev/nvme0n1p1",
		"mkfs.btrfs -f -L vanilla-router /dev/nvme0n1p2",
		"btrfs subvolume create " + filepath.Join(T, "@snapshots"),
		"mount -o subvol=@,compress=zstd:1,noatime /dev/nvme0n1p2 " + T,
		"tar --zstd -xpf",
		"chroot " + T + " grub-install --target=x86_64-efi --efi-directory=/boot/efi --uefi-secure-boot --bootloader-id=vanilla-router",
		"chroot " + T + " grub-install --target=x86_64-efi --efi-directory=/boot/efi --uefi-secure-boot --removable --no-nvram",
		"chroot " + T + " update-grub",
		"systemctl --root=" + T + " enable systemd-networkd.service",
		"chroot " + T + " useradd",
		"umount -R " + T,
	}
	i := 0
	for _, c := range r.calls {
		if i < len(order) && strings.HasPrefix(c, order[i]) {
			i++
		}
	}
	if i != len(order) {
		t.Errorf("Reihenfolge stimmt nicht, fehlt ab %q.\nAufrufe:\n%s", order[i], strings.Join(r.calls, "\n"))
	}
	for _, c := range r.calls {
		if strings.Contains(c, "--now") {
			t.Errorf("--now im Offline-Modus: %s", c)
		}
	}
	if !strings.Contains(r.stdin["sfdisk"], "label: gpt") {
		t.Error("Partitionstabelle nicht übergeben")
	}
	read := func(rel string) string { b, _ := os.ReadFile(filepath.Join(T, rel)); return string(b) }
	if f := read("etc/fstab"); !strings.Contains(f, "UUID=11111111-2222-3333-4444-555555555555  /            btrfs  subvol=@,") ||
		!strings.Contains(f, "UUID=ABCD-1234  /boot/efi") {
		t.Errorf("fstab:\n%s", f)
	}
	if read("etc/hostname") != "router\n" || !strings.Contains(read("etc/hosts"), "router.home.arpa router") {
		t.Error("Hostname")
	}
	if l, _ := os.Readlink(filepath.Join(T, "etc/localtime")); l != "../usr/share/zoneinfo/Europe/Berlin" {
		t.Errorf("localtime %q", l)
	}
	if !strings.Contains(read("etc/vanilla-router/nft/vr.nft"), "table inet vr_filter") {
		t.Error("Firewall nicht geschrieben")
	}
	if _, err := config.Load(filepath.Join(T, config.Path)); err != nil {
		t.Error(err)
	}
	if fi, err := os.Stat(filepath.Join(T, "etc/vanilla-router/secrets/webui-setup-code")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("Einrichtungscode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(T, "etc/cloud/cloud-init.disabled")); err != nil {
		t.Error("cloud-init nicht abgeschaltet")
	}
}

func TestFehlerVorDemLoeschen(t *testing.T) {
	p, _ := plan(t)
	p.Config.WAN.Type = config.WANPPPoE
	p.Config.WAN.PPPoE = &config.PPPoE{User: "u@example.org", PasswordRef: "pppoe"}
	r := &fakeRunner{stdin: map[string]string{}}
	if err := (&Installer{Run: r}).Install(p); err == nil {
		t.Fatal("fehlendes PPPoE-Passwort nicht bemerkt")
	}
	if len(r.calls) != 0 {
		t.Errorf("Datenträger angefasst, obwohl die Konfiguration unvollständig ist: %v", r.calls)
	}
	p, _ = plan(t)
	os.Remove(p.Rootfs)
	if err := (&Installer{Run: r}).Install(p); err == nil || len(r.calls) != 0 {
		t.Errorf("fehlendes Image: %v %v", err, r.calls)
	}
}

func TestFehlerHaengtAus(t *testing.T) {
	p, T := plan(t)
	r := &fakeRunner{stdin: map[string]string{}, fail: "chroot " + T + " update-grub"}
	err := (&Installer{Run: r}).Install(p)
	if err == nil || !strings.Contains(err.Error(), "update-grub") {
		t.Fatalf("%v", err)
	}
	if last := r.calls[len(r.calls)-1]; last != "umount -R "+T {
		t.Errorf("nicht ausgehängt, letzter Aufruf %q", last)
	}
}
