// SPDX-License-Identifier: AGPL-3.0-or-later

package home

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	ActionOn    = "on"
	ActionOff   = "off"
	ActionOpen  = "open"
	ActionClose = "close"
	ActionStop  = "stop"
	ActionLevel = "level"    // brightness in percent
	ActionPos   = "position" // cover: 0 closed, 100 open
)

const commandTime = 4 * time.Second

// allowedAction says what can be done with an entity of a kind. The list is closed, and nothing from the
// request reaches the URL of a device: action and component number are taken from known values.
func allowedAction(e Entity, action string) bool {
	switch e.Kind {
	case KindSwitch:
		return action == ActionOn || action == ActionOff
	case KindLight:
		return action == ActionOn || action == ActionOff || action == ActionLevel && e.Level != nil
	case KindCover:
		// Without a calibrated position the device cannot go to one
		return action == ActionOpen || action == ActionClose || action == ActionStop || action == ActionPos && e.Position != nil
	}
	return false
}

// checkValue: a number is only for the actions that need one, and within their range. Brightness 0 would
// be "off", for which there is a switch.
func checkValue(action string, value *int) error {
	lowest := -1
	switch action {
	case ActionLevel:
		lowest = 1
	case ActionPos:
		lowest = 0
	}
	if lowest < 0 {
		if value != nil {
			return fmt.Errorf("%w: This function takes no value.", ErrInvalid)
		}
		return nil
	}
	if value == nil || *value < lowest || *value > 100 {
		return fmt.Errorf("%w: The value must be between %d and 100.", ErrInvalid, lowest)
	}
	return nil
}

// gen2RPC maps a component type to the name of its RPC namespace.
var gen2RPC = map[string]string{"switch": "Switch", "light": "Light", "rgb": "RGB", "rgbw": "RGBW", "cct": "CCT"}

func commandPath(adapter string, e Entity, action string, value *int) (string, error) {
	typ, _, _ := strings.Cut(e.ID, ":")
	no := strconv.Itoa(componentNo(e.ID))
	v := ""
	if value != nil {
		v = strconv.Itoa(*value)
	}
	if adapter == adapterShelly2 {
		if typ == "cover" {
			if action == ActionPos {
				return "/rpc/Cover.GoToPosition?id=" + no + "&pos=" + v, nil
			}
			return "/rpc/Cover." + map[string]string{ActionOpen: "Open", ActionClose: "Close", ActionStop: "Stop"}[action] + "?id=" + no, nil
		}
		ns, ok := gen2RPC[typ]
		if !ok {
			return "", fmt.Errorf("%w: This function cannot be controlled.", ErrInvalid)
		}
		if action == ActionLevel {
			return "/rpc/" + ns + ".Set?id=" + no + "&on=true&brightness=" + v, nil
		}
		return "/rpc/" + ns + ".Set?id=" + no + "&on=" + strconv.FormatBool(action == ActionOn), nil
	}
	if e.Kind == KindCover {
		if action == ActionPos {
			return "/roller/" + no + "?go=to_pos&roller_pos=" + v, nil
		}
		return "/roller/" + no + "?go=" + action, nil
	}
	base := e.endpoint
	if base == "" {
		base = "/relay/" + no
	}
	if action == ActionLevel {
		param := "brightness"
		if strings.HasPrefix(base, "/color/") {
			param = "gain" // the color mode of an RGBW2 has no brightness of its own
		}
		return base + "?turn=on&" + param + "=" + v, nil
	}
	return base + "?turn=" + action, nil
}

// Command carries out an action on one entity and reads the device again, so that the page shows the result at once.
func (s *Service) Command(ctx context.Context, device, entity, action string, value *int) error {
	if err := checkValue(action, value); err != nil {
		return err
	}
	s.mu.RLock()
	var dev Device
	var auth, found bool
	key := ""
	for h, k := range s.hosts {
		if k.dev.ID == device {
			dev, auth, found, key = k.dev, k.auth, true, h
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return fmt.Errorf("%w: This device is not known.", ErrInvalid)
	}
	var e *Entity
	for i := range dev.Entities {
		if dev.Entities[i].ID == entity {
			e = &dev.Entities[i]
		}
	}
	if e == nil || !allowedAction(*e, action) {
		return fmt.Errorf("%w: This function cannot be controlled.", ErrInvalid)
	}
	if s.Fake != nil {
		s.applyFake(device, entity, action, value)
		return nil
	}
	if !dev.Online {
		return fmt.Errorf("%w: The device is offline.", ErrInvalid)
	}
	if auth {
		return fmt.Errorf("%w: This device is protected by a password, which is not supported yet.", ErrInvalid)
	}
	path, err := commandPath(dev.Adapter, *e, action, value)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, commandTime)
	defer cancel()
	if _, err := s.get(ctx, "http://"+dev.Host+path); err != nil {
		s.Log.Warn("smart home: command failed", "device", device, "err", err)
		return fmt.Errorf("%w: The device did not answer.", ErrInvalid)
	}
	s.reread(ctx, key)
	return nil
}

// reread updates one device outside the regular rounds.
func (s *Service) reread(ctx context.Context, host string) {
	s.mu.RLock()
	k := s.hosts[host]
	if k == nil {
		s.mu.RUnlock()
		return
	}
	c := *k
	s.mu.RUnlock()
	s.probe(ctx, Target{Host: c.dev.Host}, &c)
	s.mu.Lock()
	*k = c
	s.mu.Unlock()
	s.bump()
}

// applyFake changes a simulated device, for the test instance.
func (s *Service) applyFake(device, entity, action string, value *int) {
	s.mu.Lock()
	for j := range s.Fake {
		if s.Fake[j].ID != device {
			continue
		}
		ents := append([]Entity(nil), s.Fake[j].Entities...)
		for n := range ents {
			if ents[n].ID != entity {
				continue
			}
			switch action {
			case ActionOn, ActionOff:
				ents[n].On = boolp(action == ActionOn)
			case ActionOpen:
				ents[n].State, ents[n].Position = "open", intp(100)
			case ActionClose:
				ents[n].State, ents[n].Position = "closed", intp(0)
			case ActionStop:
				ents[n].State = "stopped"
			case ActionLevel:
				ents[n].On, ents[n].Level = boolp(true), value
			case ActionPos:
				ents[n].State, ents[n].Position = "stopped", value
			}
		}
		s.Fake[j].Entities = ents
		if k := s.hosts[s.Fake[j].ID]; k != nil {
			k.dev.Entities = ents
		}
	}
	s.mu.Unlock()
	s.bump()
}
