// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"vanilla-router/internal/config"
	"vanilla-router/internal/gen"
)

// Managing containers, images, volumes and networks. Starting a container is close to being root, so the
// web interface offers no privileged mode, no extra capabilities and no devices, and bind mounts are
// limited to folders meant for data. Docker runs without its own firewall rules (D-20): a container gets
// its own address on a network that hangs off one of the router's interfaces.

var (
	dockerImageRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]{0,254}$`)
	dockerEnvKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	dockerPathRe   = regexp.MustCompile(`^/[A-Za-z0-9._/ -]{0,254}$`)
	dockerImageIDs = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)
)

var dockerRestart = []string{"no", "always", "unless-stopped", "on-failure"}

// dockerBindRoots are the only places a bind mount may come from.
var dockerBindRoots = []string{"/docker/", "/srv/", "/media/", "/mnt/"}

const dockerJobTime = 20 * time.Minute

// DockerNetwork is one Docker network.
type DockerNetwork struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Parent   string `json:"parent"` // the router's interface it hangs off
	Subnet   string `json:"subnet"`
	Gateway  string `json:"gateway"`
	Internal bool   `json:"internal"`
	InUse    int    `json:"in_use"`
	Builtin  bool   `json:"builtin"`
}

// DockerParent is an interface a network can hang off.
type DockerParent struct {
	Name    string `json:"name"`
	Role    string `json:"role"` // lan, wan, segment, free
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
}

type dockerMount struct {
	volume   bool
	source   string
	target   string
	readOnly bool
}

var dockerExitRe = regexp.MustCompile(`exit status \d+:? ?`)

// dockerErr finds Docker's own explanation in an error. Its command line stays out, that may hold settings.
func dockerErr(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if loc := dockerExitRe.FindAllStringIndex(l, -1); loc != nil {
			l = strings.TrimSpace(l[loc[len(loc)-1][1]:])
		}
		if l != "" && !strings.HasPrefix(l, "Run '") {
			return strings.TrimPrefix(strings.TrimPrefix(l, "docker: "), "Error response from daemon: ")
		}
	}
	return "unknown error"
}

func validDockerPath(p string) bool {
	return dockerPathRe.MatchString(p) && filepath.Clean(p) == p && p != "/"
}

// parseMount reads "source:target" or "source:target:ro". A source starting with / is a folder on the router,
// anything else the name of a volume.
func parseMount(line string) (dockerMount, error) {
	f := strings.Split(strings.TrimSpace(line), ":")
	if len(f) == 3 && f[2] == "ro" {
		return finishMount(f[0], f[1], true)
	}
	if len(f) != 2 {
		return dockerMount{}, Userf("\"%s\" is not a storage line. Write source:target, for example media:/data or /srv/media:/data:ro.", line)
	}
	return finishMount(f[0], f[1], false)
}

func finishMount(source, target string, ro bool) (dockerMount, error) {
	if !validDockerPath(target) {
		return dockerMount{}, Userf("The path %q inside the container is not valid.", target)
	}
	m := dockerMount{source: source, target: target, readOnly: ro}
	if strings.HasPrefix(source, "/") {
		if !validDockerPath(source) {
			return dockerMount{}, Userf("The folder %q is not valid.", source)
		}
		return m, nil
	}
	if !containerNameRe.MatchString(source) {
		return dockerMount{}, Userf("%q is not a valid volume name.", source)
	}
	m.volume = true
	return m, nil
}

func boundUnderRoots(p string) bool {
	return slices.ContainsFunc(dockerBindRoots, func(r string) bool { return strings.HasPrefix(p, r) })
}

// prepareBind creates the folder and checks where it really is, a symlink must not lead out of the allowed places.
func prepareBind(p string) error {
	if err := os.MkdirAll(p, 0o755); err != nil {
		return Userf("The folder %s could not be created.", p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil || !boundUnderRoots(real) {
		return Userf("The folder %s leads outside the allowed places.", p)
	}
	return nil
}

type dockerCreate struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Network string   `json:"network"`
	IP      string   `json:"ip"`
	Restart string   `json:"restart"`
	Env     []string `json:"env"`
	Mounts  []string `json:"mounts"`

	Command           string   `json:"command"`
	Entrypoint        string   `json:"entrypoint"`
	WorkDir           string   `json:"workdir"`
	User              string   `json:"user"`
	Hostname          string   `json:"hostname"`
	Domain            string   `json:"domain"`
	MAC               string   `json:"mac"`
	IPv6              string   `json:"ipv6"`
	DNS               []string `json:"dns"`
	Hosts             []string `json:"hosts"`
	Labels            []string `json:"labels"`
	LogDriver         string   `json:"log_driver"`
	LogOpts           []string `json:"log_opts"`
	ShmSize           string   `json:"shm_size"`
	Memory            string   `json:"memory"`
	MemoryReservation string   `json:"memory_reservation"`
	CPUs              string   `json:"cpus"`
	Init              bool     `json:"init"`
	AutoRemove        bool     `json:"auto_remove"`
	AlwaysPull        bool     `json:"always_pull"`
	Advanced          bool     `json:"advanced"`
	dockerAdvanced
}

// runArgs builds the docker run command. No shell is involved, and the image comes last and cannot start with a dash.
func (p dockerCreate) runArgs() ([]string, []dockerMount, error) {
	if !containerNameRe.MatchString(p.Name) {
		return nil, nil, Userf("The name may contain letters, digits, dots, underscores and hyphens.")
	}
	if !dockerImageRe.MatchString(p.Image) {
		return nil, nil, Userf("\"%s\" is not a valid image name.", p.Image)
	}
	if !slices.Contains(dockerRestart, p.Restart) {
		return nil, nil, Userf("Unknown restart setting.")
	}
	if !containerNameRe.MatchString(p.Network) {
		return nil, nil, Userf("Please choose a network.")
	}
	args := []string{"run", "-d", "--name", p.Name, "--restart", p.Restart, "--network", p.Network}
	if p.IP != "" {
		args = append(args, "--ip", p.IP)
	}
	var mounts []dockerMount
	for _, l := range p.Mounts {
		if strings.TrimSpace(l) == "" {
			continue
		}
		m, err := parseMount(l)
		if err != nil {
			return nil, nil, err
		}
		if !m.volume && !p.Advanced && !boundUnderRoots(m.source) {
			return nil, nil, Userf("Folders of the router can only come from %s, unless \"Advanced\" is on.", strings.Join(dockerBindRoots, ", "))
		}
		kind := "bind"
		if m.volume {
			kind = "volume"
		}
		spec := "type=" + kind + ",src=" + m.source + ",dst=" + m.target
		if m.readOnly {
			spec += ",readonly"
		}
		args = append(args, "--mount", spec)
		mounts = append(mounts, m)
	}
	for _, l := range p.Env {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		k, _, ok := strings.Cut(l, "=")
		if !ok || !dockerEnvKeyRe.MatchString(k) || strings.ContainsAny(l, "\r\n\x00") {
			return nil, nil, Userf("\"%s\" is not a setting of the form NAME=value.", k)
		}
		args = append(args, "-e", l)
	}
	extra, command, err := p.extraArgs()
	if err != nil {
		return nil, nil, err
	}
	args = append(append(args, extra...), p.Image)
	return append(args, command...), mounts, nil
}

// checkContainerIP: on a network with its own addresses a container needs a fixed one, because Docker knows
// nothing about the devices and the DHCP range next to it.
func checkContainerIP(c *config.Config, n DockerNetwork, ip string) error {
	own := n.Driver == "ipvlan" || n.Driver == "macvlan"
	if !own {
		if ip != "" {
			return Userf("A container on the network %s cannot have its own address.", n.Name)
		}
		return nil
	}
	if ip == "" {
		return Userf("Please enter a free address in %s for this container.", n.Subnet)
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return Userf("\"%s\" is not an IPv4 address.", ip)
	}
	sub, err := netip.ParsePrefix(n.Subnet)
	if err != nil || !sub.Contains(addr) {
		return Userf("The address %s is not in the network %s.", ip, n.Subnet)
	}
	if n.Gateway == ip {
		return Userf("The address %s belongs to the gateway.", ip)
	}
	lan, err := netip.ParsePrefix(c.LAN.IPv4)
	if err != nil || !lan.Masked().Contains(addr) {
		return nil
	}
	if addr == lan.Addr() {
		return Userf("The address %s belongs to the router.", ip)
	}
	first, e1 := netip.ParseAddr(c.DHCP.PoolStart)
	last, e2 := netip.ParseAddr(c.DHCP.PoolEnd)
	if e1 == nil && e2 == nil && addr.Compare(first) >= 0 && addr.Compare(last) <= 0 {
		return Userf("The address %s is in the range the router hands out to devices (%s to %s).", ip, first, last)
	}
	for _, r := range c.DHCP.Reservations {
		if r.IP == ip {
			return Userf("The address %s is reserved for %s.", ip, r.Name)
		}
	}
	return nil
}

func (s *Server) dockerNetworks(ctx context.Context) ([]DockerNetwork, error) {
	if s.cfg.DryRun {
		return []DockerNetwork{
			{Name: "ipvlan_lan", Driver: "ipvlan", Parent: gen.LANIf, Subnet: "192.168.42.0/24", Gateway: "192.168.42.1"},
			{Name: "host", Driver: "host", Builtin: true}, {Name: "none", Driver: "null", Builtin: true},
		}, nil
	}
	ids, err := run(ctx, "docker", "network", "ls", "-q", "--no-trunc")
	if err != nil {
		return nil, Userf("Docker is not responding.")
	}
	out, err := run(ctx, "docker", append([]string{"network", "inspect"}, strings.Fields(ids)...)...)
	if err != nil {
		return nil, Userf("Docker is not responding.")
	}
	var raw []struct {
		Name, Driver string
		Internal     bool
		IPAM         struct {
			Config []struct{ Subnet, Gateway string }
		}
		Options    map[string]string
		Containers map[string]json.RawMessage
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	res := []DockerNetwork{}
	for _, r := range raw {
		n := DockerNetwork{Name: r.Name, Driver: r.Driver, Parent: r.Options["parent"], Internal: r.Internal, InUse: len(r.Containers),
			Builtin: r.Name == "host" || r.Name == "none" || r.Name == "bridge"}
		if len(r.IPAM.Config) > 0 {
			n.Subnet, n.Gateway = r.IPAM.Config[0].Subnet, r.IPAM.Config[0].Gateway
		}
		res = append(res, n)
	}
	slices.SortFunc(res, func(a, b DockerNetwork) int { return strings.Compare(a.Name, b.Name) })
	return res, nil
}

func ifaceSubnet(name string) string {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if p, err := netip.ParsePrefix(n.String()); err == nil && p.Addr().Is4() {
				return p.Masked().String()
			}
		}
	}
	return ""
}

func defaultGateway(ctx context.Context, dev string) string {
	out, err := run(ctx, "ip", "-j", "-4", "route", "show", "default", "dev", dev)
	if err != nil {
		return ""
	}
	var r []struct {
		Gateway string `json:"gateway"`
	}
	if json.Unmarshal([]byte(out), &r) != nil || len(r) == 0 {
		return ""
	}
	return r[0].Gateway
}

// dockerParents lists what a network can hang off: the home network, the internet port, segments and
// free ports. A port taken out of use keeps a temporary name until the next start, it is not offered then.
func (s *Server) dockerParents(ctx context.Context, c *config.Config) []DockerParent {
	lan := DockerParent{Name: gen.LANIf, Role: roleLAN}
	if p, err := netip.ParsePrefix(c.LAN.IPv4); err == nil {
		lan.Subnet, lan.Gateway = p.Masked().String(), p.Addr().String()
	}
	if s.cfg.DryRun {
		return []DockerParent{lan, {Name: gen.WANIf, Role: roleWAN, Subnet: "203.0.113.0/24", Gateway: "203.0.113.1"}, {Name: "enp9s0", Role: roleFree}}
	}
	out := []DockerParent{lan, {Name: gen.WANIf, Role: roleWAN, Subnet: ifaceSubnet(gen.WANIf), Gateway: defaultGateway(ctx, gen.WANIf)}}
	ents, _ := os.ReadDir(sysNet)
	for _, e := range ents {
		n := e.Name()
		if _, err := os.Stat("/proc/net/vlan/" + n); err == nil && !strings.HasPrefix(n, gen.WANIf+".") {
			out = append(out, DockerParent{Name: n, Role: roleSegment, Subnet: ifaceSubnet(n)})
			continue
		}
		if _, err := os.Stat(filepath.Join(sysNet, n, "bridge")); err == nil && n != gen.LANIf {
			out = append(out, DockerParent{Name: n, Role: roleSegment, Subnet: ifaceSubnet(n)})
		}
	}
	used := map[string]bool{strings.ToLower(c.WAN.MatchMAC): true}
	for _, m := range c.LAN.Members {
		used[strings.ToLower(m)] = true
	}
	for _, p := range physicalPorts() {
		if !used[p.info.MAC] && !strings.HasPrefix(p.name, "vroff-") {
			out = append(out, DockerParent{Name: p.name, Role: roleFree, Subnet: ifaceSubnet(p.name)})
		}
	}
	return out
}

func (s *Server) registerDocker() {
	var jobMu sync.Mutex
	job := map[string]string{"state": "idle"}
	setJob := func(state, name, msg string) {
		jobMu.Lock()
		defer jobMu.Unlock()
		job = map[string]string{"state": state, "name": name, "error": msg}
	}
	loadCfg := func() (*config.Config, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.loadConfig()
	}

	s.handle("docker.job.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		jobMu.Lock()
		defer jobMu.Unlock()
		return job, nil
	})

	// createCheck validates a new container, prepares its folders and returns the command that starts it.
	createCheck := func(ctx context.Context, p dockerCreate, newName bool) ([]string, error) {
		p.Name, p.Image, p.IP = strings.TrimSpace(p.Name), strings.TrimSpace(p.Image), strings.TrimSpace(p.IP)
		args, mounts, err := p.runArgs()
		if err != nil {
			return nil, err
		}
		nets, err := s.dockerNetworks(ctx)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(nets, func(n DockerNetwork) bool { return n.Name == p.Network })
		if i < 0 {
			return nil, Userf("The network %s does not exist.", p.Network)
		}
		c, err := loadCfg()
		if err != nil {
			return nil, err
		}
		if err := checkContainerIP(c, nets[i], p.IP); err != nil {
			return nil, err
		}
		if !s.cfg.DryRun {
			list, err := listContainers(ctx)
			if err != nil {
				return nil, err
			}
			if newName && slices.ContainsFunc(list, func(x Container) bool { return x.Name == p.Name }) {
				return nil, Userf("A container named %s already exists.", p.Name)
			}
			for _, m := range mounts {
				if !m.volume && boundUnderRoots(m.source) {
					if err := prepareBind(m.source); err != nil {
						return nil, err
					}
				}
			}
		}
		return args, nil
	}

	// Downloading an image can take minutes, longer than a request may. One job at a time.
	startJob := func(name string, work func(context.Context) string) error {
		jobMu.Lock()
		defer jobMu.Unlock()
		if job["state"] == "running" {
			return Userf("Another job is still running, please wait for it.")
		}
		job = map[string]string{"state": "running", "name": name}
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), dockerJobTime)
			defer cancel()
			if msg := work(bg); msg != "" {
				setJob("failed", name, msg)
				return
			}
			setJob("done", name, "")
		}()
		return nil
	}
	ops := s.registerDockerMore(startJob, createCheck)
	s.registerDockerOps(ops)
	s.registerTerminal()
	if !s.cfg.DryRun {
		go s.gitWatcher(ops)
	}

	s.handle("docker.create", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p dockerCreate
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		args, err := createCheck(ctx, p, true)
		if err != nil {
			return nil, err
		}
		image := strings.TrimSpace(p.Image)
		always := p.AlwaysPull
		return nil, startJob(p.Name, func(bg context.Context) string {
			exists := false
			if !always && !s.cfg.DryRun {
				_, ierr := run(bg, "docker", "image", "inspect", "--", image)
				exists = ierr == nil
			}
			if !exists {
				if err := s.apply(bg, "docker", "pull", "--", image); err != nil {
					if _, ierr := run(bg, "docker", "image", "inspect", "--", image); ierr != nil {
						return "The image could not be downloaded: " + dockerErr(err)
					}
				}
			}
			if err := s.apply(bg, "docker", args...); err != nil {
				return "The container could not be started: " + dockerErr(err)
			}
			return ""
		})
	})

	s.handle("docker.remove", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) {
			return nil, Userf("Invalid container name.")
		}
		if err := s.apply(ctx, "docker", "rm", "-f", "--", p.Name); err != nil {
			return nil, Userf("The container could not be removed: %s", dockerErr(err))
		}
		return nil, nil
	})

	s.handle("docker.images.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if s.cfg.DryRun {
			return []map[string]any{}, nil
		}
		out, err := run(ctx, "docker", "image", "ls", "--no-trunc", "--format", "{{json .}}")
		if err != nil {
			return nil, Userf("Docker is not responding.")
		}
		list, err := listContainers(ctx)
		if err != nil {
			return nil, err
		}
		res := []map[string]any{}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			var r struct{ ID, Repository, Tag, Size, CreatedAt string }
			if json.Unmarshal([]byte(l), &r) != nil || r.ID == "" {
				continue
			}
			name := r.Repository + ":" + r.Tag
			res = append(res, map[string]any{"id": r.ID, "name": name, "size": r.Size, "created": r.CreatedAt,
				"in_use": slices.ContainsFunc(list, func(c Container) bool { return c.Image == name })})
		}
		return res, nil
	})

	s.handle("docker.image.remove", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !dockerImageIDs.MatchString(p.ID) {
			return nil, Userf("Invalid image.")
		}
		if err := s.apply(ctx, "docker", "image", "rm", "--", p.ID); err != nil {
			return nil, Userf("The image could not be removed: %s", dockerErr(err))
		}
		return nil, nil
	})

	s.handle("docker.volumes.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		if s.cfg.DryRun {
			return []map[string]any{}, nil
		}
		out, err := run(ctx, "docker", "volume", "ls", "--format", "{{.Name}}")
		if err != nil {
			return nil, Userf("Docker is not responding.")
		}
		list, err := listContainers(ctx)
		if err != nil {
			return nil, err
		}
		res := []map[string]any{}
		for _, name := range strings.Fields(out) {
			users := []string{}
			for _, c := range list {
				if slices.Contains(c.Volumes, name) {
					users = append(users, c.Name)
				}
			}
			res = append(res, map[string]any{"name": name, "used_by": users})
		}
		return res, nil
	})

	nameAction := func(action string, build func(string) []string, what string) {
		s.handle(action, func(ctx context.Context, raw json.RawMessage) (any, error) {
			var p struct {
				Name string `json:"name"`
			}
			if err := decode(raw, &p); err != nil {
				return nil, err
			}
			if !containerNameRe.MatchString(p.Name) {
				return nil, Userf("Please enter a name of letters, digits, dots, underscores and hyphens.")
			}
			if err := s.apply(ctx, "docker", build(p.Name)...); err != nil {
				return nil, Userf("%s: %s", what, dockerErr(err))
			}
			return nil, nil
		})
	}
	nameAction("docker.volume.create", func(n string) []string { return []string{"volume", "create", "--", n} }, "The volume could not be created")
	nameAction("docker.volume.remove", func(n string) []string { return []string{"volume", "rm", "--", n} }, "The volume could not be removed")

	s.handle("docker.networks.get", func(ctx context.Context, _ json.RawMessage) (any, error) {
		nets, err := s.dockerNetworks(ctx)
		if err != nil {
			return nil, err
		}
		c, err := loadCfg()
		if err != nil {
			return nil, err
		}
		return map[string]any{"networks": nets, "parents": s.dockerParents(ctx, c)}, nil
	})

	s.handle("docker.network.create", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name     string   `json:"name"`
			Driver   string   `json:"driver"`
			Mode     string   `json:"mode"`
			Parent   string   `json:"parent"`
			Subnet   string   `json:"subnet"`
			Gateway  string   `json:"gateway"`
			Subnet6  string   `json:"subnet6"`
			Gateway6 string   `json:"gateway6"`
			Labels   []string `json:"labels"`
			Internal bool     `json:"internal"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) {
			return nil, Userf("Please enter a name of letters, digits, dots, underscores and hyphens.")
		}
		c, err := loadCfg()
		if err != nil {
			return nil, err
		}
		if p.Driver == "" {
			p.Driver = "ipvlan"
		}
		if !slices.Contains([]string{"ipvlan", "macvlan", "bridge"}, p.Driver) {
			return nil, Userf("Unknown network driver.")
		}
		bridge := p.Driver == "bridge"
		if !bridge && !slices.ContainsFunc(s.dockerParents(ctx, c), func(x DockerParent) bool { return x.Name == p.Parent }) {
			return nil, Userf("The interface %s cannot be used for a network.", p.Parent)
		}
		sub, err := netip.ParsePrefix(strings.TrimSpace(p.Subnet))
		if err != nil || !sub.Addr().Is4() || sub.Masked() != sub {
			return nil, Userf("\"%s\" is not a network. Write it like 192.168.42.0/24.", p.Subnet)
		}
		args := []string{"network", "create", "--driver", p.Driver, "--subnet", sub.String()}
		if !bridge {
			args = append(args, "-o", "parent="+p.Parent)
		}
		if p.Driver == "ipvlan" {
			mode := "l2"
			if p.Mode == "l3" {
				mode = "l3"
			}
			args = append(args, "-o", "ipvlan_mode="+mode)
		}
		if s6 := strings.TrimSpace(p.Subnet6); s6 != "" {
			px, err := netip.ParsePrefix(s6)
			if err != nil || !px.Addr().Is6() {
				return nil, Userf("\"%s\" is not an IPv6 network.", s6)
			}
			args = append(args, "--ipv6", "--subnet", px.String())
			if g6 := strings.TrimSpace(p.Gateway6); g6 != "" {
				a, err := netip.ParseAddr(g6)
				if err != nil || !px.Contains(a) {
					return nil, Userf("The gateway %s is not in the network %s.", g6, s6)
				}
				args = append(args, "--gateway", g6)
			}
		}
		labels, err := flagValues("label", p.Labels)
		if err != nil {
			return nil, err
		}
		for _, l := range labels {
			args = append(args, "--label", l)
		}
		if gw := strings.TrimSpace(p.Gateway); gw != "" {
			a, err := netip.ParseAddr(gw)
			if err != nil || !sub.Contains(a) {
				return nil, Userf("The gateway %s is not in the network %s.", gw, sub)
			}
			args = append(args, "--gateway", gw)
		} else if !p.Internal && !bridge {
			return nil, Userf("Please enter the gateway, or make the network internal.")
		}
		if p.Internal {
			args = append(args, "--internal")
		}
		if err := s.apply(ctx, "docker", append(args, "--", p.Name)...); err != nil {
			return nil, Userf("The network could not be created: %s", dockerErr(err))
		}
		return nil, nil
	})

	s.handle("docker.network.remove", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !containerNameRe.MatchString(p.Name) || p.Name == "host" || p.Name == "none" || p.Name == "bridge" {
			return nil, Userf("This network cannot be removed.")
		}
		if err := s.apply(ctx, "docker", "network", "rm", "--", p.Name); err != nil {
			return nil, Userf("The network could not be removed: %s", dockerErr(err))
		}
		return nil, nil
	})
}
