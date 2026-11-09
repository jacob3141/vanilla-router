// SPDX-License-Identifier: AGPL-3.0-or-later

// Package home is the Smart Home area of vanilla-router (docs/smarthome-concept.md): it finds devices among the
// addresses the router already knows, reads them over their local protocols and sorts them into rooms. It runs as its own
// unprivileged service: nothing here may ever affect routing.
package home

import "time"

// Kinds of entities. The list is closed on purpose: a dashboard tile works with every device of a kind.
const (
	KindSwitch = "switch"
	KindLight  = "light"
	KindCover  = "cover"
	KindPower  = "power"
	KindBinary = "binary"
)

// Entity is one readable function of a device.
type Entity struct {
	ID       string   `json:"id"` // unique within the device, e.g. switch:0
	Kind     string   `json:"kind"`
	Name     string   `json:"name,omitempty"`
	On       *bool    `json:"on,omitempty"`
	Level    *int     `json:"level,omitempty"`    // brightness in percent
	Position *int     `json:"position,omitempty"` // cover: 0 closed, 100 open
	State    string   `json:"state,omitempty"`    // cover: open, closed, opening, closing, stopped
	Power    *float64 `json:"power,omitempty"`    // W
	Energy   *float64 `json:"energy,omitempty"`   // Wh, counter of the device

	endpoint string // generation 1: URL path for commands, depends on the mode of the device
}

// Device is a found device with its latest values.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`      // chosen by the owner, otherwise from the reservation or the device
	Renamed  bool      `json:"renamed"`   // the name was chosen by the owner
	Room     string    `json:"room"`      // room ID, empty = not sorted yet
	Host     string    `json:"host"`      // address
	Adapter  string    `json:"adapter"`   // shelly1, shelly2
	Model    string    `json:"model"`     // as the device reports it
	Firmware string    `json:"firmware"`  //
	Online   bool      `json:"online"`    //
	LastSeen time.Time `json:"last_seen"` //
	Entities []Entity  `json:"entities"`
}

// Room is a place in the home.
type Room struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Overview is what the page shows.
type Overview struct {
	Rooms   []Room   `json:"rooms"`
	Devices []Device `json:"devices"`
}
