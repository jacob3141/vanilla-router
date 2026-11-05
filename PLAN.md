---
document: vanilla-router-plan
title: "vanilla-router: Planpapier"
version: 0.17
status: entwurf
license: AGPL-3.0-or-later
updated: 2026-11-05
language: de
id_schema:
  principle: "P-NN"
  lesson: "L-NN"
  requirement: "F-<BEREICH>-NN (funktional) | NF-NN (nicht funktional)"
  decision: "D-NN"
  question: "Q-NN"
  risk: "R-NN"
  milestone: "MN"
  task: "T-MN-NN"
  source: "S-NN"
status_values: [offen, in-arbeit, erledigt, verworfen]
priority_values: [MUSS, SOLL, KANN]
---

# vanilla-router: Planpapier

> **Hinweis für Agenten:** Dieses Dokument ist die führende Planungsquelle des Projekts. IDs sind
> stabil: Sie werden nie neu vergeben und nie umnummeriert. Was verworfen wird, bekommt den Status
> `verworfen` und bleibt stehen. Neue Einträge bekommen die nächste freie Nummer. Bei jeder
> inhaltlichen Änderung `version` und `updated` im Kopf anpassen und unten im Änderungsverlauf
> eintragen. Entscheidungen (D-NN) werden nur mit Begründung geändert.

---

## 1. Vision

Ein **Router aus einem ganz normalen Linux** auf **beliebiger Standardhardware** mit mindestens zwei
Netzwerkanschlüssen, eingerichtet von einem **Boot-Stick, der auch Laien Schritt für Schritt führt**.

Der Ablauf aus Sicht des Nutzers (festgelegt vom Projektinhaber am 2026-11-04, D-15):

1. **Stick erstellen:** Unter Windows ein Programm herunterladen und ohne Installation starten,
   den USB-Stick auswählen, „Stick erstellen“. Es gibt keine Fragen und keine Vorab-Konfiguration (3.12).
2. **Booten:** Stick abziehen, in den künftigen Router stecken, einschalten. Der Rechner startet ein
   kleines Linux vom Stick.
3. **Nur das Nötigste:** Der Assistent fragt nur, was sich nicht erkennen lässt, also was physisch ist:
   > „Stecken Sie jetzt das Kabel vom Modem in einen der Netzwerkanschlüsse.“

   Der Assistent erkennt den Port, macht ihn zum Internet-Anschluss (WAN), alle anderen werden das
   Heimnetz (LAN). Dazu kommt die Bestätigung, welche Festplatte gelöscht wird. Mehr nicht.
4. **Alles Weitere im Browser:** Zugangsdaten des Anbieters, Name, Netze und jede andere Einstellung
   kommen über die Weboberfläche.

### 1.1 Warum

Spezialisierte Router-Betriebssysteme wie OpenWrt sind für Geräte mit wenigen MB Flash und RAM
gebaut. Daraus folgen abgespeckte Werkzeuge, eine eigene Paketwelt, eine eingeschränkte
Oberfläche und eine hohe Einstiegshürde. Gebrauchte Büro-PCs oder Mini-PCs mit mehreren
Netzwerkanschlüssen kosten heute oft nur 100–200 € und brauchen im Leerlauf etwa 6–15 W.
Darauf läuft ein vollwertiges Linux mit allen Paketen, Sicherheitsupdates und Werkzeugen. Das
Konzept „Router-OS für minimale Hardware“ ist für diese Geräte überholt.

Die vorhandenen Alternativen für x86-Hardware (OPNsense/pfSense auf FreeBSD, IPFire, VyOS) sind
entweder Appliances für Fachleute oder eigene Distributionen. **Ein unverändertes Debian/Ubuntu
mit einem Assistenten für Laien davor gibt es nicht.** Diese Lücke soll vanilla-router füllen.

### 1.2 Grundsätze

| ID | Grundsatz | Bedeutung |
|---|---|---|
| P-01 | **Vanilla statt Spezial-OS** | Basis ist eine unveränderte Standard-Distribution (LTS). Es gibt keinen eigenen Kernel und keine eigenen Paketquellen für das Grundsystem. Alles, was vanilla-router hinzufügt, ist ein normales Paket bzw. normale Konfiguration, die man verstehen und notfalls von Hand ändern kann. |
| P-02 | **Laientauglich** | Ohne Linux-Kenntnisse vom nackten Rechner zum laufenden Router. Keine Fachbegriffe ohne Erklärung, jeder Schritt mit Rückmeldung, sinnvolle Vorgaben statt Fragen. |
| P-03 | **Sicher ab Werk** | Firewall für IPv4 **und** IPv6 vor dem ersten Paket, keine offenen Dienste nach außen, automatische Sicherheitsupdates, keine Standardpasswörter. |
| P-04 | **Eine Wahrheitsquelle** | Der gewünschte Zustand steht in einer einzigen deklarativen Datei. Alle Konfigurationsdateien werden daraus erzeugt, nie per Textsuche bearbeitet. |
| P-05 | **Prüfen vor Einspielen, Rücknahme bei Fehler** | Jede Änderung wird erzeugt, mit dem Werkzeug des Dienstes geprüft (`nft -c`, `dnsmasq --test` …), atomar ersetzt und nach dem Neuladen auf Gesundheit geprüft. Bei Fehlern wird automatisch zurückgerollt. |
| P-06 | **Reparierbar** | Updates und Konfigurationsänderungen lassen sich rückgängig machen (Snapshots, alter Stand im Bootmenü). Ein Router, der nach einem Update nicht hochkommt, ist für Laien ein Totalausfall. |
| P-07 | **Offline installierbar** | Der Stick enthält alles für eine funktionierende Grundinstallation. Internet braucht es erst für Updates. |
| P-08 | **Wenig Abhängigkeiten** | Eigener Code in Go nur mit der Standardbibliothek, Frontend ohne Frameworks und ohne CDN. Das hält die Angriffsfläche klein und das Projekt langlebig. |
| P-09 | **Keine Telemetrie** | Der Router meldet nichts nach Hause. Prüfungen der Verbindung gehen nur an neutrale, dokumentierte Ziele. |
| P-10 | **Komfort vor Optimierung** | Wenn Bequemlichkeit (bekannte Distribution, große Paketauswahl, verbreitete Anleitungen, vertraute Werkzeuge) und Optimierung (kleinstes System, wenigste Pakete, letzte Prozent Leistung) sich widersprechen, gewinnt der Komfort. Die Hardware hat Reserven genug. Sicherheit (P-03) ist davon ausgenommen und wird nie für Komfort geopfert. Festgelegt vom Projektinhaber am 2026-11-04. |
| P-11 | **Nur fragen, was sich nicht erkennen lässt** | Der Assistent auf dem Stick fragt nur nach Physischem: welches Kabel ins Modem geht und welche Festplatte gelöscht werden darf. Alles andere bekommt eine sinnvolle Vorgabe und lässt sich später in der Weboberfläche ändern. Auf dem Stick gibt es keine Texteingabe, nur Pfeiltasten und Enter. Damit spielt auch die Tastaturbelegung keine Rolle. Festgelegt vom Projektinhaber am 2026-11-04. |

### 1.3 Zielgruppen

| ID | Gruppe | Erwartung |
|---|---|---|
| Z-1 | **Laien** (Haushalt, Familie) | Programm unter Windows starten, Stick wählen, Stick in den Router, Modem-Kabel einstecken, fertig. Danach nur noch die Weboberfläche, nie eine Konsole. |
| Z-2 | **Bastler und Selbsthoster** | Volle Kontrolle über ein normales Linux, Docker, NAS und eigene Dienste auf demselben Gerät, ohne dass die Router-Funktion leidet. |
| Z-3 | **Kleine Büros, Vereine** | Wie Z-1, dazu Gastnetz (später VLAN), Portweiterleitungen und Fernzugang (später VPN). |

### 1.4 Nicht-Ziele (vorerst)

- Kein Ersatz für Enterprise-Firewalls (HA-Cluster, IDS/IPS, deep packet inspection).
- Kein eigener WLAN-Access-Point in Version 1 (siehe Q-06). Empfehlung: externe Access Points.
- Keine Unterstützung für ARM-Einplatinenrechner in Version 1 (x86-64 zuerst, siehe Q-08).
- Kein Modem: DSL-, Kabel- und Glasfasermodems bleiben eigene Geräte (Bridge-Modus oder Router davor).

---

## 2. Ausgangslage: der Prototyp

Der Prototyp ist ein von Hand eingerichteter Router im Haushalt des Projektgründers, im Einsatz seit 2025.
Im November 2026 wurde er gesichert, gehärtet und um eine Weboberfläche (`routerui`) ergänzt. Der
Code dazu liegt derzeit in einem **privaten** Arbeits-Repo und soll bereinigt übernommen werden (M2).

### 2.1 Hardware und Grundsystem

| Punkt | Prototyp |
|---|---|
| Gerät | Dell OptiPlex 3050 (Intel Core i3-6100T, 4 Threads, 8 GB RAM, NVMe 2 TB, UEFI) |
| Netzwerk | 2 × Ethernet: Onboard (WAN, Link mit 100 Mbit/s erzwungen) und eine zweite Karte (LAN, 1 Gbit/s) |
| System | Ubuntu 26.04 LTS (ursprünglich 25.04, am 2026-11-03 aktualisiert), Kernel 7.0 |
| Anschluss | WAN per DHCP hinter einem Provider-Router (Kabel, doppeltes NAT). Öffentliches IPv6 nur auf dem WAN-Interface, keine Prefix Delegation ins LAN |
| Nebenrollen | NAS (Samba-Freigabe ≈ 212 GB), Docker (Home Assistant, Omada-Controller, Portainer, GitLab) |

### 2.2 Bausteine des Prototyps

| Baustein | Umsetzung im Prototyp | Bewertung für vanilla-router |
|---|---|---|
| Interface-Namen | netplan, Umbenennung per MAC auf `wan0`/`lan0` | **übernehmen** (feste, sprechende Namen) |
| IPv4-Firewall + NAT | eigene nft-Tabelle `ip router_v4` (Filter input/forward, Masquerade), geladen von `router-v4-firewall.service` früh beim Booten (`Before=network-pre.target`) | **übernehmen** |
| IPv6-Firewall | eigene nft-Tabelle `ip6 router_v6`, ICMPv6 nach RFC 4890, DHCPv6-Client-Antworten erlaubt | **übernehmen**, um Forwarding für ein LAN-Präfix erweitern |
| Portweiterleitungen | eigene Tabelle `ip routerui_portfw` (DNAT), Freigabe in router_v4 über `ct status dnat accept` | **übernehmen** |
| Gerätesperren | eigene Tabelle `inet routerui_block` (forward, Priorität −10), Zeitsperren mit `meta hour/day`, Zeitzone über `TZ` beim Laden | **übernehmen** |
| DNS | dnsmasq, lokale Namen unter `home.arpa` (RFC 8375) per `host-record` (liefert auch PTR) | **übernehmen** |
| DHCP | isc-dhcp-server mit verwalteten Include-Dateien, dynamische Bereiche werden automatisch um Reservierungen herum aufgeteilt | **ersetzen** durch dnsmasq-DHCP (isc-dhcp ist abgekündigt und liegt nur in universe), siehe D-05 |
| Weboberfläche | `routerui`: Go (nur Standardbibliothek), drei Prozesse mit getrennten Rechten (siehe 2.3) | **übernehmen und ausbauen**, wird zum Kern des Projekts |
| Sicherheits-Audit | Bash-Skript, das nur liest (≈ 30 Prüfungen), Vergleich mit dem letzten Lauf | **übernehmen** als eingebaute Selbstprüfung |
| Konfigurations-Backup | Bash-Skript (`/etc`, Crontabs, Leases, Docker-Daten, Metadaten) | **übernehmen**, später als Export in der Oberfläche |
| Updates | unattended-upgrades (nur Sicherheit), kein automatischer Neustart, keine Snapshots | **ausbauen**: Snapshots und Rollback (F-UPD) |

### 2.3 routerui: Architektur des Prototyps

```
Browser ──HTTPS──▶ routerui serve        (Benutzer routerui, nur CAP_NET_BIND_SERVICE, stark gehärtet)
                     │  Login, Sitzungen, CSRF, statisches Frontend (go:embed)
                     ├─Unix-Socket──▶ routerui helper   (root, nur feste Aktionen, SO_PEERCRED)
                     │                 erzeugt/prüft/spielt Konfiguration ein, Firewall, Dienste, Updates
                     └─Unix-Socket──▶ routerui files    (Benutzer der NAS-Freigabe, kein Netz, keine Caps)
                                       Datei-Browser, Pfade nur über os.Root
```

- **Wahrheitsquelle** ist `state.json` des Helpers (Reservierungen, DNS-Namen, Portweiterleitungen, Sperren).
  Paket `conf` enthält Prüfregeln und Generatoren, es liest und schreibt selbst nichts.
- **Ablauf jeder Änderung:** ganzen Zustand prüfen → Dateien erzeugen → mit dem Werkzeug des Dienstes
  prüfen → atomar ersetzen → neu laden → Gesundheit prüfen → bei Fehler alte Version zurück.
  Firewall-Änderungen sind sofort aktiv und werden nach 60 s ohne Bestätigung zurückgenommen.
- **Testinstanz:** gleiche Binärdatei mit `--dev` auf `127.0.0.1:9443`, Helper im Probelauf (schreibt
  nur unter einem Test-Wurzelordner, startet nichts neu), **gleiche systemd-Härtung** wie die echte.
- **Sicherheit:**
  - Passwort als PBKDF2-SHA256-Hash (600k Runden), Einrichtung nur mit einmaligem Einrichtungscode
  - Sitzungen nur im Speicher, Ende nach 30 min ohne Aktivität, höchstens 12 h
  - Login-Bremse und fail2ban-Jail
  - strenge CSP, DOM nur über eine `h()`-Funktion (kein `innerHTML`)
  - Downloads immer als Anhang mit `CSP: sandbox`
- **Funktionsumfang** (Stand 2026-11-04, Vorbilder FRITZ!Box, OpenWrt LuCI, OPNsense, Pi-hole, UniFi):
  - Übersicht mit Live-Traffic, 24-h-Verlauf und Hinweise-Zentrale, Online-Monitor (natives ICMP, Ausfallprotokoll)
  - Geräte mit Detailseite, Namen und Notizen, „neu“-Erkennung, festen Adressen, Wake-on-LAN, Gerätesperre und Zeitsperre, CSV-Export
  - DNS-Namen und DNS-Statistik, Firewall mit Portweiterleitungen und fail2ban
  - Verbindungen (Conntrack per Netlink), Netzwerk-Status (Schnittstellen, Routen, Nachbarn, IPv6, offene Ports)
  - Dienste, Container und Protokolle (System- und Änderungsprotokoll)
  - System mit Updates, NVMe-SMART per ioctl, Audit, Neustart
  - Datei-Browser für die NAS-Freigabe mit Papierkorb, Werkzeuge (Ping, Tracepath, DNS, Port prüfen)
  - Sitzungsverwaltung, Export/Import der Konfiguration, Schnellsuche (Strg+K)

### 2.4 Lehren aus dem Prototyp

Diese Punkte sind beim Härten und Ausbau des Prototyps aufgefallen. Jeder ist eine Anforderung an
vanilla-router (Verweis in der letzten Spalte).

| ID | Lehre | Folge für vanilla-router |
|---|---|---|
| L-01 | Die IPv4-Firewall war dicht, für **IPv6 gab es gar keine Regeln**, und SSH war über die öffentliche IPv6-Adresse erreichbar. | Firewall für beide Protokolle von Anfang an, Selbsttest (F-SEC-01, F-SEC-08) |
| L-02 | Die Firewall wurde per `@reboot`-Cronjob gesetzt: Beim Booten gab es ein Zeitfenster ohne Filter. | Firewall als systemd-Unit vor `network-pre.target` (F-SEC-02) |
| L-03 | Das System lief 10 Monate auf einer **Ubuntu-Version ohne Support**, unattended-upgrades lief also ins Leere. | Nur LTS, Hinweis lange vor dem Supportende, geführtes Release-Upgrade (F-UPD-05) |
| L-04 | AppArmor war abgeschaltet, weil sich ein Container nicht stoppen ließ (Problem mit dem runc-Profil). | Sicherheitsmechanismen nie still abschalten, die Oberfläche zeigt ihren Zustand (F-SEC-06) |
| L-05 | Ein Release-Upgrade brach ab, weil `/etc/timezone` ein **leeres Verzeichnis** war (Überbleibsel eines Docker-Mounts). | Vorab-Prüfung vor Upgrades, Snapshot vorher (F-UPD-02, F-UPD-06) |
| L-06 | Beim Aufräumen hat das Purgen von `grub-pc` die Datei **`/etc/default/grub` mitgelöscht**. | Keine Aufräumaktionen ohne Snapshot, Paketliste definiert und getestet (F-UPD-02) |
| L-07 | `dhcp-host` in dnsmasq wirkte nicht, weil isc-dhcp DHCP machte. Zwei Dienste für DNS und DHCP verwirren. | Ein Dienst für DHCP und DNS (D-05) |
| L-08 | Docker-Container per ipvlan sitzen direkt im LAN und umgehen die Firewall des Hosts. Ein Container mit `docker.sock` entspricht root auf dem Router. | Container-Netze bewusst gestalten, Warnungen in der Oberfläche (F-SEC-09) |
| L-09 | Das Programm `ping` (Datei-Capability `cap_net_raw`) startete unter dem `CapabilityBoundingSet` der Oberfläche nicht. Die Testinstanz war weniger streng gehärtet, deshalb fiel es erst im Betrieb auf. | Testumgebung mit identischer Härtung, keine externen Programme mit Datei-Capabilities aus gehärteten Diensten (NF-05) |
| L-10 | Der Hash von `nft list ruleset` ändert sich ständig (Paketzähler). | Vergleiche immer mit `nft -s` (zustandslos), gilt für Tests und Selbstprüfung |
| L-11 | Ein langsamer Paket-Mirror (≈ 200 KB/s) verzögerte das Upgrade um Stunden. | Mirror-Auswahl bzw. Prüfung der Geschwindigkeit vor großen Downloads (F-UPD-07) |
| L-12 | Ein DHCP-Pool ab `.0` (im größeren Netz zulässig) vergab eine Adresse auf `.0`, die manche Geräte als Netzadresse missverstehen. | Pools nie auf `.0` oder `.255` eines /24 beginnen lassen (F-NET-07) |
| L-13 | Viele Geräte melden zufällige MAC-Adressen (WLAN-Privatsphäre). Reservierungen und Sperren nach MAC greifen dann nicht. | Hinweis in der Oberfläche (umgesetzt), langfristig Gerätekennung über mehr als die MAC (Q-12) |
| L-14 | Die per DHCP verteilte Suchdomain war ein Fantasiename statt der lokalen Domain, deshalb funktionierten kurze Namen nicht. | Suchdomain `home.arpa` als Vorgabe (F-NET-05) |
| L-15 | Ohne Backup lag die gesamte NAS-Ablage auf einer einzigen SSD. | Backup-Funktion für Nebenrollen einplanen (F-ADD-03) |
| L-16 | Bei Sperren über Mitternacht war die Zuordnung der Wochentage zuerst falsch (Morgen gehört zum Folgetag). | Zeitlogik mit Tests absichern (umgesetzt, beibehalten) |
| L-18 | Beim Übernehmen des Prototyp-Codes standen in Testdaten echte Werte aus dem Heimnetz (MAC eines Geräts, WAN-Adresse, Gerätenamen), auch als Bytes und Hex-Werte. | Übernommenen Code vor dem Commit nach Adressen, MACs und Namen durchsuchen, auch in Byte- und Hex-Schreibweise; Testdaten nur mit Dokumentationswerten |
| L-17 | Die Paketprüfung (T-M1-08) fand auf dem deutschsprachigen Prototyp nichts, weil apt „Hängt ab von:“ statt „Depends:“ schreibt. Auf einem System ohne *universe* in den Quellen rutschten universe-Pakete als „unbekannt“ durch. | Ausgaben von Werkzeugen nur mit `LC_ALL=C` auswerten. Prüfungen müssen bei fehlenden Daten fehlschlagen, nicht bestehen |

### 2.5 Was am Prototyp noch Handarbeit ist

Das ist die Liste dessen, was der Installer automatisieren muss:

1. Grundsystem installieren, Partitionierung, Bootloader.
2. Netzwerkkarten zuordnen, `wan0`/`lan0` per MAC benennen (netplan).
3. IP-Forwarding (`sysctl`), LAN-Adresse und Netz festlegen.
4. Firewall-Dateien und Units für IPv4/IPv6 installieren.
5. dnsmasq (DNS) und isc-dhcp (DHCP) konfigurieren, Upstream-DNS wählen.
6. SSH härten, Benutzer anlegen, unattended-upgrades einrichten.
7. routerui bauen und installieren (Units, Benutzer, TLS-Zertifikat, fail2ban-Jail).
8. Optional Samba, Docker usw. für Nebenrollen.

---

## 3. Zielarchitektur

### 3.1 Überblick

```
┌──────────────── Stick-Ersteller (Windows, 3.12) ──────────────────┐
│ eine .exe ohne Installation · lädt und prüft das Abbild · schreibt│
│ den Stick · legt Sprache, Tastatur, Zeitzone aus Windows dazu     │
└──────────────┬────────────────────────────────────────────────────┘
               │ USB-Stick
               ▼
┌───────────────────── Boot-Stick (Live-Image) ─────────────────────┐
│ minimales Linux · Installations-Assistent · Image des Zielsystems │
└──────────────┬────────────────────────────────────────────────────┘
               │ erkennt Hardware, fragt, installiert
               ▼
┌──────────────────────── Zielsystem (Festplatte) ──────────────────┐
│ Ubuntu LTS (unverändert, D-10) + Paket vanilla-router             │
│                                                                   │
│  /etc/vanilla-router/config.json   ◀── einzige Wahrheitsquelle    │
│            │                                                      │
│            ▼  vrctl apply (prüfen → erzeugen → testen → einspielen)│
│  systemd-networkd · nftables · dnsmasq · pppd · timesyncd · u-u   │
│            ▲                                                      │
│  routerui (Weboberfläche) ──── Helper ── liest/schreibt config    │
└───────────────────────────────────────────────────────────────────┘
```

### 3.2 Grundsystem

- **Distribution:** **Ubuntu Server LTS** (zuerst 26.04) mit dem Standard-Paketumfang des Metapakets
  `ubuntu-server` (D-10, D-12, entschieden am 2026-11-04, P-10). Gründe: Der Prototyp läuft darauf,
  Ubuntu ist die bekannteste Distribution, hat die meisten Anleitungen und unterstützt neue Hardware früh
  (HWE-Kernel). Wer Ubuntu Server kennt, findet sich sofort zurecht, jede Anleitung für Ubuntu Server gilt.
  - **Was Ubuntu Server mitbringt und wie damit umgegangen wird** (D-12): Pakete werden nicht entfernt,
    weil `ubuntu-server-minimal` sie als feste Abhängigkeiten hat. Angepasst wird nur per Konfiguration:
    | Bestandteil | Problem für einen Router | Umgang |
    |---|---|---|
    | `systemd-resolved` | Stub-Listener belegt Port 53, den dnsmasq braucht (im Prototyp maskiert) | `DNSStubListener=no` per Drop-in, resolved bleibt aktiv und zeigt auf dnsmasq |
    | `cloud-init` | bei jedem Start Suche nach Cloud-Datenquellen, kann Netzwerk umkonfigurieren | `/etc/cloud/cloud-init.disabled` |
    | `netplan` | zweite Netzwerk-Konfiguration neben networkd | leere Konfiguration, networkd-Dateien kommen von vrctl (D-06) |
    | `snapd` | Dienst ohne Aufgabe, keine Snaps installiert | bleibt installiert, Dienst und Socket deaktiviert (Erweiterungen dürfen ihn einschalten) |
    | `multipath-tools`, `open-iscsi`, `lxd-installer` | unnötige Dienste (Audit-Befund im Prototyp) | Dienste deaktiviert |
    | `chrony` | keins, ist der Zeitdienst von Ubuntu Server | bleibt, später auch NTP-Server fürs Heimnetz |
  - **Sicherheitsupdates nur aus *main*** (Stand 26.04, auf dem Prototyp geprüft [S-15]):
    | Paket | Archiv | Folgerung |
    |---|---|---|
    | `dnsmasq-base` (das Programm) | main | Sicherheitsupdates garantiert |
    | `dnsmasq` (nur Unit und Standardkonfiguration) | universe | **nicht installieren**. Eigene Unit im Paket vanilla-router, die `dnsmasq-base` startet |
    | `nftables`, `ppp`, `btrfs-progs`, `kea-dhcp4-server` | main | in Ordnung |
    | `fail2ban` | universe | **ersetzen** durch eigene Bremse in routerui (gibt es im Prototyp) und nft-Ratenbegrenzung. SSH ist ab Werk aus (F-SEC-04) |
    | `snapper` | universe | Rücknahme mit eigenem kleinen Werkzeug über `btrfs subvolume snapshot` oder Ausnahme mit Begründung (Q-03) |
    | `isc-dhcp-server` | universe, abgekündigt | entfällt (D-05) |
  - Regel für das Paket vanilla-router: **Abhängigkeiten nur aus *main***. Ausnahmen aus *universe* nur
    mit Begründung in einer D-NN und nur für Erweiterungen (F-ADD). Eine CI-Prüfung erzwingt das (T-M1-08).
  - Ubuntu Pro (für Privatleute kostenlos, deckt *universe* ab) wird angeboten, aber **nicht vorausgesetzt**.
  - Es werden keine Snaps installiert, snapd ist aus (siehe Tabelle oben).
  - Support: 5 Jahre Standard-Support pro LTS, alle zwei Jahre eine neue LTS mit geführtem Upgrade (F-UPD-05).
- **Netzwerk-Stack:** systemd-networkd direkt, ohne netplan als Zwischenschicht (siehe D-06). networkd
  kann DHCPv4/v6-Client, Prefix Delegation, Router Advertisements und Bridges.
- **Paketumfang Grundinstallation:** `ubuntu-server` und zusätzlich:
  - Netzwerk: `systemd-networkd`, `nftables`, `dnsmasq-base`, `ppp` (PPPoE über das rp-pppoe-Plugin von pppd)
  - Zeit: `chrony` (kommt mit Ubuntu Server)
  - Sicherheit: `unattended-upgrades`, `apparmor`, `openssh-server` (installiert, aber aus). Kein fail2ban (siehe oben)
  - Werkzeuge: `ethtool`, `iproute2`
  - Snapshots: `btrfs-progs` + Snapshot-Werkzeug (siehe F-UPD)
  - Paket `vanilla-router`: routerui, vrctl, Units
- **Kein** Desktop, kein Docker und kein Samba in der Grundinstallation. Das sind Erweiterungen (F-ADD).

### 3.3 Konfigurationsmodell

Eine Datei `/etc/vanilla-router/config.json` beschreibt den gewünschten Zustand. Werkzeug `vrctl`
(dieselbe Codebasis wie routerui) erzeugt daraus alle Dateien. Skizze des Schemas (wird in M2 genau
festgelegt und versioniert):

```json
{
  "schema": 1,
  "system":   { "hostname": "router", "timezone": "Europe/Berlin", "locale": "de_DE.UTF-8" },
  "wan":      { "interface": "wan0", "match_mac": "aa:bb:…", "type": "dhcp",
                "pppoe": { "user": "…", "password_ref": "secret:pppoe", "vlan": 7 },
                "ipv6": { "mode": "auto", "request_prefix": 56 } },
  "lan":      { "interface": "lan0", "members": ["aa:bb:…", "cc:dd:…"],
                "ipv4": "192.168.42.1/24", "ipv6": { "ula": "fd12:3456:789a::/48", "from_wan_prefix": true } },
  "dhcp":     { "pool": ["192.168.42.100", "192.168.42.249"], "lease": "12h",
                "reservations": [ { "name": "drucker", "mac": "…", "ip": "192.168.42.20" } ] },
  "dns":      { "domain": "home.arpa", "upstreams": ["9.9.9.9", "1.1.1.1"], "records": [] },
  "firewall": { "port_forwards": [], "blocks": [], "ipv6_pinholes": [] },
  "services": { "ssh": { "enabled": false }, "webui": { "listen": "lan" } },
  "updates":  { "security_auto": true, "reboot_window": "04:00-05:00", "snapshots": true }
}
```

- **ULA-Präfix** wird bei der Installation nach RFC 4193 **zufällig** erzeugt (40 Bit Global ID) und
  danach nie mehr geändert. Der Wert im Beispiel oben ist nur ein Platzhalter (F-NET-12).
- **wan.type** ist einer von `dhcp`, `pppoe`, `static`, später `dslite`. Bei `pppoe` ist `vlan` optional.
- **Geheimnisse** (PPPoE-Passwort, Hash des UI-Passworts) liegen nicht in `config.json`, sondern in
  `/etc/vanilla-router/secrets/` (0600). `config.json` verweist nur darauf. So lässt sich die
  Konfiguration gefahrlos exportieren.
- **Erzeugte Dateien** tragen einen Kopf „Von vanilla-router erzeugt, nicht von Hand ändern“ und liegen
  an bekannten Orten (`/etc/systemd/network/10-vr-*.network`, `/etc/nftables.d/vr-*.nft`,
  `/etc/dnsmasq.d/vr.conf`). Handgeschriebene Ergänzungen bleiben möglich, über ausgewiesene Dateien
  ohne Präfix `vr-`.
- **Ablauf von `vrctl apply`** (aus dem Prototyp übernommen):
  1. Schema prüfen und Werte validieren (z. B. Überschneidungen, doppelte IPs).
  2. Alle Dateien in einen temporären Ordner erzeugen.
  3. Mit den Werkzeugen der Dienste prüfen (`nft -c`, `dnsmasq --test`, `networkctl` …).
  4. Atomar ersetzen, alte Fassung aufheben.
  5. Dienste gezielt neu laden.
  6. Gesundheitsprüfung (LAN erreichbar, DNS antwortet, WAN-Verbindung wie vorher).
  7. Bei Fehler alte Fassung zurück, erneut laden, Fehler melden.
  Für Netzwerk- und Firewall-Änderungen gilt zusätzlich die 60-s-Bestätigung.

### 3.4 Netzwerk-Vorgaben

- **Namen:** `wan0` für den Internet-Anschluss. `lan0` ist eine Bridge über alle übrigen Ports, auch
  wenn es nur einer ist, damit spätere Ports ohne Umbau dazukommen.
- **LAN-IPv4:** ein /24 aus `192.168.0.0/16` oder `10.0.0.0/8`, das sich **nicht** mit dem WAN-Netz
  überschneidet. Der Installer wählt es automatisch, nachdem er das WAN-Netz kennt (F-NET-03).
- **DHCP-Pool:** `.100`–`.249` (L-12), Reservierungen bevorzugt unter `.100`.
- **IPv6:** DHCPv6 mit Prefix Delegation auf WAN (Größe erfragen, typischerweise /56 oder /62),
  ein /64 daraus an LAN, Router Advertisements mit SLAAC im LAN. Dazu immer ein ULA-Präfix für stabile
  lokale Adressen. Ohne delegiertes Präfix nur ULA, die Oberfläche erklärt warum (wie im Prototyp).
  systemd-networkd kann das nativ, Voraussetzung ist, dass networkd **beide** Interfaces verwaltet [S-03] [S-04]:

  ```ini
  # 10-vr-wan.network (bei PPPoE: Match auf ppp0, DHCPv6 läuft über die PPP-Verbindung)
  [Match]
  Name=wan0
  [Network]
  DHCP=yes
  IPv6AcceptRA=yes
  [DHCPv6]
  PrefixDelegationHint=::/56
  [DHCPPrefixDelegation]
  UplinkInterface=:self
  SubnetId=0
  Announce=no

  # 10-vr-lan.network
  [Match]
  Name=lan0
  [Network]
  Address=192.168.42.1/24
  DHCPPrefixDelegation=yes
  IPv6SendRA=yes
  [DHCPPrefixDelegation]
  UplinkInterface=wan0
  SubnetId=1
  [IPv6Prefix]          # ULA zusätzlich, unabhängig vom Anbieter
  Prefix=fd12:3456:789a:1::/64
  Assign=yes
  ```
  Ob RA aus networkd oder aus dnsmasq kommt, entscheidet Q-04. Im LAN darf es nur **eine** Quelle für RA geben.
- **DNS:** dnsmasq als Resolver und Cache im LAN, lokale Namen unter `home.arpa`. Upstream nach Wahl
  (Provider, Quad9, Cloudflare, eigene). Auf Wunsch DNS über TLS mit einem schlanken Zusatzdienst (Q-10).
- **Zeit:** NTP-Client, optional NTP-Server fürs LAN.

### 3.5 WAN-Anschlussarten

| Art | Erkennung durch den Installer | Version |
|---|---|---|
| DHCP (Kabel- oder Glasfaser-Router davor, Modem im Bridge-Modus mit DHCP) | DHCPOFFER auf dem erkannten Port | 1 |
| PPPoE (DSL-Modem im Bridge-Modus, Glasfaser-ONT bei vielen Anbietern) | PADI/PADO-Antwort auf dem Port, ohne und mit VLAN-Tag (Telekom: 7) | 1 |
| Statische IP | nur manuell | 1 |
| DS-Lite (nur IPv6 vom Anbieter, IPv4 über Tunnel) | IPv6-Lease ohne IPv4, AFTR per DHCPv6-Option | 2 |
| Doppeltes NAT hinter Provider-Router | WAN-Adresse aus privatem Bereich | 1, Hinweis in der Oberfläche |

**Wichtige Anbieter in Deutschland** (Recherche 2026-11, aus Foren und Hilfeseiten, nicht offiziell bestätigt, im Testlabor prüfen):

| Anbieter / Anschluss | Merkmale | Folge |
|---|---|---|
| Telekom DSL, VDSL, Glasfaser | PPPoE **über VLAN 7**. Benutzername = Anschlusskennung + Zugangsnummer + Mitbenutzernummer (meist `0001`) + `@t-online.de`. Das Glasfaser-Modem ist nur ein Medienkonverter, die Einwahl macht der Router. IPv6 per DHCPv6 über PPP, Präfix /56 [S-05] [S-06] | Eingabemaske in der Weboberfläche (F-UI-08) mit Feldern genau wie im Telekom-Brief, Benutzername wird zusammengesetzt. Der Assistent erkennt nur PPPoE über VLAN 7 |
| Vodafone Kabel | Router oder Station vom Anbieter. Im Bridge-Modus je nach Region und Zeitpunkt echtes Dual-Stack **oder DS-Lite**, das kann sich jederzeit ändern. Bridge-Modus nicht überall verfügbar [S-07] | Erkennen statt annehmen, später DS-Lite (F-NET-09). Häufigster Fall bleibt doppeltes NAT hinter der Station (wie im Prototyp) |
| Glasfaser anderer Anbieter (Deutsche Glasfaser, o2, 1&1, Stadtwerke) | teils DHCP, teils PPPoE, teils mit VLAN-ID je Anbieter | Anbieterliste mit VLAN-IDs in der Weboberfläche (Daten pflegen, Q-14) |

**Technik dahinter:** systemd-networkd kann selbst kein PPPoE. Dafür läuft `pppd` mit dem
`rp-pppoe`-Plugin auf `wan0` bzw. `wan0.7` und erzeugt `ppp0`. networkd verwaltet dann `ppp0` für
DHCPv6-PD. DS-Lite braucht einen ipip6-Tunnel (networkd: `[Tunnel] Kind=ip6tnl Mode=ipip6`) zum AFTR
des Anbieters. Ob networkd die AFTR-Option (DHCPv6 Option 64) auswertet, ist zu prüfen (Q-15).

### 3.6 Firewall-Modell

- Zonen `wan`, `lan`, später `guest` und `vpn`. Standard: Eingehend auf WAN alles verwerfen, LAN darf
  alles. Weitergeleitet wird LAN → WAN, zurück nur bei bestehenden Verbindungen und Portweiterleitungen.
- Eigene nft-Tabellen je Aufgabe, damit Docker und andere Dienste mit ihren eigenen Tabellen daneben
  bestehen können (aus dem Prototyp, L-08):
  - `inet vr_filter`: Filter für IPv4 **und** IPv6 in einer Tabelle (D-09). Der Prototyp hatte getrennte
    Tabellen, und genau dadurch fehlte IPv6 lange unbemerkt (L-01).
  - `ip vr_nat`: Masquerade
  - `ip vr_portfw`: DNAT
  - `inet vr_block`: Sperren
- Jede Tabelle wird mit dem Muster `table … ; delete table … ; table … { … }` atomar ersetzt, niemals
  mit `flush ruleset` (D-03).
- ICMPv6 nach RFC 4890, Ping von außen mit Ratenbegrenzung.
- IPv6-Freigaben („Pinholes“) für einzelne Geräte und Ports, das Gegenstück zur Portweiterleitung.

### 3.7 Updates und Rücknahme

- Sicherheitsupdates automatisch (unattended-upgrades), Neustart nur im Wartungsfenster und nur, wenn nötig.
- **Vor jedem Update ein Snapshot** des Systems. Optionen und Stand der Recherche (Entscheidung in Q-03):
  - *btrfs + snapper*: Snapshot vor und nach jedem apt-Lauf über einen apt-Hook. Ob der Hook eingebaut ist
    oder ein Zusatzpaket braucht, unterscheidet sich je nach Quelle [S-08] [S-09].
  - *grub-btrfs*: Snapshots erscheinen im Bootmenü. Auf Debian-basierten Systemen (also auch Ubuntu) sollen
    sie ohne Anpassung des initramfs aber nicht zuverlässig bootfähig sein [S-10]. Das ist ein Risiko und
    muss getestet werden. In Ubuntu 26.04 gibt es grub-btrfs nicht als Paket, snapper nur in *universe* [S-15].
  - *A/B-Partitionen* (z. B. mit systemd-sysupdate): sehr robust, passt aber schlecht zu „normalem apt“ (P-01).
  - Eine **fertige automatische Gesundheitsprüfung mit Rücknahme gibt es nicht** [S-08]. Sie muss gebaut
    werden. Ansatz: Zählung der Bootversuche (systemd-boot Boot Assessment oder `grubenv`-Zähler) und ein
    Dienst, der den Start erst nach erfolgreicher Prüfung „bestätigt“. Sonst bootet der nächste Start den
    letzten guten Stand.
- **Gesundheitsprüfung nach dem Neustart:** Läuft das LAN, antwortet DNS, kommt WAN hoch? Wenn nicht,
  wird automatisch in den letzten Snapshot zurückgebootet und die Oberfläche zeigt, was passiert ist.
  Das ist ein eigener Forschungsschritt in M5 (Spike vor der Umsetzung).
- Release-Upgrades (neue LTS) werden geführt und **nie automatisch** gestartet. Die Oberfläche kündigt
  sie Monate vor dem Supportende an.

### 3.8 Weboberfläche

- Basis ist routerui aus dem Prototyp (2.3), umgestellt auf `config.json` als Wahrheitsquelle.
- Neuer **Einrichtungsmodus** direkt nach der Installation (F-UI-02, F-UI-08):
  1. Passwort festlegen. Der Einrichtungscode steht auf dem Bildschirm des Routers, als Text und QR-Code.
  2. **Internetzugang**, nur wenn nötig: Hat der Assistent PPPoE erkannt, fragt die Oberfläche nach den
     Zugangsdaten, mit Anbieter-Vorlagen (Telekom-Brief, F-NET-13) und VLAN. Bei DHCP entfällt der Schritt.
     Ohne Antwort vom Modem bietet sie DHCP, PPPoE und statische Adresse zur Wahl an.
  3. Prüfung der Verbindung („Internet erreichbar“) und ein Kurzrundgang.
- Was der Assistent nicht mehr fragt, lässt sich hier jederzeit ändern (F-UI-09): Name des Routers,
  Heimnetz (Adresse, DHCP-Bereich), Zeitzone, Sprache.
- Nebenrollen (NAS, Docker) als optionale Module, nur sichtbar, wenn installiert.

### 3.9 Installer

#### 3.9.1 Boot-Medium

- UEFI-bootfähiges Image für USB-Sticks (später zusätzlich ISO), gebaut reproduzierbar mit **mkosi**
  (siehe Q-02). mkosi baut bootfähige Debian- und Ubuntu-Datenträger-Images, die sich direkt auf einen
  Stick schreiben lassen [S-11]. Einen fertigen „Installer“-Typ hat mkosi nicht, der Assistent ist also
  eine Anwendung im Live-Image, die das zweite Image (Zielsystem) auf die Festplatte schreibt. Enthält:
  - ein minimales Live-System (Kernel, Firmware für gängige Netzwerkkarten, Treiber),
  - den Assistenten (Go-Binary, Textoberfläche auf der Konsole),
  - ein fertiges **Image des Zielsystems** (komprimiert), damit die Installation offline und
    deterministisch ist (P-07, D-07).
- Der Assistent startet automatisch auf `tty1`, keine Anmeldung nötig.
- Geschrieben wird der Stick vom Stick-Ersteller (3.12). Andere Schreibprogramme (Rufus, Etcher, `dd`)
  funktionieren auch, dann fehlen nur die Vorgaben aus Windows und der Assistent fragt nach der Sprache.
- Im Abbild liegt auf der EFI-Partition eine reservierte Datei `vr/stick.json` fester Größe (aufgefüllt
  mit Leerzeichen). Der Stick-Ersteller trägt dort beim Schreiben Sprache, Tastaturbelegung und Zeitzone
  aus Windows ein. Die Lage der Datei im Abbild steht im Manifest des Abbilds (3.12), der Ersteller
  muss also kein FAT verstehen.

#### 3.9.2 Ablauf des Assistenten

Geändert in 0.8 (P-11, D-15): Der Assistent fragt nur noch nach Physischem. Die Schrittnamen bleiben
stehen, damit Verweise stimmen.

| Schritt | Was passiert | Laien-Text (Beispiel) | Stand 0.8 |
|---|---|---|---|
| S1 Sprache | Sprache, Tastatur und Zeitzone aus `vr/stick.json` (3.9.1) | „Willkommen! Wählen Sie Ihre Sprache.“ | **nur ohne `stick.json`** (Stick mit fremdem Programm geschrieben), Zeitzone dann aus der Sprache |
| S2 Hardware-Check | Netzwerkkarten zählen (≥ 2 nötig), Speicher, Datenträger | „Dieser Rechner hat nur einen Netzwerkanschluss. Für einen Router braucht es mindestens zwei, zum Beispiel mit einem USB-Netzwerkadapter.“ | **nur bei einem Problem** sichtbar, sonst still |
| S3 WAN erkennen | alle Ports aktivieren, auf Link warten (siehe 3.9.3) | „Ziehen Sie alle Netzwerkkabel ab. Stecken Sie jetzt **nur** das Kabel vom Modem oder Internet-Router ein.“ | bleibt |
| S4 Internetzugang | DHCP und PPPoE (ohne und mit VLAN 7) erkennen, Ergebnis in `config.json` vermerken | „Ihr Anschluss braucht Zugangsdaten. Die geben Sie gleich bequem im Browser ein.“ | **keine Eingabe mehr**, nur Anzeige. Zugangsdaten in der Oberfläche (F-UI-08) |
| S5 LAN | übrige Ports werden Heimnetz, LAN-Netz automatisch | – | **entfällt** als eigener Schritt, steht in der Zusammenfassung |
| S6 Datenträger | Ziel auswählen, deutliche Warnung | „**Alle Daten auf dieser Festplatte werden gelöscht.**“ | bleibt, auch bei nur einem Datenträger (bewusste Bestätigung) |
| S7 Name und Zeitzone | Vorgaben | – | **entfällt**, Vorgabe `router.home.arpa`, änderbar in der Oberfläche (F-UI-09) |
| S8 Installation | Image schreiben, Konfiguration erzeugen, Bootloader | Fortschrittsbalken | bleibt |
| S9 Zusammenfassung | Einrichtungscode, Adresse der Oberfläche, QR-Code | „Verbinden Sie ein Gerät mit dem Heimnetz und öffnen Sie https://router.home.arpa, Code: XXXX-XXXX“ | bleibt, dazu F-INST-17 |
| S10 Neustart | Stick abziehen, Neustart | „Ziehen Sie den Stick ab und drücken Sie Enter.“ | bleibt |

Damit sieht ein Laie im Normalfall genau drei Bildschirme mit einer Handlung: Kabel einstecken (S3),
Festplatte bestätigen (S6), Stick abziehen (S10).

Grundsätze für den Assistenten:
- Bedienung nur mit Pfeiltasten und Enter, keine Texteingabe (P-11, F-INST-16).
- Jeder Schritt lässt sich wiederholen.
- Vor S8 wird nichts auf die Festplatte geschrieben.
- Das Ergebnis landet in einer `config.json`. Eine vorbereitete `config.json` für eine unbeaufsichtigte
  Installation gibt es nur noch für automatische Tests (F-INST-08), nicht im Stick-Ersteller.

#### 3.9.3 Erkennung des WAN-Ports (Kernstück)

1. **Ports finden:** `/sys/class/net/*` mit Typ Ethernet (`type == 1`) und einem echten Gerät
   (`device`-Link, nicht virtuell). WLAN (`wireless/`) wird ausgeschlossen. Zu jedem Port werden MAC,
   Treiber, Bus (PCI/USB) und die maximale Geschwindigkeit festgehalten.
2. **Alle Ports aktivieren** (`ip link set up`), ohne Adressen.
3. **Grundzustand:** Ports, die schon Link haben, werden gemerkt. Falls es welche gibt, wird der Nutzer
   gebeten, alle Kabel abzuziehen, bis kein Port mehr Link hat.
4. **Auf das Ereignis warten:** Über Netlink (`RTMGRP_LINK`) auf `IFF_LOWER_UP` bzw. Carrier warten.
   Der erste Port mit Link wird Kandidat.
5. **Bestätigen:** Auf dem Kandidaten DHCP (und parallel PPPoE-Discovery) versuchen. Mit Antwort ist der
   Port sicher WAN. Ohne Antwort fragt der Assistent nach („Ist das wirklich das Kabel vom Modem?“).
6. **Mehrdeutigkeit:** Bekommen zwei Ports gleichzeitig Link, fragt der Assistent erneut. Als Hilfe
   lässt er die LED des Ports blinken (`ethtool --identify`), sofern der Treiber das kann.
7. **Rückfall:** manuelle Auswahl aus einer Liste mit Treiber, MAC und Position (PCI-Pfad).

#### 3.9.4 Datenträger und Boot

- **GPT** mit zwei Partitionen:
  - EFI-Systempartition 1 GiB (vfat)
  - Rest als btrfs mit Subvolumes `@` (`/`), `@log` (`/var/log`, nicht im Snapshot, damit Protokolle
    eine Rücknahme überleben), `@snapshots` und optional `@data` (für das NAS-Modul, eigene Snapshot-Regeln)
- **Kein Swap-Partition**, stattdessen zram (wenig RAM-Bedarf, schont die SSD).
- **Bootloader:** GRUB mit signiertem shim (Ubuntu-Standard, funktioniert mit Secure Boot) oder
  systemd-boot (einfacher, Boot Assessment eingebaut). Hängt an Q-03 und Q-16.
- **Kleinste Datenträger:** 16 GB (NF-01). Ab 64 GB bietet der Assistent an, den Rest für Daten zu nutzen.

#### 3.9.5 Wiederherstellung für Laien

Der Stick ist auch das Rettungswerkzeug. Startet man ihn auf einem bereits eingerichteten Router,
erkennt er die Installation und bietet an:

| Aktion | Wirkung |
|---|---|
| Passwort der Oberfläche zurücksetzen | Neuer Einrichtungscode, alle Einstellungen bleiben |
| Letzten funktionierenden Stand starten | Rücknahme auf den letzten Snapshot, der die Gesundheitsprüfung bestanden hat |
| Diagnose auf den Stick schreiben | Protokolle, `config.json` ohne Geheimnisse, Hardware-Liste, für Hilfe im Forum |
| Neu installieren, Einstellungen behalten | Zielsystem neu schreiben, `config.json` und Geheimnisse übernehmen |
| Auf Werkszustand zurücksetzen | Alles löschen, Assistent von vorn |

Siehe F-INST-12 bis F-INST-14.

#### 3.9.6 Was der Assistent nicht macht

- Er fragt nicht nach Dingen, die sich erkennen lassen (Anschlussart, LAN-Netz, Zeitzone aus Windows
  oder der Sprache).
- Er fragt nicht nach Zugangsdaten, Namen, Netzen oder Passwörtern. Das alles kommt in die
  Weboberfläche, wo man bequem tippen und Hilfe lesen kann (P-11).
- Er verlangt keine Linux-Begriffe. „WAN“ heißt im Text „Internet-Anschluss“, „LAN“ heißt „Heimnetz“.
- Er schreibt nichts auf die Festplatte, bevor S8 bestätigt ist.

### 3.10 Erweiterungen (Nebenrollen)

Optionale Module, nachinstallierbar über die Oberfläche: NAS (Samba mit Datei-Browser, Papierkorb,
Backup), Container (Docker oder Podman), VPN-Server (WireGuard), DNS-Filter (Blocklisten),
Gastnetz (VLAN oder eigener Port). Module dürfen die Router-Funktion nicht gefährden: eigene Units,
eigene Firewall-Tabellen, Ressourcengrenzen per systemd.

### 3.11 Übernahme des Prototyp-Codes

Der Code von routerui wird in M2 in dieses Repo übernommen. Dabei ändert sich:

| Bereich im Prototyp | Was sich ändert |
|---|---|
| `internal/conf` (State, Prüfregeln, Generatoren) | Wird zum Schema für `config.json` erweitert (WAN, LAN, IPv6, DHCP-Pool, Updates). Neue Generatoren für systemd-networkd und dnsmasq-DHCP. Die Generatoren für isc-dhcp entfallen. |
| Festes `10.0.0.0/8`, `10.0.0.1`, `wan0`/`lan0` im Code | Kommen aus `config.json`, keine Konstanten mehr |
| `internal/helper` (feste Aktionen, Probelauf) | Bleibt. Aktionen für WAN-Einstellungen und Netzwerk-Rücknahme kommen dazu (F-UI-03). |
| Firewall-Dateien und Units (`router-v4/v6-firewall`) | Werden aus der Konfiguration erzeugt (`inet vr_filter`, D-09), eine Unit `vr-firewall.service` |
| `internal/files`, Samba-Status | Wandert ins NAS-Modul (F-ADD-01) |
| Docker-Ansichten | Wandern ins Container-Modul (F-ADD-02) |
| Audit-Skript (Bash) | Wird in Go umgesetzt, damit Prüfungen und Oberfläche dieselben Daten nutzen (F-SEC-08) |
| Texte der Oberfläche (deutsch, fest im Code) | Werden in eine Übersetzungstabelle verschoben (F-UI-05) |
| Daten und Namen aus dem Haushalt des Gründers | Werden entfernt, Beispiele nur mit Dokumentations-Adressen (RFC 5737, RFC 3849) |

### 3.12 Stick-Ersteller für Windows

Neu in 0.8 (D-14, D-15). Ein eigenes Programm statt eines fremden Schreibprogramms, damit Laien nichts
über Abbilder, Prüfsummen oder Laufwerke wissen müssen.

**Aus Sicht des Nutzers:**

1. `vanilla-router-stick.exe` herunterladen und starten. Es gibt keine Installation. Windows fragt einmal
   nach Administratorrechten, weil das Programm direkt auf den Stick schreibt.
2. Ein Fenster mit einer Liste der eingesteckten USB-Sticks (Name, Größe). Ein Stick ist vorausgewählt,
   wenn es nur einen gibt.
3. „Stick erstellen“ → Warnung „Alle Daten auf *SanDisk Ultra (32 GB)* werden gelöscht“ → Fortschritt
   (Herunterladen, Prüfen, Schreiben, Kontrolle) → „Fertig. Ziehen Sie den Stick ab und stecken Sie ihn in
   den Rechner, der Ihr Router werden soll.“ Dazu ein Hinweis, wie man am Rechner vom USB-Stick startet
   (Boot-Menü, typische Tasten F12, F11, Esc).

**Technik:**

- **Go, nur Standardbibliothek**, gebaut mit `GOOS=windows` (D-01, NF-03). Das Fenster nutzt die
  Win32-Steuerelemente über `syscall` (Fenster, Liste, Knopf, Fortschrittsbalken), ohne WebView und ohne
  fremde Bibliotheken. Eine einzige `.exe` mit eingebettetem Manifest (`requireAdministrator`,
  DPI-Bewusstsein, Common Controls 6).
- **Nur Wechseldatenträger:** Datenträger mit Bustyp USB (`IOCTL_STORAGE_QUERY_PROPERTY`), nie das
  Systemlaufwerk, Datenträger über 256 GB nur nach zusätzlicher Bestätigung. Vor dem Schreiben werden alle
  Volumes des Sticks gesperrt und ausgehängt (`FSCTL_LOCK_VOLUME`, `FSCTL_DISMOUNT_VOLUME`).
- **Abbild:** Das Programm bleibt klein und lädt das aktuelle Abbild (ca. 1,5–2 GB) über HTTPS. Es
  prüft ein **signiertes Manifest** (Ed25519, öffentlicher Schlüssel im Programm) mit Version, Größe,
  SHA-256 und Lage von `vr/stick.json`. Das Abbild wird im Benutzerordner zwischengespeichert. Alternativ
  lässt sich eine heruntergeladene Abbild-Datei auswählen (offline), auch sie muss zum Manifest passen.
- **Vorgaben aus Windows:** Sprache (`GetUserDefaultLocaleName`), Tastaturbelegung und Zeitzone
  (Windows-Name → IANA über die CLDR-Tabelle `windowsZones`) landen in `vr/stick.json` (3.9.1). Das ist
  keine Konfiguration für den Nutzer, nur eine bessere Vorgabe.
- **Kontrolle:** Nach dem Schreiben wird der Stick zurückgelesen und gegen die SHA-256 geprüft.
- **Kein Hintergrunddienst**, keine Registry-Einträge außer dem, was Windows selbst anlegt, keine
  Telemetrie (P-09). Der Download geht nur an die dokumentierte Adresse des Projekts.

**Umgesetzt in 0.11** (T-M4-08): Manifest holen → Größe prüfen → Warnung (bei mehr als 256 GB eine zweite) → herunterladen →
Abbild vollständig prüfen → Stick sperren → Anfang und Ende löschen → schreiben, das erste MiB (Partitionstabelle)
zuletzt, damit Windows währenddessen nichts einhängt → zurücklesen. Ohne eingebaute Download-Adresse bleibt nur die
lokale Datei mit `manifest.json` und `manifest.json.sig` daneben. Manifeste erzeugt `cmd/vr-sign`.

**Offen:** Code-Signierung gegen die SmartScreen-Warnung (Q-17), Fassungen für macOS und Linux (Q-18),
Ort und Schlüsselverwaltung der Downloads (Q-19).

---

## 4. Anforderungen

Spalten: ID · Priorität · Anforderung · Status. „Prototyp“ bedeutet: im Prototyp vorhanden, muss übernommen werden.

### 4.1 Installer (F-INST)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| F-INST-01 | MUSS | UEFI-Boot von USB-Stick auf x86-64, Start des Assistenten ohne Eingabe | offen |
| F-INST-02 | MUSS | Erkennung aller kabelgebundenen Netzwerkports inkl. USB-Adapter, Abbruch mit klarer Meldung bei < 2 | offen |
| F-INST-03 | MUSS | WAN-Erkennung durch Einstecken des Kabels (3.9.3) mit Bestätigung per DHCP/PPPoE | offen |
| F-INST-04 | MUSS | PPPoE mit optionalem VLAN, Zugangsdaten-Eingabe mit Hilfe zum Anbieter-Brief | verworfen am 2026-11-04 (P-11): Eingabe wandert in die Oberfläche (F-UI-08), der Assistent erkennt nur (F-INST-15) |
| F-INST-05 | MUSS | Auswahl des Ziel-Datenträgers mit Größe und Modell, doppelte Bestätigung vor dem Löschen | offen |
| F-INST-06 | MUSS | Offline-Installation aus einem Image auf dem Stick | offen |
| F-INST-07 | MUSS | Einrichtungscode und Adresse der Oberfläche am Ende anzeigen (Text und QR) | offen |
| F-INST-08 | SOLL | Unbeaufsichtigte Installation mit vorbereiteter `config.json` auf dem Stick. Seit 0.8 nur für automatische Tests, nicht im Stick-Ersteller (D-15) | offen |
| F-INST-09 | SOLL | Port-Identifikation per LED-Blinken (`ethtool --identify`) | offen |
| F-INST-10 | SOLL | Deutsch und Englisch, Vorgabe aus `vr/stick.json` (3.9.1) | offen |
| F-INST-11 | KANN | Installation auf vorhandenes Debian/Ubuntu („Umbau“-Modus) | offen |
| F-INST-12 | MUSS | Stick erkennt eine vorhandene Installation und bietet Rettungsaktionen an (3.9.5) | offen |
| F-INST-13 | MUSS | Passwort der Oberfläche vom Stick aus zurücksetzen | offen |
| F-INST-14 | SOLL | Diagnose-Bericht auf den Stick schreiben (ohne Geheimnisse) | offen |
| F-INST-15 | MUSS | Anschlussart erkennen (DHCP, PPPoE ohne und mit VLAN, keine Antwort) und in `config.json` vermerken (`wan.detected`), damit die Oberfläche weiß, ob sie nach Zugangsdaten fragen muss | erledigt (T-M4-05) |
| F-INST-16 | MUSS | Bedienung nur mit Pfeiltasten und Enter, keine Texteingabe (P-11) | erledigt (T-M4-05) |
| F-INST-17 | MUSS | Solange die Oberfläche nicht eingerichtet ist, zeigt der Router nach jedem Start Adresse und Einrichtungscode auf dem Bildschirm (Text und QR) | Text erledigt (T-M4-06), QR offen |
| F-INST-18 | MUSS | Schritte ohne Problem werden nicht angezeigt (S2), im Normalfall nur S3, S6, S10 mit einer Handlung (3.9.2) | erledigt (T-M4-05) |

### 4.2 Netzwerk (F-NET)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| F-NET-01 | MUSS | Feste Interface-Namen `wan0`/`lan0` per MAC, LAN als Bridge über alle übrigen Ports | offen |
| F-NET-02 | MUSS | IPv4-NAT (Masquerade) und Forwarding LAN → WAN | Prototyp |
| F-NET-03 | MUSS | LAN-Netz automatisch ohne Überschneidung mit dem WAN-Netz | offen |
| F-NET-04 | MUSS | DHCPv4-Server mit Reservierungen, Pools automatisch um Reservierungen herum | Prototyp |
| F-NET-05 | MUSS | Lokaler DNS mit Namen unter `home.arpa` (vorwärts und rückwärts), Suchdomain `home.arpa` | Prototyp (teilweise) |
| F-NET-06 | MUSS | IPv6: DHCPv6-PD auf WAN, RA/SLAAC im LAN, ULA zusätzlich | offen |
| F-NET-07 | MUSS | Pools beginnen nie auf Netz- oder Broadcastadresse (L-12) | offen |
| F-NET-08 | SOLL | Erkennung von doppeltem NAT und Hinweis zu Portweiterleitungen | Prototyp |
| F-NET-09 | SOLL | DS-Lite | offen |
| F-NET-10 | SOLL | Gastnetz (eigener Port oder VLAN), isoliert vom LAN | offen |
| F-NET-11 | KANN | Mehrere WANs (Failover) | offen |
| F-NET-12 | MUSS | ULA-Präfix bei der Installation zufällig nach RFC 4193, danach stabil | offen |
| F-NET-13 | MUSS | PPPoE mit Anbieter-Vorlagen (Telekom: VLAN 7, Benutzername aus den Feldern des Briefs zusammengesetzt) | offen |

### 4.3 Sicherheit (F-SEC)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| F-SEC-01 | MUSS | Firewall IPv4 und IPv6, eingehend auf WAN standardmäßig verwerfen | Prototyp |
| F-SEC-02 | MUSS | Firewall wird vor dem Netzwerk geladen (`Before=network-pre.target`) | Prototyp |
| F-SEC-03 | MUSS | Weboberfläche nur aus dem LAN, HTTPS, Einrichtungscode, Login-Bremse (im Prototyp zusätzlich fail2ban, im Ziel nft-Ratenbegrenzung statt fail2ban, D-10) | Prototyp |
| F-SEC-04 | MUSS | SSH standardmäßig aus. Wenn an, dann nur mit Schlüssel und nur aus dem LAN | Prototyp (teilweise) |
| F-SEC-05 | MUSS | Automatische Sicherheitsupdates | Prototyp |
| F-SEC-06 | MUSS | AppArmor an, Zustand in der Oberfläche sichtbar (L-04) | Prototyp (Anzeige im Audit) |
| F-SEC-07 | MUSS | Privilegientrennung der Oberfläche (Webserver ohne root, Helper mit fester Aktionsliste) | Prototyp |
| F-SEC-08 | SOLL | Eingebaute Selbstprüfung (Audit) mit Vergleich zum letzten Lauf | Prototyp |
| F-SEC-09 | SOLL | Warnung bei riskanten Erweiterungen (Container mit `docker.sock`, privilegiert, Netz im LAN) | Prototyp |
| F-SEC-10 | SOLL | TLS-Zertifikat, dem Geräte im LAN leicht vertrauen können (Q-09) | offen |
| F-SEC-11 | KANN | DNS über TLS zum Upstream | offen |

### 4.4 Weboberfläche (F-UI)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| F-UI-01 | MUSS | Alle Funktionen aus 2.3 auf Basis von `config.json` | Prototyp (auf state.json) |
| F-UI-02 | MUSS | Einrichtungsmodus nach der Installation | offen |
| F-UI-03 | MUSS | WAN-Einstellungen (Art, Zugangsdaten, IPv6) über die Oberfläche ändern, mit Rücknahme bei Verbindungsverlust | erledigt (T-M4-03), Hardware-Test offen |
| F-UI-04 | SOLL | Mobilgerätetauglich | Prototyp (Grundlagen) |
| F-UI-05 | SOLL | Englische Übersetzung | offen |
| F-UI-06 | KANN | Benachrichtigungen (E-Mail, Push) bei Ausfällen, neuen Geräten, Updates | offen |
| F-UI-07 | MUSS | Seite „Über“ mit Version, Lizenz (AGPL-3.0-or-later) und Link zum Quelltext genau dieser Version (AGPL §13, D-11). Der Link ist ein Build-Parameter, damit veränderte Fassungen auf ihren eigenen Quelltext zeigen | erledigt |
| F-UI-08 | MUSS | Einrichtungsschritt „Internetzugang“ nach der ersten Anmeldung, wenn der Assistent PPPoE oder keine Antwort erkannt hat: Zugangsdaten mit Anbieter-Vorlagen (F-NET-13), VLAN, Prüfung der Verbindung (3.8) | erledigt (T-M4-04) |
| F-UI-09 | MUSS | Name des Routers, Heimnetz (Adresse, DHCP-Bereich), Zeitzone und Sprache in der Oberfläche ändern, mit Rücknahme bei Verbindungsverlust (wie F-UI-03) | erledigt bis auf die Sprache (F-UI-05) |

### 4.5 Updates (F-UPD)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| F-UPD-01 | MUSS | Sicherheitsupdates automatisch, Neustart nur im Wartungsfenster | Prototyp (ohne Neustart) |
| F-UPD-02 | MUSS | Snapshot vor jedem Update und jeder Paketänderung (L-05, L-06) | offen |
| F-UPD-03 | MUSS | Automatische Rücknahme, wenn das System nach einem Update nicht gesund hochkommt | offen |
| F-UPD-04 | MUSS | Updates für vanilla-router selbst (eigene Paketquelle, signiert) | offen |
| F-UPD-05 | MUSS | Ankündigung des Supportendes und geführtes Release-Upgrade (L-03) | offen |
| F-UPD-06 | SOLL | Vorab-Prüfung vor Upgrades (Platz, halbe Pakete, bekannte Stolperfallen wie L-05) | offen |
| F-UPD-07 | SOLL | Mirror mit guter Geschwindigkeit wählen (L-11) | offen |

### 4.6 Erweiterungen (F-ADD)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| F-ADD-01 | SOLL | NAS-Modul (Samba, Datei-Browser, Papierkorb) | Prototyp |
| F-ADD-02 | SOLL | Container-Modul mit Warnungen (F-SEC-09) | Prototyp (Anzeige) |
| F-ADD-03 | SOLL | Backup-Modul (versioniert, z. B. restic, auf USB oder in die Cloud) | offen |
| F-ADD-04 | SOLL | VPN-Server (WireGuard) mit QR-Codes für Handys | offen |
| F-ADD-05 | KANN | DNS-Filter mit Blocklisten | offen |

### 4.7 Stick-Ersteller (F-STICK)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| F-STICK-01 | MUSS | Eine `.exe` für Windows 10 und 11 (x86-64), ohne Installation, ohne Laufzeitumgebung, Go nur mit Standardbibliothek (3.12) | erledigt (T-M4-08) |
| F-STICK-02 | MUSS | Keine Fragen außer der Auswahl des Sticks und der Bestätigung vor dem Löschen | erledigt (T-M4-08) |
| F-STICK-03 | MUSS | Nur USB-Wechseldatenträger anzeigen, nie das Systemlaufwerk; Datenträger über 256 GB nur nach zusätzlicher Bestätigung | erledigt, dazu der Datenträger, von dem das Programm läuft (T-M4-08) |
| F-STICK-04 | MUSS | Abbild herunterladen und vor dem Schreiben gegen ein signiertes Manifest prüfen (Ed25519, SHA-256) | erledigt (T-M4-08), Download-Adresse und Schlüssel offen (Q-19) |
| F-STICK-05 | MUSS | Volumes sperren und aushängen, Abbild roh schreiben, danach zurücklesen und prüfen | erledigt (T-M4-08), mit echtem Stick geprüft (Test-Abbild, 2026-11-05) |
| F-STICK-06 | MUSS | Klare Meldungen bei Fehlern (Stick zu klein, schreibgeschützt, abgezogen, Download abgebrochen), Abbruch jederzeit | erledigt (T-M4-08) |
| F-STICK-07 | SOLL | Sprache, Tastaturbelegung und Zeitzone aus Windows in `vr/stick.json` eintragen | erledigt (T-M4-08) |
| F-STICK-08 | SOLL | Abbild zwischenspeichern, neueres Abbild erkennen; alternativ lokale Abbild-Datei wählen (offline) | erledigt (T-M4-08) |
| F-STICK-09 | SOLL | Deutsch und Englisch nach Windows-Sprache | erledigt (T-M4-08) |
| F-STICK-10 | SOLL | Hinweis am Ende, wie man vom USB-Stick startet (Boot-Menü, typische Tasten) | erledigt (T-M4-08) |
| F-STICK-11 | KANN | Fassungen für macOS und Linux (Q-18) | offen |

### 4.8 Nicht funktional (NF)

| ID | Prio | Anforderung | Status |
|---|---|---|---|
| NF-01 | MUSS | Läuft auf x86-64 mit ≥ 2 GB RAM, ≥ 16 GB Datenträger, ≥ 2 Ethernet-Ports | offen |
| NF-02 | MUSS | Routing mit mindestens 1 Gbit/s auf einem aktuellen Einsteiger-x86 (z. B. Intel N100) | offen |
| NF-03 | MUSS | Eigener Code nur mit der Go-Standardbibliothek, Frontend ohne Abhängigkeiten (P-08) | Prototyp |
| NF-04 | MUSS | Alle erzeugten Konfigurationen sind durch Tests abgedeckt (Golden Files) | Prototyp (teilweise) |
| NF-05 | MUSS | Testumgebung mit identischer Härtung wie der Betrieb (L-09) | Prototyp |
| NF-06 | SOLL | Installation in unter 10 Minuten | offen |
| NF-07 | SOLL | Leerlauf-Stromverbrauch wird nicht durch vanilla-router erhöht (keine Dauerlast, Polling nur bei offener Oberfläche) | Prototyp |
| NF-08 | MUSS | Jede Quelldatei beginnt mit `SPDX-License-Identifier: AGPL-3.0-or-later` (D-11) | offen |
| NF-09 | MUSS | Paket-Abhängigkeiten des Grundsystems nur aus Ubuntu *main* (D-10), per CI geprüft | offen |

---

## 5. Entscheidungen

| ID | Entscheidung | Begründung | Status |
|---|---|---|---|
| D-01 | Eigener Code in **Go, nur Standardbibliothek**, Frontend in Vanilla-JS ohne Build-Schritt | Ein statisches Binary, keine Lieferkette, lange wartbar. Im Prototyp bewährt (≈ 30 Funktionen ohne eine Abhängigkeit). | erledigt |
| D-02 | **Eine deklarative Datei** als Wahrheitsquelle, alle Dienstkonfigurationen werden erzeugt | Reproduzierbar, testbar, exportierbar. Im Prototyp mit `state.json` bewährt. | erledigt |
| D-03 | **Eigene nft-Tabellen je Aufgabe**, niemals `flush ruleset` | Docker und andere Dienste bringen eigene Tabellen mit. Ein `flush ruleset` zerstört sie. | erledigt |
| D-04 | **Privilegientrennung**: Webserver ohne root, Helper mit fester Aktionsliste, Dateidienst als Nutzer der Freigabe | Eine Lücke im Webserver wird nicht sofort zu root. | erledigt |
| D-05 | **dnsmasq für DNS und DHCPv4** (statt isc-dhcp), RA und DHCPv6 über systemd-networkd oder dnsmasq (Q-04) | isc-dhcp wird seit Ende 2022 nicht mehr gepflegt [S-12]. OPNsense empfiehlt für kleine Netze dnsmasq inkl. DHCPv6 und RA, Kea erst für große Netze mit Hochverfügbarkeit [S-13]. Ein Dienst statt zwei vermeidet Fehler wie L-07. | offen (Vorschlag) |
| D-06 | **systemd-networkd direkt** statt netplan | Eine Schicht weniger, PD/RA/Bridge nativ. netplan bringt auf Servern keinen Vorteil, wenn die Konfiguration ohnehin erzeugt wird. | offen (Vorschlag) |
| D-07 | **Image-basierte Installation** (vorgefertigtes Root-Dateisystem auf dem Stick) statt debootstrap zur Laufzeit | Offline, schnell, für alle Nutzer identisch, testbar. | offen (Vorschlag) |
| D-08 | **Debian stable** als Basis statt Ubuntu LTS | Alle Router-Pakete (dnsmasq, fail2ban, nftables, pppd) sind in *main* und bekommen Sicherheitsupdates. Unter Ubuntu liegen dnsmasq und fail2ban in *universe* [S-01], das ist dasselbe Problem wie bei isc-dhcp im Prototyp. Dazu kein snap und ein kleineres Grundsystem. Nachteil: Der Prototyp läuft auf Ubuntu, Erfahrungen sind aber weitgehend übertragbar. | verworfen am 2026-11-04 zugunsten von D-10 |
| D-09 | **Eine `inet`-Tabelle für den Filter** (IPv4 und IPv6 gemeinsam), NAT getrennt in `ip vr_nat` | Gemeinsame Regeln können nicht auseinanderlaufen. Der Prototyp hatte IPv6 vergessen, weil es eine eigene Tabelle brauchte (L-01). | offen (Vorschlag) |
| D-10 | **Ubuntu LTS** als Basis | Entscheidung des Projektinhabers nach P-10: Ubuntu ist bekannter, hat mehr Anleitungen und bessere Unterstützung neuer Hardware, und der Prototyp läuft schon darauf. Das *universe*-Problem aus D-08 wird gelöst, indem Abhängigkeiten nur aus *main* kommen: `dnsmasq-base` statt `dnsmasq`, keine fail2ban-Pflicht, für snapper entscheidet Q-03 (3.2) [S-15]. | erledigt (2026-11-04) |
| D-11 | Lizenz **AGPL-3.0-or-later** | Entscheidung des Projektinhabers am 2026-11-04: die neueste AGPL-Fassung (Version 3), mit „oder später“ für künftige Fassungen. Die AGPL verlangt zusätzlich zur GPL, dass Nutzer einer veränderten Fassung, die über das Netz bedient wird, den Quelltext bekommen. Für die Weboberfläche bedeutet das F-UI-07. Text in `LICENSE`, jede Quelldatei bekommt eine SPDX-Zeile (NF-08). | erledigt (2026-11-04) |
| D-12 | **Ubuntu Server** mit dem Paketumfang von `ubuntu-server`, nichts entfernen, nur per Konfiguration anpassen (3.2) | Wunsch des Projektinhabers (P-10). Ein System, das genau wie ein normales Ubuntu Server aussieht, ist für Helfer und Anleitungen am einfachsten. Pakete zu entfernen bricht die Metapakete und damit Release-Upgrades. Die Kosten (einige hundert MB, ein paar deaktivierte Dienste) sind auf PC-Hardware unerheblich. | erledigt (2026-11-04) |
| D-13 | **Rücknahme nach Updates ohne snapper**: btrfs-Snapshot vor apt, GRUB-Eintrag aus dem Snapshot, Zustand (`vr_pending`, `vr_trial`) in einer grubenv auf der ESP, Bestätigung durch `vr-health`, Rücknahme wird durch Umbenennen der Subvolumes dauerhaft | snapper liegt in *universe* (D-10), grub-btrfs fehlt in Ubuntu. GRUB kann auf btrfs nicht schreiben, auf FAT schon. Kernel und initrd kommen aus dem Snapshot selbst, passen also immer. Neustart nach Fehler nur bei einem Versuch nach Update, sonst Neustartschleife. | erledigt (Code), Hardware-Test offen |
| D-14 | **Eigener Stick-Ersteller für Windows** in Go (Win32 über `syscall`), lädt ein signiertes Abbild, keine Fremdbibliotheken (3.12) | Fremde Schreibprogramme sind für Laien eine Hürde (welches Abbild, welches Laufwerk, welche Einstellungen). Ein eigenes Programm kann nur USB-Sticks anbieten, das Abbild selbst prüfen und Vorgaben aus Windows mitgeben. Go mit `GOOS=windows` hält das Projekt bei einer Sprache (D-01). Kleine `.exe` plus Download statt einer `.exe` mit 2 GB, damit das Programm nicht bei jeder neuen Version neu geladen werden muss. Win32 statt WebView, weil WebView Fremdcode bräuchte. | offen (Vorschlag) |
| D-15 | **Ablauf aus Sicht des Nutzers** (Abschnitt 1): Stick-Ersteller ohne Fragen, Assistent nur für Physisches (WAN-Port, Festplatte), alles andere in der Weboberfläche (P-11) | Entscheidung des Projektinhabers am 2026-11-04. Im Browser lässt sich bequemer tippen und Hilfe lesen als auf einer Konsole mit unbekannter Tastaturbelegung. Weniger Fragen bedeuten weniger Fehler. Folge: PPPoE-Eingabe wandert aus dem Assistenten in die Oberfläche (F-INST-04 verworfen, F-UI-08), F-UI-03 und T-M4-03 sind Voraussetzung für die erste Version. | erledigt (2026-11-04) |

---

## 6. Offene Fragen

| ID | Frage | Optionen | Tendenz |
|---|---|---|---|
| Q-01 | Basis-Distribution | Debian stable · Ubuntu LTS | **erledigt: Ubuntu LTS** (D-10, 2026-11-04) |
| Q-02 | Werkzeug für das Image | mkosi · live-build · debos | mkosi (reproduzierbar, Debian und Ubuntu, systemd-nah) |
| Q-03 | Rücknahme von Updates | btrfs + snapper + grub-btrfs · A/B-Partitionen · systemd-sysupdate | **umgesetzt: eigenes Werkzeug** (btrfs-Snapshot + GRUB-Zustand auf der ESP, D-13), Hardware-Test offen. Frühere Einschätzung: btrfs-Snapshots passen am besten zu P-01. snapper liegt unter Ubuntu in *universe*, grub-btrfs fehlt ganz [S-15]: entweder kleines eigenes Werkzeug (`btrfs subvolume snapshot` + apt-Hook) oder snapper als begründete Ausnahme. Bootfähigkeit von Snapshots und die automatische Rücknahme sind ungeklärt (3.7), deshalb Spike in M5. |
| Q-04 | Wer macht IPv6 im LAN (RA, DHCPv6)? | systemd-networkd · dnsmasq | **umgesetzt: systemd-networkd** (RA, SLAAC, PD, eine Quelle), dnsmasq nur DHCPv4 + DNS. Bestätigung durch den Projektinhaber offen |
| Q-05 | Assistent als Textoberfläche auf dem Bildschirm oder als Weboberfläche vom Handy aus? | TUI auf tty1 · Web über einen LAN-Port · beides | **TUI auf tty1**, seit D-15 nur noch drei Bildschirme ohne Texteingabe. Alles mit Eingaben läuft ohnehin über die Weboberfläche |
| Q-06 | WLAN-Access-Point im Router? | nein (externe APs) · hostapd für unterstützte Karten | nein in Version 1 |
| Q-07 | Sprache des Projekts (Code, Doku, Oberfläche) | Deutsch · Englisch · beides | offen. Oberfläche zweisprachig, Doku eventuell englisch für mehr Mitwirkende. Festgelegt (2026-11-05): alles auf GitHub (Commits, Issues, Releases) sowie `README.md` und `SECURITY.md` englisch |
| Q-08 | ARM (Raspberry Pi 5 u. ä.)? | später · nie | später |
| Q-09 | TLS für die Oberfläche ohne Warnung | selbstsigniert + Anleitung · eigene lokale CA mit Installationshilfe · nur HTTP im LAN | offen |
| Q-10 | DNS über TLS | dnsmasq kann es nicht, Zusatzdienst (z. B. unbound, stubby) | später |
| Q-11 | Lizenz | GPL-3.0 · AGPL-3.0 · MIT · Apache-2.0 | **erledigt: AGPL-3.0-or-later** (D-11, 2026-11-04) |
| Q-12 | Geräte trotz zufälliger MAC wiedererkennen | DHCP-Client-ID, Hostname, Fingerprinting | offen |
| Q-13 | Name | „vanilla-router“ prüfen (Marken, vorhandene Projekte, Domains) | offen. Auf GitHub gibt es mehrere kleine JavaScript-Routing-Bibliotheken dieses Namens [S-14]. Das ist kein rechtliches Problem, aber Suchende landen dort. Möglich: Name behalten und mit Zusatz („vanilla-router OS“, Kürzel `vr`) eindeutig machen. Markenrecherche (DPMA/EUIPO) vor der Veröffentlichung. Stand 2026-11-05 [S-17]: Keine Marke „vanilla router“. Die EU-Marke „VANILLA“ (018129708, Cinnamon B.V.) umfasst in Klasse 42 auch Softwareentwicklung und Netzwerkdienste. Für ein kostenloses Open-Source-Projekt geringes Risiko, vor kommerziellen Angeboten unter dem Namen anwaltlich prüfen. Entscheidung des Projektinhabers: Name bleibt vorerst. |
| Q-14 | Woher kommen die Anbieterdaten (VLAN-IDs, Format der Zugangsdaten)? | eigene gepflegte Liste im Repo · Community-Beiträge | eigene Liste als Datendatei, Beiträge per Pull Request |
| Q-15 | DS-Lite mit systemd-networkd: wird die AFTR-Adresse (DHCPv6 Option 64) ausgewertet? | networkd · eigener Helfer, der die Option liest und den Tunnel anlegt | offen, prüfen in der Provider-VM |
| Q-16 | Secure Boot | shim + GRUB (signiert, Ubuntu-Standard) · systemd-boot (Signierung prüfen) · Secure Boot aus | offen. Muss mit Secure Boot funktionieren, viele Laien-Rechner haben es an. |
| Q-17 | Code-Signierung des Stick-Erstellers | ohne (SmartScreen warnt „Unbekannter Herausgeber“, Anleitung nötig) · OV/EV-Zertifikat (kostet jährlich) · Signierdienst für Open Source (z. B. SignPath Foundation) | offen. Für Laien ist die Warnung abschreckend, ein Signierdienst für Open Source wäre ideal |
| Q-18 | Stick-Ersteller auch für macOS und Linux? | nur Windows · später alle drei | Windows zuerst (Wunsch des Projektinhabers), Kern (Download, Prüfung, Schreiben) so bauen, dass nur Fenster und Laufwerkszugriff je System neu sind |
| Q-19 | Woher kommen die Abbilder, wer signiert? | GitHub Releases · eigener Server · beides | offen. Signierschlüssel offline halten, Manifest getrennt vom Abbild signieren |
| Q-20 | Meldeweg für Sicherheitslücken (SECURITY.md) | eigene Adresse mit PGP · Meldefunktion der Plattform („private vulnerability reporting“) · beides | erledigt (2026-11-05): Meldefunktion von GitHub („Private vulnerability reporting“), in SECURITY.md verlinkt. Eine eigene Adresse kann später dazukommen |

---

## 7. Roadmap

| ID | Meilenstein | Ergebnis | Abnahme |
|---|---|---|---|
| M0 | Projektstart | Repo, Planpapier, Agenten-Anleitung | dieses Dokument liegt vor |
| M1 | Port-Erkennung | Go-Prototyp der WAN-Erkennung (3.9.3), läuft im Namespace-Labor mit 3 veth-Ports (geändert von VM auf Namespaces, siehe T-M1-01) | Kabel „stecken“ wird zuverlässig erkannt, DHCP- und PPPoE-Bestätigung (mit VLAN 7) klappen: `sudo test/lab/netns-lab.sh test` besteht |
| M2 | Konfigurations-Kern | `vrctl` + Schema `config.json` + Generatoren für networkd, nftables, dnsmasq; routerui-Code bereinigt übernommen | Golden-File-Tests, VM mit erzeugter Konfiguration routet IPv4/IPv6 |
| M3 | Installer-Image | mkosi-Image mit Assistent, installiert eine VM von Grund auf | VM bootet nach Installation, LAN-Client bekommt IP, DNS und Internet |
| M4 | Laien-Ablauf | Weboberfläche auf `config.json`, Stick-Ersteller für Windows (3.12), verkürzter Assistent (3.9.2), Einrichtungsmodus mit Internetzugang (F-UI-08) | Testperson ohne Linux-Kenntnisse schafft den ganzen Weg vom Download bis „Internet geht“ ohne Hilfe |
| M5 | Updates und Rücknahme | Snapshots, Gesundheitsprüfung, automatische Rücknahme | absichtlich kaputtes Update wird automatisch zurückgenommen |
| M6 | Hardware und Beta | Testmatrix echter Geräte, Dokumentation, erste öffentliche Version | ≥ 5 Gerätemodelle getestet, Doku für Laien vorhanden |

### 7.1 Aufgaben für M1

Stand 2026-11-04: Code und Unit-Tests fertig (`go test ./...` grün, gebaut mit Go 1.26 auf Ubuntu 26.04).
`vr-detect -list` auf echter Hardware (Prototyp) erkennt beide Ports richtig. **Offen ist nur der
Laborlauf**, er braucht Root-Rechte und die Pakete `golang-go dnsmasq-base pppoe`.

| ID | Aufgabe | Ergebnis | Status |
|---|---|---|---|
| T-M1-01 | Testumgebung. **Geändert:** statt QEMU-VMs Network Namespaces mit veth-Paaren (startet in Sekunden, braucht keine Images, testet trotzdem echtes Netlink, AF_PACKET und VLAN 7). „Kabel stecken“ = Gegenseite einschalten. QEMU kommt erst für den Installer (M3) | `test/lab/netns-lab.sh` mit 5 Szenarien: DHCP, PPPoE über VLAN 7, erst abziehen, mehrdeutig, keine Antwort + Bestätigung | in-arbeit (Skript fertig, Lauf mit Root steht aus) |
| T-M1-02 | Go-Paket `internal/netdetect`: Ports finden (sysfs), Eigenschaften lesen (MAC, Treiber, Bus, Geschwindigkeit, Link) | `ports.go`, Tests mit nachgebautem sysfs-Baum. Eingebaute Ports zuerst, USB-Adapter danach, Reihenfolge nach Bus-Position (PCI hexadezimal, USB natürlich sortiert) | erledigt |
| T-M1-03 | Link-Ereignisse per Netlink (`RTMGRP_LINK`, nur Standardbibliothek) | `linkwatch.go`: Parser getestet mit nachgebauten Kernel-Nachrichten, Socket live getestet (ohne Rechte), `SetUp` per ioctl. Pufferüberlauf (`ENOBUFS`) → Zustand neu lesen | erledigt |
| T-M1-04 | Ablauf „alle Kabel ab → Modem-Kabel ein“ mit Mehrdeutigkeit und Zeitüberschreitung als Zustandsautomat | `internal/wandetect`: reiner Automat (Zeit wird übergeben), 12 Tests, Laientexte ohne Fachbegriffe (geprüft) | erledigt |
| T-M1-05 | Bestätigung per DHCP-Discover auf dem Kandidaten-Port, ohne Adresse zu übernehmen (Rohsocket) | `internal/probe`: Rahmen selbst gebaut (Prüfsummen getestet), Offer-Zerlegung, Ablauf mit nachgebautem Socket und künstlicher Uhr | erledigt (Labor offen) |
| T-M1-06 | PPPoE-Discovery (PADI senden, PADO erkennen), mit und ohne VLAN 7 | ein AF_PACKET-Socket für alles, VLAN-Tag aus `PACKET_AUXDATA` wieder eingefügt (Treiber mit VLAN-Offload), Host-Uniq ordnet Antworten zu | erledigt (Labor offen) |
| T-M1-07 | Kleine Textoberfläche als Vorführer | `cmd/vr-detect`: `-list` (ohne Rechte), Ablauf mit Eingaben (Nummer = Port von Hand, w = wiederholen, j = trotzdem), `-json` für die spätere `config.json` | erledigt |
| T-M1-08 | Prüfskript `test/check-main.sh`: Abhängigkeiten auflösen und melden, was nicht aus Ubuntu *main* kommt (NF-09), dazu SPDX-Zeilen (NF-08) | Auflösung wie apt (nur erste Alternative, erster Anbieter virtueller Pakete): 325 Pakete, alle *main*. Gegenprobe: `fail2ban` und eine Datei ohne SPDX schlagen fehl. Unbekannte Pakete zählen als Fehler | erledigt |

### 7.2 Stand M2, M3, M5 (2026-11-04)

Code und Unit-Tests fertig (10 Pakete, alle grün auf Ubuntu 26.04). Erzeugte Firewall- und dnsmasq-Dateien
für vier Beispielkonfigurationen bestehen `nft -c` und `dnsmasq --test`. Units bestehen `systemd-analyze
verify`. Das Paket `vanilla-router_*.deb` baut, alle 391 Pakete der Images kommen aus *main*.
**Noch nirgends eingespielt oder gebootet**, das ist der Hardware-Test (7.3).

| ID | Aufgabe | Ergebnis | Status |
|---|---|---|---|
| T-M2-01 | Schema `config.json` (Version 1), strenges Laden, Prüfung mit allen Fehlern auf einmal | `internal/config`, 30 Fehlerfälle getestet | erledigt |
| T-M2-02 | Generatoren networkd, nftables, dnsmasq, pppd, sysctl, resolved, sshd, unattended-upgrades | `internal/gen`, Golden Files in `testdata/` (NF-04) | erledigt |
| T-M2-03 | `vrctl apply`: prüfen, sichern, atomar ersetzen, gezielt neu laden, Gesundheitsprüfung, Rücknahme | `internal/apply`, Bestätigung per systemd-run-Timer (überlebt Abbruch der Sitzung) | erledigt |
| T-M2-04 | `vrctl setup`: Ubuntu Server anpassen (D-12) | resolved-Stub, cloud-init, netplan, snapd, SSH aus | erledigt |
| T-M2-05 | Paket und Units | `packaging/build-deb.sh`, `vr-dnsmasq` (dnsmasq-base, gehärtet), `vr-firewall` (vor dem Netzwerk), `vr-pppoe` | erledigt |
| T-M3-01 | Datenträger finden, Stick ausschließen, Partitionen planen | `internal/disk` | erledigt |
| T-M3-02 | Installation: GPT, btrfs mit @/@log/@snapshots, Image entpacken, fstab, Einstellungen, shim+GRUB (auch Standardpfad), Dienste offline, Benutzer admin | `internal/install`, Prüfung vor dem ersten Schreibzugriff | erledigt (ungetestet auf Hardware) |
| T-M3-03 | Assistent S1–S10 | `cmd/vr-install`, Telekom-Maske, verdeckte Passworteingabe, `--dry-run`, `--unattended` (F-INST-08) | erledigt |
| T-M3-04 | Images | `image/target`, `image/stick`, `image/build.sh` (mkosi) | **ungetestet**, siehe `image/README.md` |
| T-M4-01 | Weboberfläche aus dem Prototyp übernehmen, auf `config.json` umstellen (3.11) | `cmd/vr-webui` mit Helper; `internal/conf` übersetzt nur noch zwischen API-Formen und `config.json`, Prüfung per `config.Validate`, Einspielen per `apply` (Sicherung, Rücknahme). dnsmasq-Leases, Adressen und Namen aus der Konfiguration, eigenes TLS-Zertifikat, Einrichtungscode vom Installer, Erweiterungen (Container, Dateiablage, Audit) nur sichtbar, wenn installiert. Im Browser gegen eine lokale Testinstanz geprüft (alle Seiten, Reservierung, DNS, Weiterleitung mit Bestätigung, Sperre, Fehlermeldungen) | erledigt (ungetestet auf Hardware) |
| T-M4-02 | Seite „Über“ mit Lizenz und Quelltext-Link (F-UI-07) | Seitenleiste und Anmeldeseite, `/api/about`, Link per Build-Parameter | erledigt |
| T-M4-03 | Einstellungen für WAN-Art, PPPoE-Zugangsdaten, Hostname und Heimnetz in der Oberfläche (F-UI-03, F-UI-09). Seit D-15 Voraussetzung für die erste Version | Seite „Internet & Heimnetz“ (`web/static/views/internet.js`), Helper-Aktionen `settings.*`. Netzwerk-Änderungen über die Bestätigung von apply (systemd-Timer: WAN 2 min, Heimnetz 5 min), Prüfung „Internet erreichbar“. Heimnetz-Umzug verschiebt Pool, Reservierungen, DNS-Einträge und Weiterleitungen (`config.MoveLAN`), prüft Überschneidung mit dem WAN-Netz. Neues PPPoE-Passwort unter neuem Verweis, damit eine Rücknahme zum alten passt. apply übernimmt Hostname und Zeitzone (`hostnamectl`, `timedatectl`) und startet die Oberfläche bei neuem Namen, neuer Adresse oder Wechsel DHCP/PPPoE neu. Im Browser gegen die Testinstanz geprüft: Telekom-Maske mit Fehlermeldung, Bestätigen, Ablauf der Frist mit Rücknahme, Heimnetz-Umzug, Name und Zeitzone | erledigt (ungetestet auf Hardware) |
| T-M4-04 | Einrichtungsschritt „Internetzugang“ nach dem ersten Passwort (F-UI-08), Anbieter-Vorlagen als Datendatei (Q-14) | `wan.detected` in config.json (F-INST-15), nach der Anmeldung automatisch `#/internet?setup=1` mit erkanntem PPPoE/VLAN vorbelegt, „Später“ für einen Tag, Hinweis auf der Übersicht. Anbieter-Vorlagen bisher nur Telekom (fest im Code), Datendatei offen | erledigt bis auf die Datendatei |
| T-M4-05 | Assistent kürzen (3.9.2): S1 nur ohne `stick.json`, S2 nur bei Problem, S4 ohne Eingabe, S5 und S7 weg, nur Pfeiltasten und Enter (F-INST-15 bis 18) | `cmd/vr-install`: `internal/tui` neu (Tasten statt Zeilen, Menüs mit ↑ ↓ Enter, `MenuAsync` neben der laufenden Erkennung), `internal/stick` (liest `vr/stick.json` unter /efi, /boot/efi, /boot; sonst Auswahl Deutsch DE/AT/CH, English), Erkennung mit Menüs statt Buchstaben („noch einmal“, „trotzdem verwenden“, „Anschluss selbst auswählen“), `wan.detected` wird geschrieben, Löschen per Menü mit Vorauswahl „Nein, abbrechen“. Probelauf in WSL geprüft (Abbruch, Bestätigung, Plan). `vr-detect` und Laborskript angepasst | erledigt (Labor und Hardware offen) |
| T-M4-06 | Anzeige von Adresse und Einrichtungscode auf tty1 nach jedem Start, bis die Oberfläche eingerichtet ist (F-INST-17) | `vr-webui prepare` schreibt `/run/issue.d/50-vanilla-router.issue` (agetty zeigt es vor der Anmeldung), nach dem ersten Passwort ohne Code (`console.setup_done`). QR-Code fehlt noch | erledigt bis auf QR |
| T-M4-07 | Abbild mit reservierter `vr/stick.json` und signiertem Manifest (3.9.1, 3.12) | `image/build.sh`, Werkzeug zum Signieren | Werkzeug `cmd/vr-sign` erledigt (keygen, manifest mit Suche der Reservierung, verify), Abbild offen |
| T-M4-08 | Stick-Ersteller für Windows (F-STICK-01 bis 10) | `cmd/vr-stick`: Fenster mit Win32 über `syscall` (Liste, Knöpfe, Fortschritt, Dateiauswahl), Manifest als Ressource über `internal/winres` (eigene .syso, kein fremdes Werkzeug), `packaging/build-stick-exe.sh`. `internal/windisk` (nur USB-Bus, Systemlaufwerk, Programm- und Zwischenspeicher-Laufwerk ausgeschlossen, Volumes sperren und aushängen, NO_BUFFERING), `internal/stickwrite` (Prüfen vor dem Schreiben, Partitionstabelle zuletzt, `stick.json` eintragen, Zurücklesen), `internal/manifest`, `internal/windefaults` (CLDR `windowsZones`). Download mit Fortsetzen, Zwischenspeicher, Offline-Rückfall. Tests unter Linux und Windows, Fenster als Vorschau geprüft. Mit echtem Stick und signiertem Test-Abbild geprüft (2026-11-05). Seit 0.12 im Stil des Logos: Kopf mit Logo, eigene Knöpfe und eigener Fortschrittsbalken, Logo als Programm-Icon | erledigt |
| T-M5-01 | Snapshot vor jedem apt-Lauf, GRUB-Eintrag „Stand vor dem Update“, Zustand auf der ESP | `internal/snapshot`, `packaging/grub.d`, apt-Hook | erledigt (ungetestet auf Hardware) |
| T-M5-02 | Gesundheitsprüfung nach dem Start, automatische Rücknahme | `vr-health.service`, `panic=10`, Watchdog. Neustart nur bei einem Versuch nach Update (keine Neustartschleife) | erledigt (ungetestet auf Hardware) |

### 7.3 Testplan für echte Hardware

Reihenfolge so gewählt, dass jeder Schritt nur das Ergebnis des vorigen braucht.

1. **Labor in WSL** (ohne Hardware): `sudo test/lab/netns-lab.sh test` → 5 bestanden.
2. **Stick bauen**: `image/build.sh` auf einem Ubuntu 26.04 mit mkosi. Fehler der mkosi-Schlüssel beheben.
   Ab T-M4-08 den Stick mit dem Stick-Ersteller unter Windows schreiben.
3. **Stick booten** (Secure Boot an): Assistent erscheint auf tty1, Probelauf bis S6 abbrechen.
4. **Erkennung**: alle Kabel ab, Modem-Kabel an einen beliebigen Port → richtiger Port, DHCP erkannt.
   Mit einem Telekom-Anschluss zusätzlich: PPPoE über VLAN 7 erkannt.
5. **Installation** auf die interne SSD, Neustart ohne Stick → bootet, `lan0` hat 192.168.42.1, ein Gerät
   im Heimnetz bekommt Adresse, DNS und Internet, IPv6 falls der Anbieter ein Präfix gibt.
6. **vrctl apply --confirm 60s** mit einer Portweiterleitung, nicht bestätigen → nach 60 s zurück.
   In der Oberfläche: Heimnetz auf eine andere Adresse umstellen, Gerät neu verbinden, unter der neuen
   Adresse bestätigen. Zweiter Durchgang ohne Bestätigung → altes Netz kommt zurück, die Oberfläche
   startet unter der alten Adresse neu. Dabei prüfen, ob `networkctl reload` die alte Adresse von `lan0`
   entfernt. Name ändern → neues Zertifikat, Oberfläche unter dem neuen Namen erreichbar.
7. **Rücknahme nach Update**: `vrctl snapshot pre-update`, dann absichtlich `vr-dnsmasq` kaputt machen
   (z. B. `systemctl mask vr-dnsmasq`), neu starten → vr-health startet neu, GRUB nimmt den Snapshot,
   danach läuft der alte Stand und bleibt nach einem weiteren Neustart.

---

## 8. Teststrategie

- **Unit-Tests** für Prüfregeln und Generatoren (Golden Files: erwartete Konfiguration je Eingabe).
- **VM-Tests** mit QEMU/KVM: eine „Provider-VM“ simuliert den Anschluss (DHCPv4, DHCPv6-PD, PPPoE-Server,
  optional DS-Lite), die Router-VM hat 3 NICs, eine Client-VM im LAN prüft DHCP, DNS, Internet, IPv6.
  Kabel stecken und ziehen per Link-Status der virtuellen NIC (QEMU-Monitor `set_link <netdev> on|off`).
  Aufbau:

  ```
  [Provider-VM] ──netz-wan── [Router-VM nic0]
                               [Router-VM nic1] ──netz-lan── [Client-VM]
                               [Router-VM nic2]   (frei, für Mehrdeutigkeits-Tests)
  ```
  Die Netze sind QEMU-`socket`- oder Linux-Bridge-Netze ohne Verbindung zum echten Netz. Die Provider-VM
  hängt für „Internet“ an einem eigenen NAT-Netz. Szenarien als Dateien (`test/scenarios/*.json`): Anschlussart,
  erwartete Erkennung, erwartete Erreichbarkeit.
- **Echte Hardware:** Testmatrix mit Mini-PCs und typischen USB-Netzwerkadaptern (Realtek, ASIX, Intel) in M6.
- **Gleiche Härtung im Test wie im Betrieb** (NF-05, L-09).
- **Selbstprüfung** (Audit) läuft in jeder Test-VM und darf keine kritischen Befunde haben.

---

## 9. Risiken

| ID | Risiko | Gegenmaßnahme |
|---|---|---|
| R-01 | Vielfalt der Provider-Anschlüsse (PPPoE-Varianten, VLAN, DS-Lite, CGNAT) | Provider-VM für jede Variante, Liste getesteter Anbieter, Rückfall auf manuelle Einstellung |
| R-02 | Netzwerkkarten ohne freie Treiber oder mit Firmware-Pflicht (manche Realtek-Chips) | Firmware-Pakete im Image, Hardware-Liste, klare Meldung im Assistenten |
| R-03 | Update macht den Router unbrauchbar | Snapshots + automatische Rücknahme (F-UPD-02/03) |
| R-04 | Laien sperren sich aus (falsche WAN-Einstellung, vergessenes Passwort) | Rücknahme bei Verbindungsverlust, Reset-Weg am Gerät (z. B. Stick erneut booten → „Passwort zurücksetzen“) |
| R-05 | Sicherheitslücke in der Oberfläche | Privilegientrennung, nur LAN, keine Abhängigkeiten, Audit, schnelle Updates |
| R-06 | Projekt hängt an einer Person | offene Lizenz, gute Doku, maschinenlesbares Planpapier, Tests |
| R-07 | Stick-Ersteller überschreibt den falschen Datenträger | nur USB-Bus, nie das Systemlaufwerk, Größengrenze mit Zusatzbestätigung, Name und Größe in der Warnung (F-STICK-03) |
| R-08 | SmartScreen oder Virenscanner warnen vor dem Stick-Ersteller, Laien brechen ab | Code-Signierung (Q-17), reproduzierbarer Build, Prüfsummen auf der Download-Seite, Anleitung mit Bildschirmfotos |
| R-09 | Rechner startet nicht vom Stick (Boot-Reihenfolge, Secure Boot, alte BIOS-Rechner) | Hinweis zu Boot-Menü-Tasten (F-STICK-10), shim für Secure Boot (Q-16), Liste getesteter Geräte |

---

## 10. Repo-Struktur

Stand 0.15. Was wo liegt und wie es zusammenhängt, erklärt [docs/architektur.md](docs/architektur.md),
wie man damit arbeitet [docs/entwicklung.md](docs/entwicklung.md).

```
vanilla-router/
  PLAN.md              dieses Planpapier (führend)
  README.md            Einstieg für Menschen, Wegweiser zur Doku
  CONTRIBUTING.md      Regeln für Code, Tests, Texte, Commits
  SECURITY.md          Sicherheitslücken melden (Meldefunktion von GitHub, Q-20)
  CLAUDE.md            Arbeitsregeln für KI-Agenten
  docs/                architektur.md, entwicklung.md, logo/
  cmd/                 vrctl, vr-webui, vr-install, vr-detect, vr-stick (Windows), vr-sign
  internal/            ein Paket je Aufgabe (Überblick: docs/architektur.md, Abschnitt 10)
  web/                 Frontend (Vanilla-JS, per go:embed eingebettet)
  packaging/           Paket .deb, systemd-Units, Stick-Ersteller
  image/               mkosi-Konfiguration für Stick und Zielsystem
  test/                check-all.sh (alle Prüfungen), check-main.sh (NF-08, NF-09), lab/ (Namespace-Labor),
                       dev/ (Testinstanz der Oberfläche), stick/ (Test-Abbild, Vorschau des Stick-Erstellers)
```

---

## 11. Glossar

| Begriff | Bedeutung |
|---|---|
| WAN | Anschluss Richtung Internet (Modem oder Provider-Router) |
| LAN | Heimnetz |
| PD | Prefix Delegation: Der Anbieter vergibt per DHCPv6 ein IPv6-Präfix für das Heimnetz |
| RA / SLAAC | Router Advertisement / automatische IPv6-Adressen der Geräte |
| ULA | Unique Local Address, private IPv6-Adressen (`fd00::/8`) |
| PPPoE | Einwahlprotokoll bei DSL und manchen Glasfaseranschlüssen |
| DS-Lite | Anschluss mit echtem IPv6 und IPv4 nur über einen Tunnel zum Anbieter |
| CGNAT | IPv4-NAT beim Anbieter, eingehende IPv4-Verbindungen unmöglich |
| home.arpa | reservierte Domain für Heimnetze (RFC 8375) |

---

## 12. Quellen

Recherche vom 2026-11-04. Foren sind als solche gekennzeichnet und müssen vor einer Umsetzung im Testlabor bestätigt werden.

| ID | Quelle | Inhalt |
|---|---|---|
| S-01 | https://packages.ubuntu.com/resolute/dnsmasq · https://packages.ubuntu.com/resolute/fail2ban | dnsmasq und fail2ban liegen in Ubuntu 26.04 in *universe* |
| S-02 | https://www.debian.org/releases/stable/index.de.html | Debian 13: volle Unterstützung bis 2028-08-09, LTS bis 2030-06-30 |
| S-03 | https://wiki.debian.org/IPv6PrefixDelegation | PD mit networkd: beide Interfaces müssen von networkd verwaltet werden |
| S-04 | https://major.io/p/dhcpv6-prefix-delegation-with-systemd-networkd | Beispiel PD + `IPv6SendRA` mit networkd |
| S-05 | https://forum.netgate.com/post/949140 (Forum) | Telekom: Format des PPPoE-Benutzernamens, VLAN 7, Modem als Medienkonverter |
| S-06 | https://telekomhilft.telekom.de/t5/Festnetz-Internet/Optic-fiber-connection-setup-with-Glasfaser-Modem-2-and-my-own/td-p/6566852 (Telekom-Community) | PPPoE über VLAN 7 an Telekom-Anschlüssen |
| S-07 | https://forum.vodafone.de/t5/Ger%C3%A4te/Eigener-Router-Bridge-Mode-DS-Lite/m-p/3055535 (Forum) | Vodafone Kabel: Bridge-Modus, DS-Lite oder Dual-Stack, kann sich ändern |
| S-08 | https://dev.to/lyraalishaikh/stop-risky-linux-upgrades-from-becoming-outages-practical-rollbacks-with-btrfs-snapper-126h | btrfs + snapper für Rücknahmen nach Upgrades |
| S-09 | https://github.com/david-cortes/snapper-in-debian-guide | snapper + grub-btrfs + snapper-rollback auf Debian, Snapshot bei jedem apt-Lauf |
| S-10 | https://parrot.sh/docs/configuration/btrfs-assistant-snapper-setup/ | grub-btrfs zeigt Snapshots, bootet sie auf Debian-Systemen aber nicht zuverlässig ohne initramfs-Anpassung |
| S-11 | https://wiki.archlinux.org/title/Mkosi | mkosi baut bootfähige Debian- und Ubuntu-Images, auf USB schreibbar |
| S-12 | https://isc.org/dhcp/ | ISC DHCP nicht mehr gepflegt seit Ende 2022, Nachfolger Kea |
| S-13 | https://docs.opnsense.org/manual/isc.html | OPNsense: Wechsel zu Kea oder dnsmasq, dnsmasq mit DHCPv6 und RA |
| S-14 | https://github.com/xan105/web-vanilla-router · https://awesome.ecosyste.ms/projects/github.com%2Frayc2045%2Fvanilla-router | Namensgleiche JavaScript-Bibliotheken |
| S-15 | `apt-cache show` auf dem Prototyp (Ubuntu 26.04, 2026-11-04) | *main*: dnsmasq-base, nftables, ppp, btrfs-progs, kea-dhcp4-server. *universe*: dnsmasq, fail2ban, snapper, mkosi. grub-btrfs: kein Paket |
| S-16 | https://www.gnu.org/licenses/agpl-3.0.txt | Lizenztext AGPL Version 3 (19. November 2007), liegt als `LICENSE` im Repo |
| S-17 | https://www.tmdn.org/tmview/ (Suche „vanilla“, Klassen 9/38/42, Ämter DE/EM/WO/AT/CH/BX, 2026-11-05) · https://euipo.europa.eu/eSearch/#details/trademarks/018129708 | Markenrecherche zum Namen (Q-13): 18 Treffer, keine Marke „vanilla router“; „VANILLA“ von Cinnamon B.V. mit Klasse 42 |

---

## Änderungsverlauf

| Version | Datum | Änderung |
|---|---|---|
| 0.1 | 2026-11-04 | Erster Entwurf aus Prototyp und Gesprächen |
| 0.2 | 2026-11-04 | Recherche eingearbeitet: Debian statt Ubuntu (D-08), networkd-PD-Beispiel, Anbieter-Details (Telekom, Vodafone), Stand Snapshots und Rücknahme, mkosi, Name; Q-14, Q-15, Quellen (S-NN) |
| 0.3 | 2026-11-04 | Durchsicht: Widersprüche behoben (Distribution, Zeitdienst, Paketliste), ULA zufällig (F-NET-12), PPPoE-Vorlagen (F-NET-13), Firewall in einer `inet`-Tabelle (D-09), Datenträger und Boot (3.9.4), Wiederherstellung (3.9.5, F-INST-12–14), Code-Übernahme (3.11), Aufgaben für M1 (7.1), VM-Aufbau, Secure Boot (Q-16) |
| 0.4 | 2026-11-04 | Entscheidungen des Projektinhabers: Ubuntu LTS (D-10, D-08 verworfen, Q-01 erledigt), Lizenz AGPL-3.0-or-later (D-11, Q-11 erledigt), Grundsatz Komfort vor Optimierung (P-10). Paketprüfung auf *main* [S-15]: dnsmasq-base statt dnsmasq, kein fail2ban, snapper offen (Q-03). Neu: F-UI-07, NF-08, NF-09, T-M1-08 |
| 0.5 | 2026-11-04 | Ubuntu Server mit Paketumfang `ubuntu-server` (D-12, Tabelle in 3.2: resolved, cloud-init, snapd …), chrony statt timesyncd. M1 begonnen: T-M1-02 bis 08 erledigt, T-M1-01 auf Namespace-Labor umgestellt (Lauf steht aus). L-17 (Locale, Prüfungen ohne Daten) |
| 0.6 | 2026-11-04 | M2 (Konfigurations-Kern), M3 (Installer, Images) und M5 (Snapshots, Rücknahme) im Code fertig, ungetestet auf Hardware (7.2). Q-03 und Q-04 umgesetzt, D-13. Testplan für Hardware (7.3). M4 (Weboberfläche) offen |
| 0.7 | 2026-11-04 | M4: Weboberfläche übernommen und auf `config.json` umgestellt (T-M4-01, T-M4-02), F-UI-07 erledigt, WAN-Einstellungen in der Oberfläche offen (T-M4-03). L-18 |
| 0.8 | 2026-11-04 | Ablauf aus Sicht des Nutzers (Abschnitt 1, D-15) und Grundsatz P-11: Stick-Ersteller für Windows (3.12, F-STICK, D-14), Assistent auf Physisches gekürzt (3.9.2, F-INST-04 verworfen, F-INST-15 bis 18), `vr/stick.json`, Zugangsdaten und Einstellungen in der Oberfläche (3.8, F-UI-08/09). M4 erweitert (T-M4-04 bis 08), Q-17 bis Q-19, R-07 bis R-09 |
| 0.9 | 2026-11-04 | T-M4-03 und T-M4-04 erledigt: Seite „Internet & Heimnetz“, Einrichtungsschritt Internetzugang, `wan.detected`. F-UI-03, F-UI-08, F-UI-09 erledigt (ohne Hardware-Test), Testplan 7.3 ergänzt |
| 0.17 | 2026-11-05 | Auf GitHub englisch (Q-07, Teil): `README.md`, `SECURITY.md` und alle Commit-Nachrichten übersetzt, Verlauf neu geschrieben |
| 0.16 | 2026-11-05 | Erste Veröffentlichung auf GitHub (`jacob3141/vanilla-router`), Vorabversion 0.0.1. Q-20 erledigt (Meldefunktion von GitHub), Markenrecherche zu Q-13 [S-17], Name bleibt vorerst |
| 0.15 | 2026-11-05 | Doku für Entwickler: `docs/architektur.md` (Idee, Programme, Datenfluss, Prozesse, Pakete, Dateien, Sicherheitsmodell, Lesereihenfolge, Stand auf Hardware), `docs/entwicklung.md` (Einrichtung, Testen, Ausprobieren ohne Router, Bauen, Stick-Ersteller, häufige Probleme), `CONTRIBUTING.md`, `SECURITY.md`, README als Wegweiser. Werkzeuge: `test/check-all.sh` (alle Prüfungen mit einem Befehl), `test/dev/webui.sh` (Testinstanz ohne Root), `test/stick/test-package.sh` und `preview.sh`. Q-20 neu, Abschnitt 10 auf den echten Stand |
| 0.14 | 2026-11-05 | Logo als Piktogramm: Kreis (Kugel) über Dreieck (Hörnchen), zwei Farben, ohne Muster. Text-Logo im Assistenten ebenso |
| 0.13 | 2026-11-05 | Logo schlichter: nur Kugel und Hörnchen in zwei Farben (Vanille, Braun), ohne WLAN-Bezug. Stick-Ersteller und Assistent in diesen Farben |
| 0.12 | 2026-11-05 | Projektlogo (Vanilleeis, die Vanilleschote als Antenne): `internal/logo` erzeugt SVG, PNG und Icon ohne fremde Werkzeuge (`docs/logo/`, Favicon der Oberfläche). Stick-Ersteller im Stil des Logos und mit Icon (`internal/winres` kann jetzt mehrere Ressourcen), Assistent mit Text-Logo und farbigen Überschriften. T-M4-08 mit echtem Stick geprüft |
| 0.11 | 2026-11-05 | T-M4-08: Stick-Ersteller für Windows (`cmd/vr-stick`), F-STICK-01 bis 10 erledigt, Test mit echtem Stick offen. `vr-sign` für signierte Manifeste (Teil von T-M4-07) |
| 0.10 | 2026-11-05 | T-M4-05 und T-M4-06: Assistent nur mit Pfeiltasten und Enter, ohne Zugangsdaten, Name und Zeitzone; `vr/stick.json`; Adresse und Code auf der Konsole. F-INST-15, 16, 18 erledigt, F-INST-17 ohne QR |
