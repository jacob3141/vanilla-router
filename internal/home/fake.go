// SPDX-License-Identifier: AGPL-3.0-or-later

package home

// FakeDevices are simulated devices for the test instance. Nothing is contacted.
func FakeDevices() []Device {
	mk := func(id, name, adapter, model, host string, e ...Entity) Device {
		return Device{ID: id, Name: name, Adapter: adapter, Model: model, Host: host, Online: true, Entities: e}
	}
	return []Device{
		mk("sim-1", "kitchen-spots", adapterShelly2, "Plus1PM", "192.0.2.1", Entity{ID: "switch:0", Kind: KindSwitch, On: boolp(true), Power: floatp(14.5)}),
		mk("sim-2", "kitchen-blind", adapterShelly2, "Plus2PM", "192.0.2.2", Entity{ID: "cover:0", Kind: KindCover, State: "stopped", Position: intp(60), Power: floatp(0)}),
		mk("sim-3", "dining-light", adapterShelly1, "SHRGBW2", "192.0.2.3", Entity{ID: "light:0", Kind: KindLight, On: boolp(true), Level: intp(40), Power: floatp(6)}),
		mk("sim-4", "house-meter", adapterShelly2, "Pro3EM", "192.0.2.4", Entity{ID: "em:0", Kind: KindPower, Power: floatp(4264)}),
	}
}
