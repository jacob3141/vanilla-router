// SPDX-License-Identifier: AGPL-3.0-or-later

package home

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"vanilla-router/internal/config"
)

// The owner's arrangement: rooms and what is assigned to them. It is not part of config.json, because it changes often and
// needs no network rollback (concept, section 3). Loading is as strict as for config.json.

const (
	schemaVersion = 1
	maxRooms      = 64
	maxName       = 64
)

// DeviceSettings is what the owner chose for a device.
type DeviceSettings struct {
	Name string `json:"name,omitempty"`
	Room string `json:"room,omitempty"`
}

type storeFile struct {
	Schema  int                       `json:"schema"`
	Rooms   []Room                    `json:"rooms"`
	Devices map[string]DeviceSettings `json:"devices"`
}

// Store keeps home.json.
type Store struct {
	path string
	mu   sync.Mutex
	f    storeFile
}

// ErrInvalid marks an error whose text may be shown to the owner.
var ErrInvalid = errors.New("invalid")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, a...)...)
}

// OpenStore reads the file. A missing file is an empty home.
func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, f: storeFile{Schema: schemaVersion, Rooms: []Room{}, Devices: map[string]DeviceSettings{}}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f storeFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Schema != schemaVersion {
		return nil, fmt.Errorf("%s: schema %d is not supported", path, f.Schema)
	}
	if f.Rooms == nil {
		f.Rooms = []Room{}
	}
	if f.Devices == nil {
		f.Devices = map[string]DeviceSettings{}
	}
	s.f = f
	return s, nil
}

func (s *Store) save() error {
	b, err := json.MarshalIndent(s.f, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(s.path, append(b, '\n'), 0o600)
}

func validName(n string, limit int) bool {
	if n == "" || utf8.RuneCountInString(n) > limit || strings.TrimSpace(n) != n {
		return false
	}
	return !strings.ContainsFunc(n, unicode.IsControl)
}

// Rooms returns a copy of the rooms.
func (s *Store) Rooms() []Room {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.f.Rooms)
}

// Device returns what the owner chose for a device.
func (s *Store) Device(id string) DeviceSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Devices[id]
}

// SetRoom creates a room (empty id) or renames one.
func (s *Store) SetRoom(id, name string) (Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validName(name, maxName) {
		return Room{}, invalid("The name needs 1 to %d characters", maxName)
	}
	if slices.ContainsFunc(s.f.Rooms, func(r Room) bool { return r.ID != id && strings.EqualFold(r.Name, name) }) {
		return Room{}, invalid("There is already a room called %s", name)
	}
	if id == "" {
		if len(s.f.Rooms) >= maxRooms {
			return Room{}, invalid("At most %d rooms", maxRooms)
		}
		b := make([]byte, 6)
		rand.Read(b)
		r := Room{ID: hex.EncodeToString(b), Name: name}
		s.f.Rooms = append(s.f.Rooms, r)
		return r, s.save()
	}
	i := slices.IndexFunc(s.f.Rooms, func(r Room) bool { return r.ID == id })
	if i < 0 {
		return Room{}, invalid("This room does not exist any more")
	}
	s.f.Rooms[i].Name = name
	return s.f.Rooms[i], s.save()
}

// DeleteRoom removes a room. Its devices become unsorted, nothing else is lost.
func (s *Store) DeleteRoom(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.f.Rooms = slices.DeleteFunc(s.f.Rooms, func(r Room) bool { return r.ID == id })
	for k, d := range s.f.Devices {
		if d.Room == id {
			d.Room = ""
			s.f.Devices[k] = d
		}
	}
	return s.save()
}

// SetDevice stores name and room of a device. An empty name means the automatic one again.
func (s *Store) SetDevice(id, name, room string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name != "" && !validName(name, maxName) {
		return invalid("The name needs 1 to %d characters", maxName)
	}
	if room != "" && !slices.ContainsFunc(s.f.Rooms, func(r Room) bool { return r.ID == room }) {
		return invalid("This room does not exist any more")
	}
	if name == "" && room == "" {
		delete(s.f.Devices, id)
	} else {
		s.f.Devices[id] = DeviceSettings{Name: name, Room: room}
	}
	return s.save()
}
