// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// --- NVMe health (SMART log via ioctl, there is no smartctl) ------------------------

type nvmeAdminCmd struct {
	Opcode      uint8
	Flags       uint8
	Rsvd1       uint16
	NSID        uint32
	Cdw2, Cdw3  uint32
	Metadata    uint64
	Addr        uint64
	MetadataLen uint32
	DataLen     uint32
	Cdw10       uint32
	Cdw11       uint32
	Cdw12       uint32
	Cdw13       uint32
	Cdw14       uint32
	Cdw15       uint32
	TimeoutMs   uint32
	Result      uint32
}

const nvmeIoctlAdminCmd = 0xC0484E41 // _IOWR('N', 0x41, struct nvme_admin_cmd), 72 Byte

type NVMeHealth struct {
	Device          string  `json:"device"`
	Model           string  `json:"model"`
	Firmware        string  `json:"firmware"`
	CriticalWarning uint8   `json:"critical_warning"`
	TemperatureC    int     `json:"temperature_c"`
	AvailableSpare  uint8   `json:"available_spare"`
	SpareThreshold  uint8   `json:"spare_threshold"`
	PercentUsed     uint8   `json:"percent_used"`
	DataReadTB      float64 `json:"data_read_tb"`
	DataWrittenTB   float64 `json:"data_written_tb"`
	PowerCycles     uint64  `json:"power_cycles"`
	PowerOnHours    uint64  `json:"power_on_hours"`
	UnsafeShutdowns uint64  `json:"unsafe_shutdowns"`
	MediaErrors     uint64  `json:"media_errors"`
}

func le128(b []byte) *big.Int {
	r := make([]byte, 16)
	for i := 0; i < 16; i++ {
		r[15-i] = b[i]
	}
	return new(big.Int).SetBytes(r)
}

func lowU64(b []byte) uint64 { return binary.LittleEndian.Uint64(b[:8]) }

// parseSMARTLog reads the 512 bytes of NVMe log page 0x02.
func parseSMARTLog(d []byte) NVMeHealth {
	unitsTB := func(b []byte) float64 {
		// One data unit is 1000 blocks of 512 bytes
		v := new(big.Float).SetInt(le128(b))
		v.Mul(v, big.NewFloat(512000))
		v.Quo(v, big.NewFloat(1e12))
		f, _ := v.Float64()
		return f
	}
	return NVMeHealth{
		CriticalWarning: d[0],
		TemperatureC:    int(binary.LittleEndian.Uint16(d[1:3])) - 273,
		AvailableSpare:  d[3],
		SpareThreshold:  d[4],
		PercentUsed:     d[5],
		DataReadTB:      unitsTB(d[32:48]),
		DataWrittenTB:   unitsTB(d[48:64]),
		PowerCycles:     lowU64(d[112:128]),
		PowerOnHours:    lowU64(d[128:144]),
		UnsafeShutdowns: lowU64(d[144:160]),
		MediaErrors:     lowU64(d[160:176]),
	}
}

func readNVMeHealth(dev string) (NVMeHealth, error) {
	f, err := os.OpenFile("/dev/"+dev, os.O_RDONLY, 0)
	if err != nil {
		return NVMeHealth{}, err
	}
	defer f.Close()
	buf := make([]byte, 512)
	cmd := nvmeAdminCmd{
		Opcode:  0x02, // Get Log Page
		NSID:    0xffffffff,
		Addr:    uint64(uintptr(unsafe.Pointer(&buf[0]))),
		DataLen: uint32(len(buf)),
		// Log page 0x02 (SMART), number of dwords - 1 in the upper 16 bits
		Cdw10:     0x02 | (uint32(len(buf)/4-1) << 16),
		TimeoutMs: 5000,
	}
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), nvmeIoctlAdminCmd, uintptr(unsafe.Pointer(&cmd)))
	runtime.KeepAlive(buf) // the kernel writes through the address in cmd.Addr
	if e != 0 {
		return NVMeHealth{}, e
	}
	h := parseSMARTLog(buf)
	h.Device = dev
	base := "/sys/class/nvme/" + strings.TrimSuffix(dev, "n1")
	h.Model = readTrim(base + "/model")
	h.Firmware = readTrim(base + "/firmware_rev")
	return h, nil
}

func readTrim(p string) string {
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

type Mount struct {
	Path  string `json:"path"`
	Dev   string `json:"device"`
	FS    string `json:"fs"`
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}

func listMounts() []Mount {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Mount
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		// Only the first entry per device: the helper's sandbox bind-mounts / several times (/home, /root …)
		if len(fl) < 3 || !strings.HasPrefix(fl[0], "/dev/") || seen[fl[0]] || strings.HasPrefix(fl[1], "/snap") || strings.Contains(fl[1], "/docker/") {
			continue
		}
		seen[fl[0]] = true
		var st syscall.Statfs_t
		if syscall.Statfs(fl[1], &st) != nil {
			continue
		}
		out = append(out, Mount{Path: fl[1], Dev: fl[0], FS: fl[2], Total: st.Blocks * uint64(st.Bsize), Free: st.Bavail * uint64(st.Bsize)})
	}
	return out
}

// --- fail2ban --------------------------------------------------------------------------

type Jail struct {
	Name          string   `json:"name"`
	CurrentFailed int      `json:"current_failed"`
	TotalFailed   int      `json:"total_failed"`
	CurrentBanned int      `json:"current_banned"`
	TotalBanned   int      `json:"total_banned"`
	Banned        []string `json:"banned"`
}

func fail2banJails(ctx context.Context) ([]string, error) {
	out, err := run(ctx, "fail2ban-client", "status")
	if err != nil {
		return nil, Userf("fail2ban is not responding.")
	}
	for _, line := range strings.Split(out, "\n") {
		if _, list, ok := strings.Cut(line, "Jail list:"); ok {
			var jails []string
			for _, j := range strings.Split(list, ",") {
				if j = strings.TrimSpace(j); j != "" {
					jails = append(jails, j)
				}
			}
			return jails, nil
		}
	}
	return nil, nil
}

func fail2banStatus(ctx context.Context) ([]Jail, error) {
	names, err := fail2banJails(ctx)
	if err != nil {
		return nil, err
	}
	var jails []Jail
	for _, n := range names {
		out, err := run(ctx, "fail2ban-client", "status", n)
		if err != nil {
			continue
		}
		j := Jail{Name: n, Banned: []string{}}
		for _, line := range strings.Split(out, "\n") {
			_, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			num, _ := strconv.Atoi(v)
			switch {
			case strings.Contains(line, "Currently failed"):
				j.CurrentFailed = num
			case strings.Contains(line, "Total failed"):
				j.TotalFailed = num
			case strings.Contains(line, "Currently banned"):
				j.CurrentBanned = num
			case strings.Contains(line, "Total banned"):
				j.TotalBanned = num
			case strings.Contains(line, "Banned IP list"):
				j.Banned = append(j.Banned, strings.Fields(v)...)
			}
		}
		jails = append(jails, j)
	}
	return jails, nil
}

// --- Update history (apt) --------------------------------------------------------------

type AptRun struct {
	Start     string   `json:"start"`
	Command   string   `json:"command"`
	Upgraded  []string `json:"upgraded"`
	Installed []string `json:"installed"`
	Removed   []string `json:"removed"`
	Error     string   `json:"error,omitempty"`
}

var pkgListRe = regexp.MustCompile(`([a-z0-9][a-z0-9+.\-]*)(?::\w+)? \(`)

func aptHistory(limit int) []AptRun {
	b, err := os.ReadFile("/var/log/apt/history.log")
	if err != nil {
		return nil
	}
	var runs []AptRun
	var cur *AptRun
	for _, line := range strings.Split(string(b), "\n") {
		k, v, _ := strings.Cut(line, ": ")
		switch k {
		case "Start-Date":
			runs = append(runs, AptRun{Start: v})
			cur = &runs[len(runs)-1]
		case "Commandline":
			if cur != nil {
				cur.Command = v
			}
		case "Upgrade", "Install", "Remove", "Purge":
			if cur == nil {
				continue
			}
			var names []string
			for _, m := range pkgListRe.FindAllStringSubmatch(v, -1) {
				names = append(names, m[1])
			}
			switch k {
			case "Upgrade":
				cur.Upgraded = append(cur.Upgraded, names...)
			case "Install":
				cur.Installed = append(cur.Installed, names...)
			default:
				cur.Removed = append(cur.Removed, names...)
			}
		case "Error":
			if cur != nil {
				cur.Error = v
			}
		}
	}
	slices.Reverse(runs)
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return runs
}

// --- Samba ------------------------------------------------------------------------------

func sambaStatus(ctx context.Context) (any, error) {
	out, err := run(ctx, "smbstatus", "--json")
	if err != nil {
		return nil, Userf("smbstatus is not responding.")
	}
	var v any
	if json.Unmarshal([]byte(out), &v) != nil {
		return nil, Userf("smbstatus does not return JSON.")
	}
	return v, nil
}

// --- Actions ---------------------------------------------------------------------------

func (s *Server) registerSysinfo() {
	s.handle("disk.status.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		var health []NVMeHealth
		devs, _ := filepath.Glob("/dev/nvme[0-9]n1")
		var errs []string
		for _, d := range devs {
			h, err := readNVMeHealth(filepath.Base(d))
			if err != nil {
				errs = append(errs, filepath.Base(d)+": "+err.Error())
				continue
			}
			health = append(health, h)
		}
		return map[string]any{"mounts": listMounts(), "nvme": health, "errors": errs}, nil
	})

	s.handle("hw.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		get := func(k string) string { o, _ := run(ctx, "dmidecode", "-s", k); return strings.TrimSpace(o) }
		cpu := ""
		if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(line, "model name"); ok {
					cpu = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), ":"))
					break
				}
			}
		}
		timeInfo, _ := run(ctx, "timedatectl", "show")
		sync, _ := run(ctx, "timedatectl", "show-timesync")
		kv := func(s string) map[string]string {
			m := map[string]string{}
			for _, l := range strings.Split(s, "\n") {
				if k, v, ok := strings.Cut(l, "="); ok {
					m[k] = v
				}
			}
			return m
		}
		return map[string]any{
			"manufacturer": get("system-manufacturer"), "product": get("system-product-name"),
			"bios": get("bios-version"), "cpu": cpu, "cores": strings.Count(mustRead("/proc/cpuinfo"), "processor\t"),
			"time": kv(timeInfo), "timesync": kv(sync), "now": time.Now().UTC(),
		}, nil
	})

	s.handle("fail2ban.status.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return fail2banStatus(ctx)
	})

	s.handle("fail2ban.unban", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Jail string `json:"jail"`
			IP   string `json:"ip"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		jails, err := fail2banJails(ctx)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(jails, p.Jail) {
			return nil, Userf("Unknown jail %q.", p.Jail)
		}
		ip, err := netip.ParseAddr(p.IP)
		if err != nil {
			return nil, Userf("Invalid IP address.")
		}
		return nil, s.apply(ctx, "fail2ban-client", "set", p.Jail, "unbanip", ip.String())
	})

	s.handle("updates.history.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		cfg, _ := run(ctx, "apt-config", "dump")
		settings := map[string]string{}
		for _, l := range strings.Split(cfg, "\n") {
			for _, k := range []string{"APT::Periodic::Update-Package-Lists", "APT::Periodic::Unattended-Upgrade", "Unattended-Upgrade::Automatic-Reboot ", "Unattended-Upgrade::Automatic-Reboot-Time", "Unattended-Upgrade::Remove-Unused-Dependencies"} {
				if strings.HasPrefix(l, strings.TrimSpace(k)+" ") {
					settings[strings.TrimSpace(k)] = strings.Trim(strings.TrimSuffix(strings.TrimPrefix(l, strings.TrimSpace(k)+" "), ";"), `"`)
				}
			}
		}
		timer, _ := run(ctx, "systemctl", "show", "--timestamp=unix", "--property=LastTriggerUSec,NextElapseUSecRealtime", "apt-daily-upgrade.timer")
		return map[string]any{"runs": aptHistory(25), "settings": settings, "timer": timer}, nil
	})

	s.handle("samba.status.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return sambaStatus(ctx)
	})

	s.handle("journal.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Unit     string `json:"unit"`
			Priority int    `json:"priority"`
			Query    string `json:"query"`
			Lines    int    `json:"lines"`
			Hours    int    `json:"hours"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		args := []string{"--no-pager", "-o", "short-iso", "-p", strconv.Itoa(min(max(p.Priority, 0), 7))}
		if p.Unit != "" {
			if !slices.Contains(logUnits, p.Unit) {
				return nil, Userf("There are no logs for %q here.", p.Unit)
			}
			args = append(args, "-u", p.Unit+".service")
		}
		hours := min(max(p.Hours, 1), 24*14)
		args = append(args, "--since", "-"+strconv.Itoa(hours)+"h")
		limit := min(max(p.Lines, 50), 2000)
		if p.Query == "" {
			args = append(args, "-n", strconv.Itoa(limit))
		}
		out, _ := run(ctx, "journalctl", args...)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if q := strings.ToLower(strings.TrimSpace(p.Query)); q != "" {
			var keep []string
			for _, l := range lines {
				if strings.Contains(strings.ToLower(l), q) {
					keep = append(keep, l)
				}
			}
			if len(keep) > limit {
				keep = keep[len(keep)-limit:]
			}
			lines = keep
		}
		return map[string]any{"lines": lines}, nil
	})

	// Change log: what was changed through the web interface (helper, file service, logins)
	s.handle("uilog.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		units := []string{"vr-webui-helper", "vr-webui", "vr-webui-files"}
		if s.cfg.DryRun {
			units = []string{"vr-webui-dev-helper", "vr-webui-dev", "vr-webui-dev-files"}
		}
		args := []string{"--no-pager", "-o", "short-iso", "--since", "-30d"}
		for _, u := range units {
			args = append(args, "-u", u+".service")
		}
		out, _ := run(ctx, "journalctl", args...)
		var events []map[string]string
		for _, l := range strings.Split(out, "\n") {
			var kind string
			switch {
			case strings.Contains(l, `msg="action executed"`), strings.Contains(l, "dry run:"):
				kind = "change"
			case strings.Contains(l, `msg="action rejected"`):
				kind = "rejected"
			case strings.Contains(l, "auth: "):
				kind = "auth"
			case strings.Contains(l, "msg=uploaded"), strings.Contains(l, `msg=renamed`), strings.Contains(l, `msg="moved to trash"`), strings.Contains(l, `msg="permanently deleted"`):
				kind = "files"
			case strings.Contains(l, "firewall:"), strings.Contains(l, "system:"), strings.Contains(l, "wake-on-lan"):
				kind = "change"
			default:
				continue
			}
			ts, rest, _ := strings.Cut(l, " ")
			if i := strings.Index(rest, ": "); i >= 0 {
				rest = rest[i+2:]
			}
			events = append(events, map[string]string{"time": ts, "kind": kind, "text": rest})
		}
		slices.Reverse(events)
		if len(events) > 500 {
			events = events[:500]
		}
		return map[string]any{"events": events}, nil
	})
}

func mustRead(p string) string { b, _ := os.ReadFile(p); return string(b) }
