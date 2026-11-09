# Concept: a "Smart Home" area in vanilla-router

| | |
|---|---|
| **Status** | Proposal, nothing implemented. Open decisions in section 11 |
| **As of** | 2026-11-09, based on vanilla-router 0.0.9 |
| **Wish** | Bring devices together in the context of the home rather than the network: group them, sort them by room, build dashboards and serve them, later in kiosk mode on a wall-mounted iPad |
| **Reads with** | [PLAN.md](../PLAN.md) (authoritative), [architecture.md](architecture.md) |

All IDs in section 10 are proposals. PLAN.md stays unchanged until the owner adopts them.

## 1. Summary

**Recommendation:** build it ourselves, as a separate unprivileged service `vr-home`, with one small adapter per kind of device and no dependency on a
cloud or another home automation system. The router has one advantage that no add-on has: it already knows every device on the network, by name and
address, so adding a device is a confirmation and not a setup.

| Topic | Decision proposed | Main reason |
|---|---|---|
| Foundation | Own implementation in Go, standard library only | Same rules as the rest of the project (D-01). One system, one update path, one interface |
| Process | New service `vr-home`, no root, no listening port, reached through the web server | A misbehaving device or adapter must never touch the router function |
| Data | Own file `home.json`, not `config.json` | Rooms and dashboards change constantly and need no network rollback |
| Model | Device → entities with a fixed set of kinds → rooms and groups | A dashboard knows "a switch", not "a Shelly" |
| Devices | Local protocols only. Cloud adapters only as an explicit exception | Works without internet, no account, no data leaves the home |
| Live data | Server-Sent Events, as the overview page already does | Standard library, passes the existing CSP, reconnects by itself |
| Dashboards | Grid editor in the web interface, served by the router | Nothing to install on the display |
| iPad | Own address per dashboard with a secret token, scoped rights, no login | A wall display cannot type a password, and a session that expires is useless |
| Certificates | The router offers its certificate for installation on devices | Safari rejects untrusted certificates outright [S-5] |
| Automations | Not in the first version, the model is prepared for them | Dashboards first, rules only once the device layer is proven |

**First target:** the Shelly devices, because they are the majority of what is already installed and have a documented local API. **Nothing here has been
tested against real devices yet**, stage 0 does that.

## 2. Requirements from PLAN.md

| ID | Consequence |
|---|---|
| P-01, P-02 | Laypeople must be able to use it, no YAML, no jargon |
| P-03 | No new open service, no default secrets |
| P-11 | Only ask what cannot be detected: devices are found, not typed in |
| D-01 | Go standard library only, frontend without frameworks. WebSocket and Protobuf have to be small hand-written parts |
| D-04 | The web server has no privileges, whatever it needs goes through fixed interfaces |
| NF-09 | Packages from *main*. This design needs no new package |
| Owner's rule | Software reaches the router only through an official release |

**Non-goals for now:** voice assistants, remote access from outside the home, HomeKit, Zigbee or Z-Wave radios.

## 2a. Results of stage 0 (2026-11-09)

Read-only probe of the real devices. All 33 Shelly devices answer locally, none has authentication turned on. Three are generation 1 (one Shelly 2.5 in roller
mode, two RGBW2), the others generation 2 (Plus 1PM, Plus 2PM, PlugS, i4, PM Mini, Pro 3EM). The inverter's port 10081 is open. **Consequence:** stage 1 reads
all of them by HTTP every 5 seconds, which is enough and needs no WebSocket client. Instant updates over WebSocket come with commands in stage 2.
Open: every device in the home network can switch every Shelly, because none has a password. Stage 2 should support passwords.

## 3. Architecture

```
iPad / browser ──HTTPS──▶ vr-webui ──Unix socket──▶ vr-home ──HTTP, WebSocket, TCP──▶ devices in the home network
                          login, CSRF,              adapters, state,
                          kiosk token check         history, dashboards
```

| Part | Design | Why |
|---|---|---|
| Process | `vr-home serve`, own user `vr-home`, no capabilities | Same pattern as `vr-webui files` |
| Exposure | Unix socket only, `/api/home/*` and `/k/<token>` proxied by `vr-webui` | No new port, no second certificate, existing login and CSRF checks |
| Network limits | `RestrictNetworkInterfaces=lan0`, `IPAddressAllow` for the home network | A compromised adapter cannot reach the internet or the router's other services |
| Secrets | Device passwords in `secrets/`, handed over by the helper through a fixed action | Never in `home.json`, never in an export without encryption |
| Data | `/var/lib/vanilla-router/home/`: `home.json`, device state cache, history | Survives updates, part of the configuration export |
| Failure | `vr-home` can crash or be switched off, the router does not notice | Routing must never depend on a light switch |

**`home.json` instead of `config.json`.** `config.json` is the source of truth for the network and every change goes through generate, check, apply and
rollback (D-02). Renaming a lamp or moving a tile needs none of that. Validation stays as strict: unknown fields are errors, and one function checks the
whole file before it is written.

## 4. Device model

```
Device       a physical thing: address, adapter, credentials reference, state (online, firmware)
 └─ Entity   one controllable or readable function, of a fixed kind
Room         a place, an entity belongs to one room
Group        any set of entities, operated together ("All blinds south side")
Dashboard    pages of tiles, each tile is bound to entities
```

| Entity kind | Reads | Commands | Example |
|---|---|---|---|
| `switch` | on/off | on, off, toggle | Plug, relay |
| `light` | on/off, brightness, colour | on, off, level, colour | Dimmer, RGBW |
| `cover` | position, moving | open, close, stop, position | Blinds |
| `power` | W | | Consumption of a plug |
| `energy` | kWh, resets handled | | Meter, PV yield |
| `temperature` | °C | set point, if the device has one | Heating circuit |
| `binary` | on/off | | Button, contact, "door rang" |
| `state` | text from a known list | | Washing machine: "rinsing" |
| `event` | moment and type | | Button press, doorbell |
| `camera` | live image | | Door station |

The list is closed on purpose: a new kind is a small code change, and in exchange every dashboard tile works with every device of that kind. A device that
offers more than the kinds can express keeps the rest in its adapter until there is a real use.

## 5. Finding and adding devices

This is where a router has an advantage. The DHCP table and the reservations already say which devices exist and how they are named.

1. `vr-home` goes through the known devices of the home network and asks each one politely whether it speaks a known protocol. For Shelly devices that is one
   `GET /shelly`, which identifies the device [S-3]. Devices are only asked if they have a reservation or a lease, nothing scans address ranges.
2. The page lists what was found: "12 new Shelly devices". A name comes from the reservation, as far as there is one.
3. The owner assigns rooms, in one pass over a list. Nothing is added without being confirmed.

mDNS is not needed for this. It would also be less reliable: second-generation Shelly devices answer queries but do not announce themselves [S-4].

A device with a fixed reservation keeps its address. A device without one should get one, and the page offers that in one click.

## 6. Adapters

Each adapter implements one small interface: identify, read entities, subscribe to changes, send a command.

| Device | Protocol | Status | Notes |
|---|---|---|---|
| **Shelly Gen2 and newer** (Plus, Pro, Mini) | RPC over WebSocket, the device pushes `NotifyStatus` [S-1] [S-2]. HTTP `/rpc/...` as a fallback | **Stage 1** | The router connects to each device. The device-initiated "outbound WebSocket" is not used: Shelly lists it as exempt from authentication [S-6] |
| **Shelly Gen1** (e.g. RGBW2) | HTTP status and commands, polled | Stage 2 | Only a few devices. Exact endpoints to be checked in stage 0 |
| **Hoymiles inverter with built-in Wi-Fi DTU** | Protobuf over TCP 10081 [S-7] | Stage 4 | Unofficial, reverse engineered. Own small decoder. How often it may be read is undocumented, so start slowly. Community reports say a local port can be missing on some models and firmware [S-7] |
| **Door station** with the project owner's firmware | The firmware's own hub API, JSON-RPC over WebSocket, video separately | Stage 4 | Doorbell as an `event`, image as a `camera` |
| **Heating** | Unknown until the model is known | After stage 0 | Connected by Ethernet, which raises the chance of a local interface |
| **Washing machine** | Unknown until the model is known | After stage 0 | Often cloud only. For Home Connect, local access exists only as an unofficial route needing a one-time cloud login and an extracted key [S-8] |

**WebSocket client:** the standard library has none. A client for what these devices need is small (RFC 6455, text frames, ping) and gets its own tests.
With several dozen devices that means several dozen long-lived connections, which is cheap in Go. Whether the devices keep them stable over days is to be
measured in stage 0.

**Cloud rule:** an adapter that needs a manufacturer's cloud is off by default, is marked as such on the page, and is only added when no local way exists.

## 7. Dashboards and the iPad

**Editor.** A grid of tiles, per dashboard one or more pages. Add a tile, pick an entity, drag it, resize it. Sensible tiles by kind: switch, dimmer, blind,
meter, graph, room overview, doorbell image. Built with the same `h()` and no framework, under the existing CSP.

**Serving.** Each dashboard can be published under its own address, `https://router.home.arpa/k/<token>`:

| Property | Design | Why |
|---|---|---|
| Token | 256 bits, random, shown once as a link and a QR code, can be revoked | A wall display cannot type a password |
| Rights | Bound to this dashboard only. Each tile is either view-only or operable | A stolen token must not open the router's settings |
| Reach | Home network only, the firewall already ensures that | |
| Sensitive actions | Door opener and similar need a PIN on the display, or are simply not offered there | A tablet in the hallway is reachable from the hallway |
| Live data | Server-Sent Events, reconnect by itself, a visible "connection lost" state instead of stale values | A dashboard that silently shows old numbers is worse than none |

**Certificates on the iPad.** Today the router has a self-signed certificate. Safari does not offer an exception for that as a desktop browser does: it
rejects it, and the certificate has to be installed on the device and enabled under *Settings → General → About → Certificate Trust Settings*, and it has to
name the host the iPad uses [S-5]. So the router needs a page "Install on this device" that serves its certificate as a profile. This also removes the
warning on every other device in the home. Its private key must then be treated accordingly: it is what makes other devices trust this name.

**Kiosk setup** needs nothing from the router: Guided Access locks the iPad to one app, Auto-Lock is set to Never, the iPad stays on power [S-9]. Whoever
knows the passcode can leave Guided Access, which is fine at home [S-9]. Open point for stage 3: whether a web app added to the Home Screen behaves better than a
Safari tab under Guided Access. The sources found do not answer that.

**For a display that runs for years:** dim or darken after inactivity to protect the panel, reload the page by itself after a router update, no animation that
runs constantly.

## 8. History

Power and energy are only useful over time. `vr-home` keeps one value per entity per minute for 48 hours, then hourly values for two years, in a compact file
per entity. No database: the volume is small, and it stays readable and part of the backup. Writes are batched, so that an SSD is not worn by a thermometer.

## 9. Stages

| Stage | Content | Done when |
|---|---|---|
| **0. Inventory** | Models of heating, washing machine, iPad. Read-only probe of the real devices: which Shellys use authentication, how stable the connections are, whether the inverter's port answers | A table of what really works |
| **1. Core and Shelly, read only** | `vr-home`, `home.json`, model, WebSocket client, Shelly Gen2, discovery, page with rooms | Every Shelly shows live values |
| **2. Control** | Commands, groups, Shelly Gen1 | Light and blinds work from the page |
| **3. Dashboards and iPad** | Editor, tiles, tokens, certificate profile, kiosk view | One dashboard runs for a week on the wall |
| **4. Energy and door** | Inverter, door station, history and graphs | PV and doorbell on the dashboard |
| **5. Heating and washing machine** | According to stage 0 | |
| **6. Automations** | Simple rules on the existing model (time, event → action), scenes | Decided when the rest has proven itself |

Every stage ends with an official release, as for everything else on the router.

## 10. Proposed entries for PLAN.md

Numbers are provisional, PLAN.md has moved on since earlier concepts.

| ID | Proposal |
|---|---|
| D-NN | Smart Home as an own service `vr-home` without root and without a port of its own, reached through `vr-webui` |
| D-NN | Rooms, devices and dashboards in `home.json`, not in `config.json` |
| D-NN | Local protocols only. Cloud adapters are an exception that is marked and off by default |
| F-HOME-01 | Discover devices from the router's DHCP data, confirmed by the owner |
| F-HOME-02 | Rooms and groups |
| F-HOME-03 | Live state and commands for the entity kinds of section 4 |
| F-HOME-04 | Dashboard editor |
| F-HOME-05 | Published dashboards with a revocable token and rights per tile |
| F-HOME-06 | Certificate of the router installable on devices |
| F-HOME-07 | History of power and energy |
| NF-NN | A failure of `vr-home` never affects routing, DNS or DHCP |

## 11. Decisions

| ID | Question | Recommendation |
|---|---|---|
| **Q-A** | Own implementation, or Home Assistant as a container and only dashboards on top? | **Decided by the owner on 2026-11-09: own implementation.** Home Assistant has far more integrations, and that is its real strength: each further unusual device costs us work. But it is a second system with its own updates, users and interface, and it was just removed from this router. If the list of devices later grows beyond what is reasonable to write adapters for, it can still be added as a container |
| **Q-B** | Which model are the heating and the washing machine? | Needed from the owner, decides whether they are possible locally at all |
| **Q-C** | Which iPad, which iOS version? | Needed from the owner: it decides which browser features can be used |
| **Q-D** | Should the door be opened from the wall display at all? | Recommendation: only with a PIN, or not at all at first |
| **Q-E** | Install the router's certificate on devices? | Yes. There is no good way around it on an iPad. A public certificate would need a public domain, which contradicts "home network only" |

## 12. Risks

| ID | Risk | Measure |
|---|---|---|
| R-1 | Reverse-engineered protocols change with a firmware update (inverter, washing machine) | Adapters are isolated, a broken one shows as offline and affects nothing else |
| R-2 | Many permanent WebSocket connections are unstable on small devices | Measure in stage 0, HTTP polling as a fallback |
| R-3 | A token in a QR code or on a photo gives access to the dashboard | Scope per dashboard, revocable, sensitive tiles need a PIN |
| R-4 | Writing adapters is a permanent cost | Closed list of kinds, Q-A can be revisited |
| R-5 | Devices are as weak as their firmware, and they sit in the home network | Later a separate segment for them (docs/sdn-concept.md, section 5.7). `vr-home` itself may only talk to the home network |
| R-6 | The installed certificate makes devices trust this router for any name | Restrict the name constraints of the certificate to the router's own name, verify on iOS in stage 3 |

## 13. Sources

Found through searches on 2026-11-09. Not checked against real devices.

- [S-1] Shelly: Outbound WebSocket and RPC channels, https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Ws
- [S-2] Shelly: Notifications, https://shelly-api-docs.shelly.cloud/gen2/General/Notifications
- [S-3] Shelly: `/shelly` and `Shelly.GetDeviceInfo`, https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Shelly
- [S-4] Shelly: network design guide, https://us.shelly.com/blogs/documentation/network-design-guide-for-shelly-gen2-devices
- [S-5] code-server: notes on iPad, Safari and self-signed certificates, https://github.com/coder/code-server/blob/v3.8.0/doc/ipad.md, and Kurento, https://doc-kurento.readthedocs.io/en/stable/knowledge/selfsigned_certs.html
- [S-6] Shelly: Authentication, https://shelly-api-docs.shelly.cloud/gen2/General/Authentication/
- [S-7] `hoymiles-wifi`, https://pypi.org/project/hoymiles-wifi/, and ioBroker adapter, https://cdn.jsdelivr.net/npm/iobroker.hoymiles@0.4.1/README.md
- [S-8] `hcpy`, https://github.com/osresearch/hcpy
- [S-9] iPad kiosk with Guided Access, https://blog.kandji.io/set-up-manage-ipad-kiosk, and https://help.virtualfrontdesk.com/en/article/ipad-kiosk-mode-guided-access
