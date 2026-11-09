// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The functions of Portainer that make sense on one router, see docs/portainer-parity.md.

var (
	dockerFlagRe  = regexp.MustCompile(`^[^\s-][^\r\n]{0,500}$`)
	dockerStackRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	dockerSizeRe  = regexp.MustCompile(`^[0-9]{1,12}[bkmgBKMG]?$`)
	dockerCPUsRe  = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,2})?$`)
	dockerMACRe   = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)
	dockerHostRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$`)
)

const (
	stacksDir    = "/var/lib/vanilla-router/stacks"
	templatesDir = "/var/lib/vanilla-router/stack-templates"
	maxCompose   = 1 << 20
)

// dockerAdvanced is what Portainer also offers and what makes a container able to reach into the router.
type dockerAdvanced struct {
	Privileged  bool     `json:"privileged"`
	Devices     []string `json:"devices"`
	Sysctls     []string `json:"sysctls"`
	SecurityOpt []string `json:"security_opt"`
	CapAdd      []string `json:"cap_add"`
	CapDrop     []string `json:"cap_drop"`
	GPUs        string   `json:"gpus"`
}

func (a dockerAdvanced) used() bool {
	return a.Privileged || a.GPUs != "" || len(a.Devices)+len(a.Sysctls)+len(a.SecurityOpt)+len(a.CapAdd)+len(a.CapDrop) > 0
}

// shellSplit cuts a command line into words, with single and double quotes.
func shellSplit(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	quote, started := rune(0), false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, started = r, true
		case unicode.IsSpace(r):
			if started {
				out, started = append(out, cur.String()), false
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, Userf("A quote in the command is not closed.")
	}
	if started {
		out = append(out, cur.String())
	}
	return out, nil
}

func shellJoin(w []string) string {
	q := make([]string, len(w))
	for i, x := range w {
		if x == "" || strings.ContainsAny(x, " \t'\"") {
			q[i] = "'" + x + "'"
			if strings.Contains(x, "'") {
				q[i] = `"` + x + `"`
			}
		} else {
			q[i] = x
		}
	}
	return strings.Join(q, " ")
}

func flagValues(what string, vals []string) ([]string, error) {
	var out []string
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !dockerFlagRe.MatchString(v) {
			return nil, Userf("\"%s\" is not a valid value for %s.", v, what)
		}
		out = append(out, v)
	}
	return out, nil
}

// extraArgs turns the optional settings into flags.
func (p dockerCreate) extraArgs() (flags, command []string, err error) {
	one := func(flag, v string, re *regexp.Regexp, what string) {
		v = strings.TrimSpace(v)
		if v == "" || err != nil {
			return
		}
		if !re.MatchString(v) {
			err = Userf("\"%s\" is not a valid %s.", v, what)
			return
		}
		flags = append(flags, flag, v)
	}
	many := func(flag, what string, vals []string) {
		if err != nil {
			return
		}
		var vs []string
		if vs, err = flagValues(what, vals); err == nil {
			for _, v := range vs {
				flags = append(flags, flag, v)
			}
		}
	}
	if p.Entrypoint != "" {
		var w []string
		if w, err = shellSplit(p.Entrypoint); err != nil {
			return nil, nil, err
		}
		if len(w) > 0 {
			// Docker takes only the program here, the remaining words start the command
			flags = append(flags, "--entrypoint", w[0])
			command = append(command, w[1:]...)
		}
	}
	if p.Command != "" {
		var w []string
		if w, err = shellSplit(p.Command); err != nil {
			return nil, nil, err
		}
		command = append(command, w...)
	}
	one("--workdir", p.WorkDir, dockerFlagRe, "working folder")
	one("--user", p.User, dockerFlagRe, "user")
	one("--hostname", p.Hostname, dockerHostRe, "host name")
	one("--domainname", p.Domain, dockerHostRe, "domain")
	one("--mac-address", p.MAC, dockerMACRe, "MAC address")
	if p.IPv6 != "" {
		if a, e := netip.ParseAddr(strings.TrimSpace(p.IPv6)); e != nil || !a.Is6() {
			return nil, nil, Userf("\"%s\" is not an IPv6 address.", p.IPv6)
		}
		flags = append(flags, "--ip6", strings.TrimSpace(p.IPv6))
	}
	for _, d := range p.DNS {
		if d = strings.TrimSpace(d); d == "" {
			continue
		}
		if _, e := netip.ParseAddr(d); e != nil {
			return nil, nil, Userf("\"%s\" is not a DNS server address.", d)
		}
		flags = append(flags, "--dns", d)
	}
	many("--add-host", "hosts entry", p.Hosts)
	many("--label", "label", p.Labels)
	one("--log-driver", p.LogDriver, dockerFlagRe, "logging driver")
	many("--log-opt", "logging option", p.LogOpts)
	one("--shm-size", p.ShmSize, dockerSizeRe, "size")
	one("--memory", p.Memory, dockerSizeRe, "memory limit")
	one("--memory-reservation", p.MemoryReservation, dockerSizeRe, "memory reservation")
	one("--cpus", p.CPUs, dockerCPUsRe, "CPU limit")
	if p.Init {
		flags = append(flags, "--init")
	}
	if p.AutoRemove {
		flags = append(flags, "--rm")
	}
	if p.dockerAdvanced.used() && !p.Advanced {
		return nil, nil, Userf("These settings can reach into the router. Please turn on \"Advanced\" to use them.")
	}
	if p.Privileged {
		flags = append(flags, "--privileged")
	}
	many("--device", "device", p.Devices)
	many("--sysctl", "sysctl", p.Sysctls)
	many("--security-opt", "security option", p.SecurityOpt)
	many("--cap-add", "capability", p.CapAdd)
	many("--cap-drop", "capability", p.CapDrop)
	one("--gpus", p.GPUs, dockerFlagRe, "GPU setting")
	return flags, command, err
}

// dockerOut runs a read-only Docker command. In a dry run there is no output.
func (s *Server) dockerOut(ctx context.Context, args ...string) (string, error) {
	if s.cfg.DryRun {
		return "", nil
	}
	out, err := run(ctx, "docker", args...)
	if err != nil {
		return "", Userf("Docker: %s", dockerErr(err))
	}
	return out, nil
}

// inDataFolder checks a path for a file that Docker reads or writes on request.
func inDataFolder(p string) bool {
	return validDockerPath(p) && boundUnderRoots(p)
}

// specOf turns a running container into the settings it could be created with.
func (s *Server) specOf(ctx context.Context, name string) (dockerCreate, bool, error) {
	out, err := s.dockerOut(ctx, "inspect", "--", name)
	if err != nil {
		return dockerCreate{}, false, err
	}
	if s.cfg.DryRun {
		return dockerCreate{Name: name, Image: "example/app:1", Network: "ipvlan_lan", IP: "192.168.42.60", Restart: "unless-stopped"}, true, nil
	}
	var raw []struct {
		ID     string `json:"Id"`
		State  struct{ Running bool }
		Config struct {
			Image, WorkingDir, User, Hostname, Domainname string
			Cmd, Entrypoint, Env                          []string
			Labels                                        map[string]string
		}
		HostConfig struct {
			Privileged, AutoRemove             bool
			Init                               *bool
			Binds, CapAdd, CapDrop             []string
			SecurityOpt, ExtraHosts, Dns       []string
			ShmSize, Memory, MemoryReservation int64
			NanoCpus                           int64
			Sysctls                            map[string]string
			RestartPolicy                      struct{ Name string }
			LogConfig                          struct {
				Type   string
				Config map[string]string
			}
			Devices []struct{ PathOnHost, PathInContainer, CgroupPermissions string }
		}
		Mounts []struct {
			Type, Name, Source, Destination string
			RW                              bool
		}
		NetworkSettings struct {
			Networks map[string]struct {
				MacAddress string
				IPAMConfig *struct{ IPv4Address, IPv6Address string }
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil || len(raw) != 1 {
		return dockerCreate{}, false, Userf("The container could not be read.")
	}
	r := raw[0]
	// What only comes from the image is not a setting of this container, or an update could never change it
	var image struct {
		Cmd, Entrypoint, Env []string
		WorkingDir, User     string
	}
	if b, err := s.dockerOut(ctx, "image", "inspect", "--format", "{{json .Config}}", "--", r.Config.Image); err == nil {
		json.Unmarshal([]byte(b), &image)
	}
	if slices.Equal(r.Config.Cmd, image.Cmd) {
		r.Config.Cmd = nil
	}
	if slices.Equal(r.Config.Entrypoint, image.Entrypoint) {
		r.Config.Entrypoint = nil
	}
	if r.Config.WorkingDir == image.WorkingDir {
		r.Config.WorkingDir = ""
	}
	if r.Config.User == image.User {
		r.Config.User = ""
	}
	r.Config.Env = slices.DeleteFunc(r.Config.Env, func(e string) bool { return slices.Contains(image.Env, e) })
	p := dockerCreate{Name: name, Image: r.Config.Image, Restart: r.HostConfig.RestartPolicy.Name, Env: r.Config.Env,
		WorkDir: r.Config.WorkingDir, User: r.Config.User, Domain: r.Config.Domainname, Command: shellJoin(r.Config.Cmd), Entrypoint: shellJoin(r.Config.Entrypoint),
		AutoRemove: r.HostConfig.AutoRemove, Hosts: r.HostConfig.ExtraHosts, DNS: r.HostConfig.Dns}
	if p.Restart == "" {
		p.Restart = "no"
	}
	if !strings.HasPrefix(r.ID, r.Config.Hostname) {
		p.Hostname = r.Config.Hostname
	}
	for k, v := range r.Config.Labels {
		if !strings.HasPrefix(k, "com.docker.compose.") && !strings.HasPrefix(k, "org.opencontainers.") {
			p.Labels = append(p.Labels, k+"="+v)
		}
	}
	slices.Sort(p.Labels)
	p.Init = r.HostConfig.Init != nil && *r.HostConfig.Init
	p.Privileged, p.CapAdd, p.CapDrop = r.HostConfig.Privileged, r.HostConfig.CapAdd, r.HostConfig.CapDrop
	p.SecurityOpt = r.HostConfig.SecurityOpt
	for k, v := range r.HostConfig.Sysctls {
		p.Sysctls = append(p.Sysctls, k+"="+v)
	}
	for _, d := range r.HostConfig.Devices {
		p.Devices = append(p.Devices, d.PathOnHost+":"+d.PathInContainer)
	}
	if v := r.HostConfig.ShmSize; v != 0 && v != 67108864 {
		p.ShmSize = strconv.FormatInt(v, 10)
	}
	if v := r.HostConfig.Memory; v != 0 {
		p.Memory = strconv.FormatInt(v, 10)
	}
	if v := r.HostConfig.MemoryReservation; v != 0 {
		p.MemoryReservation = strconv.FormatInt(v, 10)
	}
	if v := r.HostConfig.NanoCpus; v != 0 {
		p.CPUs = strconv.FormatFloat(float64(v)/1e9, 'f', 2, 64)
	}
	if t := r.HostConfig.LogConfig.Type; t != "" && t != "json-file" {
		p.LogDriver = t
	}
	for k, v := range r.HostConfig.LogConfig.Config {
		p.LogOpts = append(p.LogOpts, k+"="+v)
	}
	for _, m := range r.Mounts {
		src := m.Source
		if m.Type == "volume" {
			src = m.Name
		}
		l := src + ":" + m.Destination
		if !m.RW {
			l += ":ro"
		}
		p.Mounts = append(p.Mounts, l)
		if m.Type != "volume" && !boundUnderRoots(src) {
			p.Advanced = true
		}
	}
	for n, c := range r.NetworkSettings.Networks {
		p.Network = n
		if c.IPAMConfig != nil {
			p.IP, p.IPv6 = c.IPAMConfig.IPv4Address, c.IPAMConfig.IPv6Address
		}
		p.MAC = ""
		break
	}
	if p.dockerAdvanced.used() {
		p.Advanced = true
	}
	return p, r.State.Running, nil
}

func (s *Server) registerDockerMore(startJob func(name string, work func(context.Context) string) error, createCheck func(context.Context, dockerCreate, bool) ([]string, error)) dockerOps {
	name := func(raw json.RawMessage) (string, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := decode(raw, &p); err != nil {
			return "", err
		}
		if !containerNameRe.MatchString(p.Name) {
			return "", Userf("Invalid name.")
		}
		return p.Name, nil
	}
	read := func(action string, build func(string) []string) {
		s.handle(action, func(ctx context.Context, raw json.RawMessage) (any, error) {
			n, err := name(raw)
			if err != nil {
				return nil, err
			}
			out, err := s.dockerOut(ctx, build(n)...)
			if err != nil {
				return nil, err
			}
			return map[string]string{"text": out}, nil
		})
	}
	read("docker.inspect.get", func(n string) []string { return []string{"inspect", "--", n} })
	read("docker.top.get", func(n string) []string { return []string{"top", n} })
	read("docker.stats.get", func(n string) []string { return []string{"stats", "--no-stream", "--format", "{{json .}}", n} })
	s.handle("docker.image.inspect.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerImageRe.MatchString(p.Name) {
			return nil, Userf("Invalid image.")
		}
		out, err := s.dockerOut(ctx, "image", "inspect", "--", p.Name)
		if err != nil {
			return nil, err
		}
		return map[string]string{"text": out}, nil
	})

	s.handle("docker.rename", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
			New  string `json:"new"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) || !containerNameRe.MatchString(p.New) {
			return nil, Userf("Please use letters, digits, dots, underscores and hyphens.")
		}
		if err := s.apply(ctx, "docker", "rename", p.Name, p.New); err != nil {
			return nil, Userf("Renaming failed: %s", dockerErr(err))
		}
		return nil, nil
	})

	s.handle("docker.commit", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) || !dockerImageRe.MatchString(p.Image) {
			return nil, Userf("Please enter a valid image name.")
		}
		if err := s.apply(ctx, "docker", "commit", p.Name, p.Image); err != nil {
			return nil, Userf("%s", dockerErr(err))
		}
		return nil, nil
	})

	s.handle("docker.network.connect", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Network   string `json:"network"`
			Container string `json:"container"`
			IP        string `json:"ip"`
			Leave     bool   `json:"leave"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Network) || !containerNameRe.MatchString(p.Container) {
			return nil, Userf("Invalid name.")
		}
		args := []string{"network", "disconnect", p.Network, p.Container}
		if !p.Leave {
			args = []string{"network", "connect"}
			if p.IP != "" {
				if _, err := netip.ParseAddr(p.IP); err != nil {
					return nil, Userf("\"%s\" is not an address.", p.IP)
				}
				args = append(args, "--ip", p.IP)
			}
			args = append(args, p.Network, p.Container)
		}
		if err := s.apply(ctx, "docker", args...); err != nil {
			return nil, Userf("%s", dockerErr(err))
		}
		return nil, nil
	})

	s.handle("docker.spec.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		n, err := name(raw)
		if err != nil {
			return nil, err
		}
		p, _, err := s.specOf(ctx, n)
		return p, err
	})

	// Replaces a container. Its old self stays until the new one is running, and comes back if that fails.
	replace := func(ctx context.Context, old string, pull bool, spec dockerCreate) error {
		_, wasRunning, err := s.specOf(ctx, old)
		if err != nil {
			return err
		}
		args, err := createCheck(ctx, spec, spec.Name != old)
		if err != nil {
			return err
		}
		spare, image := old+"-vrold", spec.Image
		return startJob(spec.Name, func(bg context.Context) string {
			if pull {
				if err := s.apply(bg, "docker", "pull", "--", image); err != nil {
					return "The image could not be downloaded: " + dockerErr(err)
				}
			}
			if err := s.apply(bg, "docker", "rename", old, spare); err != nil {
				return "The container could not be replaced: " + dockerErr(err)
			}
			s.apply(bg, "docker", "stop", "--", spare)
			if err := s.apply(bg, "docker", args...); err != nil {
				msg := dockerErr(err)
				// run may have created the container before it failed
				s.apply(bg, "docker", "rm", "-f", "--", spec.Name)
				s.apply(bg, "docker", "rename", spare, old)
				if wasRunning {
					s.apply(bg, "docker", "start", "--", old)
				}
				return "The old container was restored: " + msg
			}
			s.apply(bg, "docker", "rm", "-f", "--", spare)
			return ""
		})
	}
	s.handle("docker.replace", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Old  string       `json:"old"`
			Pull bool         `json:"pull"`
			Spec dockerCreate `json:"spec"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Old) {
			return nil, Userf("Invalid name.")
		}
		return nil, replace(ctx, p.Old, p.Pull, p.Spec)
	})

	// Images
	s.handle("docker.image.pull", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Image string `json:"image"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerImageRe.MatchString(p.Image) {
			return nil, Userf("\"%s\" is not a valid image name.", p.Image)
		}
		return nil, startJob(p.Image, func(bg context.Context) string {
			if err := s.apply(bg, "docker", "pull", "--", p.Image); err != nil {
				return "The image could not be downloaded: " + dockerErr(err)
			}
			return ""
		})
	})
	s.handle("docker.image.tag", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct{ Image, Tag string }
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerImageRe.MatchString(p.Image) || !dockerImageRe.MatchString(p.Tag) {
			return nil, Userf("Please enter a valid image name.")
		}
		if err := s.apply(ctx, "docker", "image", "tag", p.Image, p.Tag); err != nil {
			return nil, Userf("%s", dockerErr(err))
		}
		return nil, nil
	})
	s.handle("docker.image.push", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct{ Image string }
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerImageRe.MatchString(p.Image) {
			return nil, Userf("Please enter a valid image name.")
		}
		return nil, startJob(p.Image, func(bg context.Context) string {
			if err := s.apply(bg, "docker", "push", p.Image); err != nil {
				return "The image could not be uploaded: " + dockerErr(err)
			}
			return ""
		})
	})
	s.handle("docker.image.build", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Tag        string `json:"tag"`
			Dockerfile string `json:"dockerfile"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerImageRe.MatchString(p.Tag) || strings.TrimSpace(p.Dockerfile) == "" || len(p.Dockerfile) > maxCompose {
			return nil, Userf("Please enter a name and a Dockerfile.")
		}
		return nil, startJob(p.Tag, func(bg context.Context) string {
			if s.cfg.DryRun {
				return ""
			}
			dir, err := os.MkdirTemp("", "vr-build-")
			if err != nil {
				return "The build could not be prepared."
			}
			defer os.RemoveAll(dir)
			if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(p.Dockerfile), 0o600); err != nil {
				return "The build could not be prepared."
			}
			if err := s.apply(bg, "docker", "build", "-t", p.Tag, dir); err != nil {
				return "The build failed: " + dockerErr(err)
			}
			return ""
		})
	})
	s.handle("docker.image.export", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct{ Image, File string }
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerImageRe.MatchString(p.Image) || !inDataFolder(p.File) {
			return nil, Userf("The file must be under %s.", strings.Join(dockerBindRoots, ", "))
		}
		return nil, startJob(p.Image, func(bg context.Context) string {
			if err := s.apply(bg, "docker", "image", "save", "-o", p.File, p.Image); err != nil {
				return "Export failed: " + dockerErr(err)
			}
			return ""
		})
	})
	s.handle("docker.image.import", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct{ File string }
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !inDataFolder(p.File) {
			return nil, Userf("The file must be under %s.", strings.Join(dockerBindRoots, ", "))
		}
		return nil, startJob(path.Base(p.File), func(bg context.Context) string {
			if err := s.apply(bg, "docker", "image", "load", "-i", p.File); err != nil {
				return "Import failed: " + dockerErr(err)
			}
			return ""
		})
	})
	prune := func(action string, args ...string) {
		s.handle(action, func(ctx context.Context, _ json.RawMessage) (any, error) {
			if err := s.apply(ctx, "docker", args...); err != nil {
				return nil, Userf("%s", dockerErr(err))
			}
			return nil, nil
		})
	}
	prune("docker.image.prune", "image", "prune", "-a", "-f")
	prune("docker.volume.prune", "volume", "prune", "-a", "-f")

	// Browsing a volume, read only. os.Root keeps the walk inside the volume, even past symlinks.
	s.handle("docker.volume.browse.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) {
			return nil, Userf("Invalid name.")
		}
		rel := strings.TrimPrefix(path.Clean("/"+p.Path), "/")
		if rel == "" {
			rel = "."
		}
		entries := []map[string]any{}
		if s.cfg.DryRun {
			return map[string]any{"path": p.Path, "entries": entries}, nil
		}
		root, err := os.OpenRoot("/var/lib/docker/volumes/" + p.Name + "/_data")
		if err != nil {
			return nil, Userf("This volume cannot be browsed.")
		}
		defer root.Close()
		dir, err := root.Open(rel)
		if err != nil {
			return nil, Userf("This folder cannot be opened.")
		}
		defer dir.Close()
		list, err := dir.ReadDir(2000)
		if err != nil {
			return nil, Userf("This folder cannot be opened.")
		}
		for _, e := range list {
			var size int64
			var mod time.Time
			if i, err := e.Info(); err == nil {
				size, mod = i.Size(), i.ModTime()
			}
			entries = append(entries, map[string]any{"name": e.Name(), "dir": e.IsDir(), "size": size, "modified": mod, "link": e.Type()&fs.ModeSymlink != 0})
		}
		return map[string]any{"path": rel, "entries": entries}, nil
	})

	s.handle("docker.volume.create.full", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name   string   `json:"name"`
			Driver string   `json:"driver"`
			Opts   []string `json:"opts"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) {
			return nil, Userf("Please enter a name of letters, digits, dots, underscores and hyphens.")
		}
		args := []string{"volume", "create"}
		if p.Driver != "" {
			if !containerNameRe.MatchString(p.Driver) {
				return nil, Userf("Invalid driver.")
			}
			args = append(args, "--driver", p.Driver)
		}
		opts, err := flagValues("option", p.Opts)
		if err != nil {
			return nil, err
		}
		for _, o := range opts {
			args = append(args, "--opt", o)
		}
		if err := s.apply(ctx, "docker", append(args, "--", p.Name)...); err != nil {
			return nil, Userf("The volume could not be created: %s", dockerErr(err))
		}
		return nil, nil
	})

	// Host, events, registries
	s.handle("docker.host.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		info, err := s.dockerOut(ctx, "info", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		df, err := s.dockerOut(ctx, "system", "df", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		var i any
		json.Unmarshal([]byte(info), &i)
		rows := []any{}
		for _, l := range strings.Split(strings.TrimSpace(df), "\n") {
			var r any
			if json.Unmarshal([]byte(l), &r) == nil {
				rows = append(rows, r)
			}
		}
		return map[string]any{"info": i, "disk": rows}, nil
	})
	s.handle("docker.events.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		out, err := s.dockerOut(ctx, "events", "--since", "24h", "--until", "0s", "--format", "{{json .}}")
		if err != nil {
			return nil, err
		}
		rows := []any{}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			var r any
			if json.Unmarshal([]byte(l), &r) == nil {
				rows = append(rows, r)
			}
		}
		slices.Reverse(rows)
		if len(rows) > 500 {
			rows = rows[:500]
		}
		return rows, nil
	})
	s.handle("docker.registries.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		hosts := []string{}
		b, err := os.ReadFile("/root/.docker/config.json")
		if err == nil && !s.cfg.DryRun {
			var c struct {
				Auths map[string]json.RawMessage `json:"auths"`
			}
			if json.Unmarshal(b, &c) == nil {
				for h := range c.Auths {
					hosts = append(hosts, h)
				}
			}
		}
		slices.Sort(hosts)
		return hosts, nil
	})
	s.handle("docker.registry.login", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Server   string `json:"server"`
			User     string `json:"user"`
			Password string `json:"password"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerHostRe.MatchString(p.Server) && p.Server != "" || !dockerFlagRe.MatchString(p.User) || p.Password == "" {
			return nil, Userf("Please enter a user name and a password.")
		}
		if s.cfg.DryRun {
			return nil, nil
		}
		args := []string{"login", "--username", p.User, "--password-stdin"}
		if p.Server != "" {
			args = append(args, p.Server)
		}
		if err := runInput(ctx, p.Password, "docker", args...); err != nil {
			return nil, Userf("Login failed: %s", dockerErr(err))
		}
		return nil, nil
	})
	s.handle("docker.registry.logout", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct{ Server string }
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerHostRe.MatchString(p.Server) {
			return nil, Userf("Invalid server.")
		}
		if err := s.apply(ctx, "docker", "logout", p.Server); err != nil {
			return nil, Userf("%s", dockerErr(err))
		}
		return nil, nil
	})

	// Stacks are Compose projects. The files stay readable for root only: they may hold passwords.
	stackDir, compose, own := s.stackDir, s.composeArgs, s.stackOwned
	deploy := func(name string, pull bool, before func(context.Context) string) error {
		return startJob(name, func(bg context.Context) string {
			if before != nil {
				if msg := before(bg); msg != "" {
					return msg
				}
			}
			up := []string{"up", "-d", "--remove-orphans"}
			if pull {
				up = append(up, "--pull", "always")
			}
			if err := s.apply(bg, "docker", compose(name, up...)...); err != nil {
				return "The stack could not be started: " + dockerErr(err)
			}
			return ""
		})
	}
	stackName := func(raw json.RawMessage) (string, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := decode(raw, &p); err != nil {
			return "", err
		}
		if !dockerStackRe.MatchString(p.Name) {
			return "", Userf("A stack name may contain lowercase letters, digits, underscores and hyphens.")
		}
		return p.Name, nil
	}
	s.handle("docker.stacks.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		seen := map[string]map[string]any{}
		ents, _ := os.ReadDir(s.Path(stacksDir))
		for _, e := range ents {
			if e.IsDir() {
				seen[e.Name()] = map[string]any{"name": e.Name(), "status": "not deployed", "editable": true, "git": s.readGit(e.Name()) != nil}
			}
		}
		out, err := s.dockerOut(ctx, "compose", "ls", "--all", "--format", "json")
		if err != nil {
			return nil, err
		}
		var ls []struct{ Name, Status string }
		if out != "" {
			json.Unmarshal([]byte(out), &ls)
		}
		for _, l := range ls {
			if m := seen[l.Name]; m != nil {
				m["status"] = l.Status
			} else {
				seen[l.Name] = map[string]any{"name": l.Name, "status": l.Status, "editable": false}
			}
		}
		res := []map[string]any{}
		for _, m := range seen {
			res = append(res, m)
		}
		slices.SortFunc(res, func(a, b map[string]any) int { return strings.Compare(a["name"].(string), b["name"].(string)) })
		return res, nil
	})
	s.handle("docker.stack.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		n, err := stackName(raw)
		if err != nil {
			return nil, err
		}
		if m := s.readGit(n); m != nil {
			env, _ := os.ReadFile(filepath.Join(stackDir(n), ".env"))
			return map[string]any{"name": n, "env": string(env), "git": map[string]any{"url": m.URL, "ref": m.Ref, "path": m.Path, "user": m.User, "interval": m.Interval, "has_token": m.Token != ""}}, nil
		}
		b, err := os.ReadFile(filepath.Join(stackDir(n), "compose.yaml"))
		if err != nil {
			return nil, Userf("This stack was not created here and cannot be edited.")
		}
		env, _ := os.ReadFile(filepath.Join(stackDir(n), ".env"))
		return map[string]string{"name": n, "compose": string(b), "env": string(env)}, nil
	})
	s.handle("docker.stack.deploy", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name    string `json:"name"`
			Compose string `json:"compose"`
			Env     string `json:"env"`
			Pull    bool   `json:"pull"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerStackRe.MatchString(p.Name) {
			return nil, Userf("A stack name may contain lowercase letters, digits, underscores and hyphens.")
		}
		if strings.TrimSpace(p.Compose) == "" || len(p.Compose)+len(p.Env) > maxCompose {
			return nil, Userf("Please enter the Compose file.")
		}
		dir := stackDir(p.Name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		os.Remove(filepath.Join(dir, gitMetaName))
		os.RemoveAll(filepath.Join(dir, "repo"))
		for f, c := range map[string]string{"compose.yaml": p.Compose, ".env": p.Env} {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(c), 0o600); err != nil {
				return nil, err
			}
		}
		if _, err := s.checkCompose(ctx, compose(p.Name, "config", "-q")); err != nil {
			return nil, err
		}
		return nil, deploy(p.Name, p.Pull, nil)
	})
	stackAct := func(action, verb string) {
		s.handle(action, func(ctx context.Context, raw json.RawMessage) (any, error) {
			n, err := stackName(raw)
			if err != nil {
				return nil, err
			}
			args := compose(n, verb)
			if !own(n) {
				args = []string{"compose", "-p", n, verb}
			}
			if err := s.apply(ctx, "docker", args...); err != nil {
				return nil, Userf("%s", dockerErr(err))
			}
			return nil, nil
		})
	}
	stackAct("docker.stack.start", "start")
	stackAct("docker.stack.stop", "stop")
	s.handle("docker.stack.remove", func(ctx context.Context, raw json.RawMessage) (any, error) {
		n, err := stackName(raw)
		if err != nil {
			return nil, err
		}
		args := compose(n, "down")
		if !own(n) {
			args = []string{"compose", "-p", n, "down"}
		}
		if err := s.apply(ctx, "docker", args...); err != nil {
			return nil, Userf("%s", dockerErr(err))
		}
		return nil, os.RemoveAll(stackDir(n))
	})
	s.handle("docker.stack.logs.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		n, err := stackName(raw)
		if err != nil {
			return nil, err
		}
		out, err := s.dockerOut(ctx, "compose", "-p", n, "logs", "--no-color", "--tail", "300")
		if err != nil {
			return nil, err
		}
		return map[string]string{"text": out}, nil
	})

	// Custom templates: Compose files kept for later
	tplPath := func(n string) string { return filepath.Join(s.Path(templatesDir), n+".yaml") }
	s.handle("docker.templates.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		out := []map[string]string{}
		ents, _ := os.ReadDir(s.Path(templatesDir))
		for _, e := range ents {
			n := strings.TrimSuffix(e.Name(), ".yaml")
			if b, err := os.ReadFile(tplPath(n)); err == nil {
				out = append(out, map[string]string{"name": n, "compose": string(b)})
			}
		}
		return out, nil
	})
	s.handle("docker.template.save", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name    string `json:"name"`
			Compose string `json:"compose"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerStackRe.MatchString(p.Name) || strings.TrimSpace(p.Compose) == "" || len(p.Compose) > maxCompose {
			return nil, Userf("Please enter a name of lowercase letters, digits, underscores and hyphens, and the Compose file.")
		}
		if err := os.MkdirAll(s.Path(templatesDir), 0o700); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(tplPath(p.Name), []byte(p.Compose), 0o600)
	})
	s.handle("docker.template.remove", func(ctx context.Context, raw json.RawMessage) (any, error) {
		n, err := stackName(raw)
		if err != nil {
			return nil, err
		}
		return nil, os.Remove(tplPath(n))
	})

	return dockerOps{startJob: startJob, replace: replace, deploy: deploy}
}

// runInput runs a program and hands it a secret on stdin, never on the command line.
func runInput(ctx context.Context, input, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C.UTF-8", "HOME=/root"}
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exit status 1: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// checkCompose asks Compose whether a file is valid before anything is started.
func (s *Server) checkCompose(ctx context.Context, args []string) (string, error) {
	if s.cfg.DryRun {
		return "", nil
	}
	out, err := run(ctx, "docker", args...)
	if err != nil {
		return "", Userf("The Compose file has an error: %s", dockerErr(err))
	}
	return out, nil
}
