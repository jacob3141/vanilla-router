// SPDX-License-Identifier: AGPL-3.0-or-later

// Package install writes the target system to the disk (PLAN.md 3.9.2 S8, 3.9.4):
// partitions (GPT: ESP + btrfs with @, @log, @snapshots, @config, @state), system from the image, settings from
// config.json, bootloader (shim + GRUB, Secure Boot, Q-16) and services. Nothing is written to
// the disk before this step (a principle of the assistant).
package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vanilla-router/internal/apply"
	"vanilla-router/internal/config"
	"vanilla-router/internal/disk"
	"vanilla-router/internal/gen"
)

// Runner runs programs, optionally with input on stdin.
type Runner interface {
	Run(env []string, stdin []byte, name string, args ...string) ([]byte, error)
}

// Plan describes an installation.
type Plan struct {
	Disk      string // device, e.g. /dev/sda (will be completely erased)
	Rootfs    string // image of the target system (tar.zst)
	Config    *config.Config
	Secrets   map[string]string // e.g. "pppoe" → password
	SetupCode string            // setup code of the web interface
	Target    string            // mount point, default /mnt/vr-target
}

// Installer executes a plan.
type Installer struct {
	Run Runner
	// Progress reports the progress for display (step n of total).
	Progress func(n, total int, text string)
}

// Steps are the steps with texts for non-experts.
var Steps = []string{
	"Preparing the disk",
	"Creating file systems",
	"Copying the system (takes a few minutes)",
	"Writing settings",
	"Setting up the boot loader",
	"Setting up services",
	"Finishing",
}

type mapSecrets map[string]string

func (m mapSecrets) Get(ref string) (string, error) {
	if v, ok := m[ref]; ok {
		return v, nil
	}
	return "", fmt.Errorf("secret %q is missing", ref)
}

// Install runs all steps. On an error everything is unmounted, the disk stays in an
// unfinished state and can be overwritten with a new installation.
func (in *Installer) Install(p Plan) (err error) {
	if p.Target == "" {
		p.Target = "/mnt/vr-target"
	}
	if err := p.Config.Validate(); err != nil {
		return err
	}
	// Generate before the first write: an error in the configuration or secrets
	// should come to light before the disk is erased.
	files, err := gen.Render(p.Config, mapSecrets(p.Secrets))
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Rootfs); err != nil {
		return fmt.Errorf("image of the target system is missing: %w", err)
	}
	T := p.Target
	esp, root := disk.PartPath(p.Disk, 1), disk.PartPath(p.Disk, 2)
	run := func(stdin []byte, name string, args ...string) error {
		out, err := in.Run.Run([]string{"LC_ALL=C"}, stdin, name, args...)
		if err != nil {
			return fmt.Errorf("%s %s: %v\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	chroot := func(name string, args ...string) error {
		return run(nil, "chroot", append([]string{T, name}, args...)...)
	}
	// The kernel's file systems under the target can stay busy (cgroup under /sys), and that says nothing
	// about the installation. So they are detached lazily. The target itself is then unmounted strictly:
	// only that guarantees that everything is on the disk.
	detachAPI := func() {
		for _, dir := range []string{"proc", "run", "sys", "dev"} {
			in.Run.Run(nil, nil, "umount", "-R", "-l", filepath.Join(T, dir))
		}
	}
	mounted := false
	defer func() {
		if mounted {
			detachAPI()
			in.Run.Run(nil, nil, "umount", "-R", T)
		}
	}()
	step := 0
	next := func() {
		step++
		if in.Progress != nil {
			in.Progress(step, len(Steps), Steps[step-1])
		}
	}

	next() // Prepare the disk
	if err := run(nil, "wipefs", "--all", "--force", p.Disk); err != nil {
		return err
	}
	if err := run([]byte(disk.SfdiskScript()), "sfdisk", "--wipe", "always", "--wipe-partitions", "always", p.Disk); err != nil {
		return err
	}
	if err := run(nil, "udevadm", "settle", "--timeout=30"); err != nil {
		return err
	}

	next() // File systems
	if err := run(nil, "mkfs.vfat", "-F", "32", "-n", disk.ESPLabel, esp); err != nil {
		return err
	}
	if err := run(nil, "mkfs.btrfs", "-f", "-L", disk.RootLabel, root); err != nil {
		return err
	}
	if err := os.MkdirAll(T, 0o755); err != nil {
		return err
	}
	if err := run(nil, "mount", root, T); err != nil {
		return err
	}
	for _, sv := range []string{disk.SubvolRoot, disk.SubvolLog, disk.SubvolSnaps, disk.SubvolConfig, disk.SubvolState} {
		if err := run(nil, "btrfs", "subvolume", "create", filepath.Join(T, sv)); err != nil {
			run(nil, "umount", T)
			return err
		}
	}
	if err := run(nil, "umount", T); err != nil {
		return err
	}
	opts := func(sv string) string { return "subvol=" + sv + "," + disk.MountOptions }
	if err := run(nil, "mount", "-o", opts(disk.SubvolRoot), root, T); err != nil {
		return err
	}
	mounted = true
	for _, m := range []struct{ dir, sv string }{
		{"var/log", disk.SubvolLog}, {".snapshots", disk.SubvolSnaps},
		{"etc/vanilla-router", disk.SubvolConfig}, {"var/lib/vanilla-router", disk.SubvolState},
	} {
		if err := os.MkdirAll(filepath.Join(T, m.dir), 0o755); err != nil {
			return err
		}
		if err := run(nil, "mount", "-o", opts(m.sv), root, filepath.Join(T, m.dir)); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(T, "boot", "efi"), 0o755); err != nil {
		return err
	}
	if err := run(nil, "mount", "-o", "umask=0077", esp, filepath.Join(T, "boot", "efi")); err != nil {
		return err
	}

	next() // Copy the system
	if err := run(nil, "tar", "--zstd", "-xpf", p.Rootfs, "--xattrs", "--xattrs-include=*", "--acls",
		"--numeric-owner", "-C", T); err != nil {
		return err
	}

	next() // Settings
	uuid := func(dev string) (string, error) {
		out, err := in.Run.Run([]string{"LC_ALL=C"}, nil, "blkid", "-s", "UUID", "-o", "value", dev)
		u := strings.TrimSpace(string(out))
		if err != nil || u == "" {
			return "", fmt.Errorf("UUID of %s not readable: %v", dev, err)
		}
		return u, nil
	}
	rootUUID, err := uuid(root)
	if err != nil {
		return err
	}
	espUUID, err := uuid(esp)
	if err != nil {
		return err
	}
	if err := writeSystemFiles(T, p, rootUUID, espUUID, files); err != nil {
		return err
	}

	next() // Boot loader
	for _, b := range []struct{ fs, dir string }{{"--rbind", "dev"}, {"--rbind", "sys"}, {"--bind", "run"}} {
		if err := run(nil, "mount", b.fs, "/"+b.dir, filepath.Join(T, b.dir)); err != nil {
			return err
		}
	}
	if err := run(nil, "mount", "-t", "proc", "proc", filepath.Join(T, "proc")); err != nil {
		return err
	}
	grub := []string{"--target=x86_64-efi", "--efi-directory=/boot/efi", "--uefi-secure-boot"}
	if err := chroot("grub-install", append(grub, "--bootloader-id=vanilla-router")...); err != nil {
		return err
	}
	// Also under the standard path EFI/BOOT/BOOTX64.EFI: some firmware forgets NVRAM entries.
	if err := chroot("grub-install", append(grub, "--removable", "--no-nvram")...); err != nil {
		return err
	}
	if err := chroot("update-grub"); err != nil {
		return err
	}

	next() // Services
	a := &apply.Applier{Root: T, Run: offlineRunner{in.Run}}
	if _, err := a.Setup(false, T); err != nil {
		return err
	}
	if err := chroot("useradd", "--create-home", "--shell", "/bin/bash", "--groups", "sudo", "admin"); err != nil {
		return err
	}
	if err := chroot("passwd", "--lock", "admin"); err != nil {
		return err
	}
	// No login at the console either, whatever the image brought along
	if err := chroot("passwd", "--lock", "root"); err != nil {
		return err
	}

	next() // Finish
	run(nil, "sync")
	detachAPI()
	if err := run(nil, "umount", "-R", T); err != nil {
		return err
	}
	mounted = false
	return nil
}

// offlineRunner adapts the installer runner to apply.Runner.
type offlineRunner struct{ r Runner }

func (o offlineRunner) Run(env []string, name string, args ...string) ([]byte, error) {
	return o.r.Run(env, nil, name, args...)
}

func writeSystemFiles(T string, p Plan, rootUUID, espUUID string, files []gen.File) error {
	c := p.Config
	w := func(rel string, mode os.FileMode, content string) error {
		return config.WriteFileAtomic(filepath.Join(T, filepath.FromSlash(rel)), []byte(content), mode)
	}
	mo := disk.MountOptions
	fstab := fmt.Sprintf(`# Generated by vanilla-router during installation
UUID=%[1]s  /            btrfs  subvol=@,%[3]s          0 0
UUID=%[1]s  /var/log     btrfs  subvol=@log,%[3]s       0 0
UUID=%[1]s  /.snapshots  btrfs  subvol=@snapshots,%[3]s 0 0
UUID=%[1]s  /etc/vanilla-router  btrfs  subvol=@config,%[3]s 0 0
UUID=%[1]s  /var/lib/vanilla-router  btrfs  subvol=@state,%[3]s 0 0
UUID=%[2]s  /boot/efi    vfat   umask=0077               0 1
`, rootUUID, espUUID, mo)
	hosts := fmt.Sprintf("127.0.0.1\tlocalhost\n127.0.1.1\t%s.%s %s\n::1\tlocalhost ip6-localhost ip6-loopback\n",
		c.System.Hostname, c.DNS.Domain, c.System.Hostname)
	steps := []struct {
		rel     string
		mode    os.FileMode
		content string
	}{
		{"/etc/fstab", 0o644, fstab},
		{"/etc/hostname", 0o644, c.System.Hostname + "\n"},
		{"/etc/hosts", 0o644, hosts},
		{"/etc/timezone", 0o644, c.System.Timezone + "\n"},
		{"/etc/default/locale", 0o644, "LANG=" + c.System.Locale + "\n"},
		{"/etc/default/keyboard", 0o644, fmt.Sprintf("XKBMODEL=\"pc105\"\nXKBLAYOUT=%q\nXKBVARIANT=\"\"\nXKBOPTIONS=\"\"\nBACKSPACE=\"guess\"\n", c.System.Keymap)},
		{"/etc/machine-id", 0o444, ""}, // regenerated at the first start, each device gets its own
	}
	for _, s := range steps {
		if err := w(s.rel, s.mode, s.content); err != nil {
			return err
		}
	}
	lt := filepath.Join(T, "etc", "localtime")
	os.Remove(lt)
	if err := os.Symlink("../usr/share/zoneinfo/"+c.System.Timezone, lt); err != nil {
		return err
	}
	if err := c.Save(filepath.Join(T, config.Path)); err != nil {
		return err
	}
	sec := apply.DirSecrets{Dir: filepath.Join(T, "etc", "vanilla-router", "secrets")}
	for k, v := range p.Secrets {
		if err := sec.Set(k, v); err != nil {
			return err
		}
	}
	if p.SetupCode != "" {
		if err := sec.Set("webui-setup-code", p.SetupCode); err != nil {
			return err
		}
	}
	for _, f := range files {
		if err := config.WriteFileAtomic(filepath.Join(T, filepath.FromSlash(f.Path)), f.Content, os.FileMode(f.Mode)); err != nil {
			return err
		}
	}
	return nil
}

// ExecRunner is the real runner.
type ExecRunner struct{}

// Run starts a program.
func (ExecRunner) Run(env []string, stdin []byte, name string, args ...string) ([]byte, error) {
	return execRun(env, stdin, name, args...)
}

var errNoRoot = errors.New("installation needs root")
