// SPDX-License-Identifier: AGPL-3.0-or-later

package home

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Shelly devices over their local HTTP interface. Generation 2 and newer speak RPC (/rpc/Shelly.GetStatus), generation 1
// has /status. Both answer GET /shelly with who they are. Only reading so far (stage 1 of the concept).

const (
	adapterShelly1 = "shelly1"
	adapterShelly2 = "shelly2"
)

// shellyIdentity is the answer of GET /shelly, for both generations.
type shellyIdentity struct {
	MAC  string `json:"mac"`
	Gen  int    `json:"gen"`
	Mod  string `json:"model"` // generation 2
	App  string `json:"app"`   // generation 2
	Ver  string `json:"ver"`   // generation 2
	Type string `json:"type"`  // generation 1
	FW   string `json:"fw"`    // generation 1
	Auth bool   `json:"auth"`  // generation 1
	AuE  bool   `json:"auth_en"`
}

// parseIdentity returns adapter, stable device ID, model and firmware. An answer that is not from a Shelly is an error.
func parseIdentity(b []byte) (adapter, id, model, fw string, auth bool, err error) {
	var s shellyIdentity
	if err = json.Unmarshal(b, &s); err != nil {
		return
	}
	if len(s.MAC) != 12 {
		return "", "", "", "", false, errors.New("not a Shelly")
	}
	id = "shelly-" + strings.ToLower(s.MAC)
	switch {
	case s.Gen >= 2:
		return adapterShelly2, id, firstOf(s.App, s.Mod), s.Ver, s.AuE, nil
	case s.Type != "":
		v := s.FW
		if i := strings.LastIndex(v, "/"); i >= 0 {
			v = v[i+1:]
		}
		return adapterShelly1, id, s.Type, v, s.Auth, nil
	}
	return "", "", "", "", false, errors.New("not a Shelly")
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func boolp(b bool) *bool        { return &b }
func intp(i int) *int           { return &i }
func floatp(f float64) *float64 { return &f }

// componentNo returns the number of a component key such as switch:1.
func componentNo(key string) int {
	_, n, _ := strings.Cut(key, ":")
	i, _ := strconv.Atoi(n)
	return i
}

type gen2Energy struct {
	Total *float64 `json:"total"`
}

type gen2Component struct {
	Output     *bool       `json:"output"`
	APower     *float64    `json:"apower"`
	AEnergy    *gen2Energy `json:"aenergy"`
	State      any         `json:"state"`
	CurrentPos *int        `json:"current_pos"`
	Brightness *float64    `json:"brightness"`
	TotalPower *float64    `json:"total_act_power"`
	TotalAct   *float64    `json:"total_act"`
}

// parseGen2 turns Shelly.GetStatus into entities. Components without a state of their own (an input that is detached
// reports null) do not appear.
func parseGen2(b []byte) ([]Entity, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []Entity{}
	var meter *Entity
	for _, k := range keys {
		typ, _, ok := strings.Cut(k, ":")
		if !ok {
			continue
		}
		var c gen2Component
		if err := json.Unmarshal(raw[k], &c); err != nil {
			continue // one odd component must not hide the others
		}
		energy := func() *float64 {
			if c.AEnergy != nil {
				return c.AEnergy.Total
			}
			return nil
		}
		switch typ {
		case "switch":
			out = append(out, Entity{ID: k, Kind: KindSwitch, On: c.Output, Power: c.APower, Energy: energy()})
		case "light", "rgb", "rgbw", "cct":
			e := Entity{ID: k, Kind: KindLight, On: c.Output, Power: c.APower, Energy: energy()}
			if c.Brightness != nil {
				e.Level = intp(int(*c.Brightness + 0.5))
			}
			out = append(out, e)
		case "cover":
			st, _ := c.State.(string)
			out = append(out, Entity{ID: k, Kind: KindCover, State: st, Position: c.CurrentPos, Power: c.APower, Energy: energy()})
		case "pm1":
			out = append(out, Entity{ID: k, Kind: KindPower, Power: c.APower, Energy: energy()})
		case "em":
			e := Entity{ID: k, Kind: KindPower, Power: c.TotalPower}
			out = append(out, e)
			meter = &out[len(out)-1]
		case "emdata":
			if meter != nil {
				meter.Energy = c.TotalAct
			}
		case "input":
			if on, ok := c.State.(bool); ok {
				out = append(out, Entity{ID: k, Kind: KindBinary, On: boolp(on)})
			}
		}
	}
	return out, nil
}

type gen1Status struct {
	Relays []struct {
		IsOn bool `json:"ison"`
	} `json:"relays"`
	Rollers []struct {
		State string   `json:"state"`
		Pos   *int     `json:"current_pos"`
		Power *float64 `json:"power"`
	} `json:"rollers"`
	Lights []struct {
		IsOn       bool     `json:"ison"`
		Brightness *int     `json:"brightness"`
		Power      *float64 `json:"power"`
	} `json:"lights"`
	Meters []struct {
		Power *float64 `json:"power"`
		Total *float64 `json:"total"` // Wh on most models, watt-minutes on a few: shown as reported
	} `json:"meters"`
}

// parseGen1 turns /status into entities. In roller mode a Shelly 2.5 still lists its relays, they are not usable then.
func parseGen1(b []byte) ([]Entity, error) {
	var s gen1Status
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	out := []Entity{}
	for i, r := range s.Rollers {
		st := r.State
		if st == "stop" {
			st = "stopped"
		}
		out = append(out, Entity{ID: fmt.Sprintf("roller:%d", i), Kind: KindCover, State: st, Position: r.Pos, Power: r.Power})
	}
	if len(s.Rollers) == 0 {
		for i, r := range s.Relays {
			e := Entity{ID: fmt.Sprintf("relay:%d", i), Kind: KindSwitch, On: boolp(r.IsOn)}
			if i < len(s.Meters) {
				e.Power = s.Meters[i].Power
			}
			out = append(out, e)
		}
	}
	for i, l := range s.Lights {
		out = append(out, Entity{ID: fmt.Sprintf("light:%d", i), Kind: KindLight, On: boolp(l.IsOn), Level: l.Brightness, Power: l.Power})
	}
	return out, nil
}
