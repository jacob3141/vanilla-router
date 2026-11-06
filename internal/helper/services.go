// SPDX-License-Identifier: AGPL-3.0-or-later

package helper

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Services the web interface may show and restart. The web interface itself and its
// helper are deliberately only in the log list: a restart from within the web interface would cut it off.
// Extensions (Docker, Samba) only appear if they are installed (unitInfos hides
// units that do not exist).
var manageableUnits = []string{
	"vr-dnsmasq", "systemd-networkd", "vr-firewall", "vr-pppoe", "vr-hostapd", "systemd-resolved", "chrony", "ssh",
	"unattended-upgrades", "cron", "docker", "smbd",
}

var logUnits = append(slices.Clone(manageableUnits), "vr-webui", "vr-webui-helper", "vr-health", "apt-daily-upgrade")

var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type UnitInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Since       string `json:"since"`
	Enabled     string `json:"enabled"`
	Restartable bool   `json:"restartable"`
}

func unitInfos(ctx context.Context, units []string) ([]UnitInfo, error) {
	args := []string{"show", "--timestamp=unix", "--property=Id,Description,LoadState,ActiveState,SubState,ActiveEnterTimestamp,UnitFileState", "--"}
	for _, u := range units {
		args = append(args, u+".service")
	}
	out, err := run(ctx, "systemctl", args...)
	if err != nil {
		return nil, err
	}
	var res []UnitInfo
	var cur UnitInfo
	notFound := false
	flush := func() {
		if cur.Name != "" && !notFound {
			cur.Restartable = slices.Contains(manageableUnits, cur.Name)
			res = append(res, cur)
		}
		cur = UnitInfo{}
		notFound = false
	}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			if strings.TrimSpace(line) == "" {
				flush()
			}
			continue
		}
		switch k {
		case "Id":
			cur.Name = strings.TrimSuffix(v, ".service")
		case "Description":
			cur.Description = v
		case "LoadState":
			notFound = v == "not-found"
		case "ActiveState":
			cur.Active = v
		case "SubState":
			cur.Sub = v
		case "ActiveEnterTimestamp":
			cur.Since = v
		case "UnitFileState":
			cur.Enabled = v
		}
	}
	flush()
	return res, nil
}

func clampLines(n int) string {
	if n <= 0 {
		n = 200
	}
	return strconv.Itoa(min(n, 2000))
}

type Container struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	State        string `json:"state"`
	Status       string `json:"status"`
	Health       string `json:"health,omitempty"`
	IP           string `json:"ip,omitempty"`
	ImageCreated string `json:"image_created,omitempty"`
	Restart      string `json:"restart_policy"`
	DockerSock   bool   `json:"docker_sock"`
}

func listContainers(ctx context.Context) ([]Container, error) {
	ids, err := run(ctx, "docker", "ps", "-aq", "--no-trunc")
	if err != nil {
		return nil, Userf("Docker is not responding.")
	}
	fields := strings.Fields(ids)
	if len(fields) == 0 {
		return []Container{}, nil
	}
	out, err := run(ctx, "docker", append([]string{"inspect"}, fields...)...)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name   string
		Image  string
		Config struct{ Image string }
		State  struct {
			Status string
			Health *struct{ Status string }
		}
		HostConfig struct {
			RestartPolicy struct{ Name string }
		}
		Mounts          []struct{ Source string }
		NetworkSettings struct {
			Networks map[string]struct{ IPAddress string }
		}
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	var res []Container
	created := map[string]string{}
	for _, r := range raw {
		c := Container{Name: strings.TrimPrefix(r.Name, "/"), Image: r.Config.Image, State: r.State.Status, Restart: r.HostConfig.RestartPolicy.Name}
		if r.State.Health != nil {
			c.Health = r.State.Health.Status
		}
		for _, n := range r.NetworkSettings.Networks {
			if n.IPAddress != "" {
				c.IP = n.IPAddress
			}
		}
		for _, m := range r.Mounts {
			if m.Source == "/var/run/docker.sock" || m.Source == "/run/docker.sock" {
				c.DockerSock = true
			}
		}
		if _, ok := created[r.Image]; !ok {
			t, _ := run(ctx, "docker", "image", "inspect", "-f", "{{.Created}}", r.Image)
			created[r.Image] = strings.TrimSpace(t)
		}
		c.ImageCreated = created[r.Image]
		res = append(res, c)
	}
	slices.SortFunc(res, func(a, b Container) int { return strings.Compare(a.Name, b.Name) })
	return res, nil
}

func (s *Server) registerServices() {
	s.handle("services.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return unitInfos(ctx, logUnits)
	})

	s.handle("service.restart", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Unit string `json:"unit"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !slices.Contains(manageableUnits, p.Unit) {
			return nil, Userf("The service %q may not be restarted through the web interface.", p.Unit)
		}
		if s.cfg.DryRun {
			s.cfg.Log.Info("dry run: restart skipped", "unit", p.Unit)
			return nil, nil
		}
		if err := restartUnit(ctx, p.Unit); err != nil {
			return nil, Userf("%s could not be restarted: %v", p.Unit, err)
		}
		return nil, nil
	})

	s.handle("service.logs.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Unit  string `json:"unit"`
			Lines int    `json:"lines"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if !slices.Contains(logUnits, p.Unit) {
			return nil, Userf("There are no logs for %q here.", p.Unit)
		}
		out, err := run(ctx, "journalctl", "-u", p.Unit+".service", "-n", clampLines(p.Lines), "--no-pager", "-o", "short-iso")
		if err != nil {
			return nil, err
		}
		return map[string]string{"text": out}, nil
	})

	s.handle("docker.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		return listContainers(ctx)
	})

	// Only containers that exist: the name is checked against the current list
	knownContainer := func(ctx context.Context, name string) error {
		if !containerNameRe.MatchString(name) {
			return Userf("Invalid container name.")
		}
		list, err := listContainers(ctx)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(list, func(c Container) bool { return c.Name == name }) {
			return Userf("The container %s does not exist.", name)
		}
		return nil
	}

	s.handle("docker.action", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Action != "start" && p.Action != "stop" && p.Action != "restart" {
			return nil, Userf("Unknown action %q.", p.Action)
		}
		if err := knownContainer(ctx, p.Name); err != nil {
			return nil, err
		}
		if err := s.apply(ctx, "docker", p.Action, "--", p.Name); err != nil {
			return nil, Userf("docker %s %s failed: %s", p.Action, p.Name, lastLines(err.Error(), 1))
		}
		return nil, nil
	})

	s.handle("docker.logs.get", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Name  string `json:"name"`
			Lines int    `json:"lines"`
		}
		if err := decode(raw, &p); err != nil {
			return nil, err
		}
		if err := knownContainer(ctx, p.Name); err != nil {
			return nil, err
		}
		out, _ := run(ctx, "docker", "logs", "--tail", clampLines(p.Lines), "--timestamps", "--", p.Name)
		return map[string]string{"text": out}, nil
	})
}
