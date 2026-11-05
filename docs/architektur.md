# Architektur

Dieses Dokument erklärt, wie vanilla-router aufgebaut ist und warum. Es richtet sich an alle, die den
Code verstehen oder ändern wollen. Wie man die Entwicklungsumgebung einrichtet, steht in
[entwicklung.md](entwicklung.md), die Regeln für Beiträge in [CONTRIBUTING.md](../CONTRIBUTING.md).

Führend ist [PLAN.md](../PLAN.md). Dort stehen Anforderungen, Entscheidungen und offene Fragen mit
stabilen Kennungen (`D-05`, `F-INST-03`, `Q-19` …). Dieses Dokument beschreibt den **gebauten Stand**
und verweist auf die Kennungen, statt sie zu wiederholen.

## Inhalt

1. [Die Idee in fünf Sätzen](#1-die-idee-in-fünf-sätzen)
2. [Gesamtbild](#2-gesamtbild)
3. [Programme](#3-programme)
4. [Das Herz: config.json → erzeugen → einspielen](#4-das-herz-configjson--erzeugen--einspielen)
5. [Weboberfläche: drei Prozesse, drei Rechte](#5-weboberfläche-drei-prozesse-drei-rechte)
6. [Installation: Stick, Assistent, Zielsystem](#6-installation-stick-assistent-zielsystem)
7. [Erkennung des Internet-Anschlusses](#7-erkennung-des-internet-anschlusses)
8. [Updates und Rücknahme](#8-updates-und-rücknahme)
9. [Stick-Ersteller für Windows](#9-stick-ersteller-für-windows)
10. [Pakete im Überblick](#10-pakete-im-überblick)
11. [Dateien und Dienste auf dem Router](#11-dateien-und-dienste-auf-dem-router)
12. [Sicherheitsmodell](#12-sicherheitsmodell)
13. [Wo anfangen beim Lesen](#13-wo-anfangen-beim-lesen)
14. [Stand: was auf echter Hardware geprüft ist](#14-stand-was-auf-echter-hardware-geprüft-ist)

## 1. Die Idee in fünf Sätzen

1. Ein Router ist ein **unverändertes Ubuntu Server LTS** auf einem normalen PC mit zwei
   Netzwerkanschlüssen, plus ein Paket `vanilla-router` (D-10, D-12).
2. Alles, was den Router ausmacht, steht in **einer Datei**, `/etc/vanilla-router/config.json` (D-02).
   Die Dateien von systemd-networkd, nftables, dnsmasq, pppd usw. werden daraus **erzeugt**, nie von Hand
   bearbeitet.
3. Jede Änderung wird **vor dem Einspielen mit den Werkzeugen der Dienste geprüft**, atomar ersetzt und
   bei einem Fehler zurückgenommen. Netzwerk- und Firewall-Änderungen nimmt ein Timer zurück, wenn
   niemand sie bestätigt (P-05).
4. Laien richten den Router ein, ohne Linux zu kennen: Ein **Windows-Programm** schreibt den Stick, ein
   **Assistent** auf dem Stick fragt nur nach Physischem (welches Kabel ins Modem geht, welche Festplatte),
   alles andere passiert in der **Weboberfläche** (D-15, P-11).
5. Der Code ist **Go mit nur der Standardbibliothek** und Vanilla-JS ohne Build-Schritt (D-01). Es gibt
   keine Lieferkette außer Go selbst und Ubuntu *main* (NF-09).

## 2. Gesamtbild

```
   Windows-PC                         der künftige Router
 ┌─────────────────────┐   Stick    ┌──────────────────────────────────────────────┐
 │ vr-stick.exe        │ ─────────▶ │ Boot-Stick (mkosi-Abbild)                    │
 │  Manifest prüfen    │            │  vr-install auf tty1                         │
 │  Abbild laden       │            │   Ports finden → WAN erkennen → Platte wählen│
 │  Stick schreiben    │            │   config.json erzeugen → Zielsystem schreiben│
 │  vr/stick.json      │            └──────────────────────┬───────────────────────┘
 └─────────────────────┘                                   │ Neustart ohne Stick
                                                           ▼
 ┌────────────────────────────────────────────────────────────────────────────────┐
 │ Zielsystem: Ubuntu Server LTS + Paket vanilla-router                           │
 │                                                                                │
 │   Browser im Heimnetz ──HTTPS──▶ vr-webui serve ──Socket──▶ vr-webui helper    │
 │                                   (ohne Root)                (root, feste      │
 │                                                               Aktionsliste)    │
 │                                                                   │            │
 │                     /etc/vanilla-router/config.json ◀─────────────┘            │
 │                                   │                                            │
 │                                   ▼  internal/gen (reine Funktion)             │
 │                         erzeugte Dateien ─▶ internal/apply ─▶ Dienste          │
 │                                             prüfen, sichern,   networkd, nft,  │
 │                                             ersetzen, laden,   dnsmasq, pppd   │
 │                                             Gesundheit, zurück                 │
 │                                                                                │
 │   apt ─Hook─▶ vrctl snapshot pre-update     vr-health nach jedem Start         │
 └────────────────────────────────────────────────────────────────────────────────┘
```

## 3. Programme

Alle Programme liegen unter `cmd/`. Jedes ist ein einzelnes statisches Binary.

| Programm | Läuft wo, als wer | Aufgabe |
|---|---|---|
| `vrctl` | Router, root | Kommandozeile für die Konfiguration: `validate`, `render`, `apply`, `confirm`, `rollback`, `status`, `default`, `secret`, `setup`, `firewall-load`, `snapshot`, `health`. Kopf von [cmd/vrctl/main.go](../cmd/vrctl/main.go) |
| `vr-webui` | Router | Weboberfläche mit den Unterbefehlen `serve` (Benutzer `vr-webui`), `helper` (root), `prepare` (root, vor dem Start), `files` (Besitzer der Freigabe), `setup-code`, `set-password`. Siehe [Abschnitt 5](#5-weboberfläche-drei-prozesse-drei-rechte) |
| `vr-install` | Boot-Stick, root, tty1 | Installations-Assistent. Nur Pfeiltasten und Enter. `--dry-run` für einen Probelauf ohne Schreiben, `--unattended plan.json` für automatische Tests |
| `vr-detect` | Router oder Stick, root | Vorführer der WAN-Erkennung, `-list` zeigt nur die Ports (ohne Rechte, sendet nichts) |
| `vr-stick` | Windows, Administrator | Stick-Ersteller (Win32-Fenster). Auf anderen Systemen nur ein Hinweis |
| `vr-sign` | Rechner des Release-Verantwortlichen | Schlüssel erzeugen, Manifest eines Abbilds signieren und prüfen |

Dazu kommen kleine Erzeuger, die nur in der Entwicklung laufen: `internal/logo/mklogo` (Logo-Dateien),
`internal/winres/mkres` (Windows-Ressourcen per `go generate`), `test/stick/mkimage` (Test-Abbild).

## 4. Das Herz: config.json → erzeugen → einspielen

### 4.1 Schema

[internal/config](../internal/config/config.go) ist das Schema von `config.json` (Version 1). Laden ist
streng: Unbekannte Felder sind ein Fehler. `Validate` prüft **alle** Werte und liefert alle Fehler auf
einmal (`config.Errors`, je Feld ein Text für Laien). Danach müssen die Erzeuger nichts mehr prüfen.

Geheimnisse (PPPoE-Passwort) stehen nicht in `config.json`, sondern als Dateien unter
`/etc/vanilla-router/secrets/` (0600). `config.json` verweist nur darauf (`password_ref`). So lässt sich
die Konfiguration gefahrlos exportieren.

`config.Default` baut die Vorgabe-Konfiguration aus dem, was der Assistent erkannt hat: MAC des
Internet-Anschlusses, MACs der übrigen Ports, Netz am Internet-Anschluss (damit sich das Heimnetz nicht
überschneidet). Das Heimnetz ist `192.168.42.0/24`, der Router `.1`, DHCP `.100`–`.249`, ein zufälliges
ULA-Präfix nach RFC 4193.

### 4.2 Erzeugen

[internal/gen](../internal/gen/gen.go) macht aus einer geprüften Konfiguration eine Liste von Dateien
(`gen.File`: Pfad, Rechte, Gruppe, Inhalt). Das ist eine **reine Funktion** ohne Zugriff auf das System.
Deshalb lässt sie sich mit Golden Files testen: Für jede Beispielkonfiguration unter
`internal/gen/testdata/<fall>/config.json` liegt die erwartete Ausgabe unter `…/out/`.

| Gruppe | Dateien | Neu laden mit |
|---|---|---|
| `link` | `/etc/systemd/network/10-vr-*.link` (Ports per MAC auf `wan0`, `lanp1`, `lanp2` … umbenennen) | erst nach Neustart |
| `networkd` | `20-vr-*.netdev`, `30-vr-*.network` (Bridge `lan0`, VLAN, DHCP, IPv6-PD, RA) | `networkctl reload` |
| `firewall` | `/etc/vanilla-router/nft/vr.nft` (eigene Tabellen, nie `flush ruleset`, D-03) | `vrctl firewall-load` |
| `dnsmasq` | `/etc/vanilla-router/dnsmasq.conf` (DNS, DHCPv4, Reservierungen, lokale Namen) | Neustart `vr-dnsmasq` |
| `ppp` | `/etc/ppp/peers/vr-wan`, `chap-secrets`, `pap-secrets` | Neustart `vr-pppoe` |
| `sysctl`, `resolved`, `ssh`, `apt` | Weiterleitung, resolved ohne Stub-Listener, sshd, unattended-upgrades | gezielt je Gruppe |

Feste Namen (PLAN.md 3.4): `wan0` ist der Internet-Anschluss, `lan0` eine Bridge über alle übrigen
Ports (auch wenn es nur einer ist), bei PPPoE kommt `ppp0` dazu, mit VLAN `wan0.7`.

Jede erzeugte Datei beginnt mit „Von vanilla-router erzeugt, nicht von Hand ändern“. `gen.Managed` legt
fest, welche Dateien vanilla-router gehören. Was dort liegt und nicht mehr erzeugt wird, entfernt apply
(zum Beispiel die VLAN-Datei nach einem Wechsel von PPPoE auf DHCP).

### 4.3 Einspielen

[internal/apply](../internal/apply/apply.go) spielt die erzeugten Dateien ein:

```
erzeugen ─▶ mit dem Ist-Stand vergleichen ─▶ im Staging prüfen (nft -c, dnsmasq --test)
        ─▶ alte Fassung als Generation sichern (/var/lib/vanilla-router/generations/<id>)
        ─▶ atomar ersetzen (schreiben + rename) ─▶ nur betroffene Gruppen neu laden
        ─▶ Gesundheitsprüfung (Firewall geladen, vr-dnsmasq läuft, lan0 hat seine Adresse)
        ─▶ bei Fehler: Generation zurückspielen, neu laden, Fehler melden
```

Die Gesundheitsprüfung ([internal/apply/health.go](../internal/apply/health.go)) prüft das Internet
bewusst nicht: Das Modem kann aus sein, und dafür soll keine Änderung zurückgenommen werden.

**Bestätigung** (`vrctl apply --confirm 60s`, von der Oberfläche für Netzwerk und Firewall genutzt): apply
legt `pending.json` an und startet mit `systemd-run` einen Timer, der `vrctl rollback --pending <id>`
aufruft. Der Timer läuft unabhängig vom aufrufenden Prozess. Reißt die Browser- oder SSH-Sitzung ab, weil
die Änderung das Netz kaputt gemacht hat, kommt der alte Stand trotzdem zurück. `vrctl confirm` stoppt
den Timer.

Alles, was Programme startet, läuft über das Interface `apply.Runner`. In Tests wird es durch eine
Aufzeichnung ersetzt, und `Applier.Root` zeigt auf einen Testordner statt auf `/`.

## 5. Weboberfläche: drei Prozesse, drei Rechte

Die Oberfläche stammt aus einem von Hand eingerichteten Prototyp, der einen echten Haushalt versorgt
(PLAN.md 2.3, 3.11). Sie ist in drei Prozesse mit getrennten Rechten geteilt (D-04):

```
Browser ──HTTPS :443──▶ vr-webui serve            Benutzer vr-webui, nur CAP_NET_BIND_SERVICE,
                        internal/api, auth, sys    ProtectSystem=strict, schreibt nur ihren Datenordner
                           │            │
     Unix-Socket, eine JSON-Zeile       │ HTTP über Unix-Socket (nur /api/files/*)
     je Anfrage, SO_PEERCRED            ▼
                           ▼         vr-webui files      Besitzer der Freigabe (Erweiterung NAS),
                    vr-webui helper  internal/files      ohne Capabilities, ohne Netz, alle Pfade
                    root, internal/helper                über os.Root
                    feste Aktionsliste, jede prüft
                    ihre Parameter selbst
                           │
                           ▼
                    config.json ─▶ internal/conf ─▶ config.Validate ─▶ gen ─▶ apply
```

- **`serve`** ([internal/api](../internal/api/server.go)): Login, Sitzungen, CSRF-Schutz, statische
  Dateien, JSON-Endpunkte, Live-Werte per Server-Sent Events. Liest frei lesbare Werte selbst
  ([internal/sys](../internal/sys/stats.go): `/proc`, Leases, Verkehr), alles andere fragt er beim Helper.
- **`helper`** ([internal/helper](../internal/helper/protocol.go)): Jede Verbindung trägt genau eine
  Anfrage `{"action": "...", "params": {...}}` und eine Antwort. Es gibt nur fest registrierte Aktionen
  (`settings.wan.set`, `portfw.set`, `dhcp.reservation.set`, `service.restart` …, Liste mit
  `grep -rn 's.handle("' internal/helper`). Fehlertexte erscheinen nur dann in der Oberfläche, wenn sie
  als `helper.UserError` markiert sind, sonst steht dort „interner Fehler“ und die Details im Journal.
- **`files`** ([internal/files](../internal/files/files.go)): nur für die Erweiterung NAS. Löschen
  verschiebt in einen Papierkorb, Downloads gehen immer als Anhang mit `CSP: sandbox`.
- **`prepare`** läuft vor `serve` als root (`ExecStartPre=+`): TLS-Zertifikat anlegen oder erneuern, wenn
  sich Name oder Adresse geändert haben, Einrichtungscode vom Installer übernehmen, Hinweis für die
  Konsole schreiben (`/run/issue.d/50-vanilla-router.issue`).

[internal/conf](../internal/conf/conf.go) übersetzt zwischen den JSON-Formen der API (aus dem Prototyp)
und `config.json`. Geprüft wird **nur** mit `config.Validate`, erzeugt **nur** mit `internal/gen`. So gibt
es keine zweite Wahrheit.

**Frontend** ([web/static](../web/static)): Vanilla-JS als ES-Module, ein Hash-Router (`app.js`), eine
Datei je Seite unter `views/`. Das DOM entsteht nur über die Hilfsfunktion `h()` in `ui.js`, nie über
`innerHTML`. Die CSP verbietet Inline-Skripte und `style`-Attribute, deshalb werden Stile nur über
`el.style` gesetzt. Die Dateien werden per `go:embed` ([web/embed.go](../web/embed.go)) ins Binary
eingebettet. Einen Build-Schritt gibt es nicht.

**Anmeldung**: Passwort als PBKDF2-SHA256 mit 600 000 Runden ([internal/auth](../internal/auth/password.go)),
Sitzungen nur im Speicher (30 Minuten Untätigkeit, höchstens 12 Stunden), Login-Bremse je IP. Das erste
Passwort setzt man mit einem Einrichtungscode, den der Installer erzeugt und der Router auf seinem
Bildschirm zeigt.

## 6. Installation: Stick, Assistent, Zielsystem

**Abbilder** ([image/](../image/README.md), mkosi): `image/target` baut das Zielsystem als
`rootfs.tar.zst` (Ubuntu Server mit dem Paket vanilla-router), `image/stick` ein kleines Live-System, das
`vr-install` auf tty1 startet und das Zielsystem enthält. Das Paket baut
[packaging/build-deb.sh](../packaging/build-deb.sh), seine Abhängigkeiten stehen in
`image/packages.txt` und kommen nur aus Ubuntu *main* (geprüft von `test/check-main.sh`).

**Ablauf des Assistenten** ([cmd/vr-install](../cmd/vr-install/main.go), PLAN.md 3.9.2):

| Schritt | Paket | Was passiert |
|---|---|---|
| Sprache | `internal/stick` | `vr/stick.json` vom Stick lesen (vom Stick-Ersteller), sonst eine Auswahl |
| Hardware | `internal/netdetect`, `internal/disk` | Ports und Datenträger finden. Sichtbar nur bei einem Problem (weniger als zwei Ports, keine Platte ≥ 16 GB) |
| Internet-Anschluss | `internal/wandetect/live` | siehe [Abschnitt 7](#7-erkennung-des-internet-anschlusses) |
| Festplatte | `internal/tui` | Auswahl und Bestätigung, Vorauswahl „Nein, abbrechen“ |
| Installation | `internal/install` | GPT (ESP + btrfs mit `@`, `@log`, `@snapshots`), Zielsystem entpacken, Konfiguration erzeugen, shim + GRUB, Dienste, Einrichtungscode |
| Geschafft | | Adresse der Oberfläche und Einrichtungscode, dann Neustart |

Grundsatz: **Bis zur Bestätigung der Festplatte wird nichts geschrieben.** `install.Install` erzeugt
sogar alle Dateien schon vor dem ersten Schreibzugriff, damit ein Fehler in der Konfiguration auffällt,
bevor die Platte gelöscht ist.

[internal/tui](../internal/tui/tui.go) ist absichtlich klein: nur Pfeiltasten und Enter (keine
Texteingabe, also egal welche Tastaturbelegung), funktioniert auf jeder Konsole, auch seriell.

## 7. Erkennung des Internet-Anschlusses

Das Kernstück des Assistenten (PLAN.md 3.9.3). „Ziehen Sie alle Kabel ab, stecken Sie nur das Kabel vom
Modem ein“, und der Port mit dem ersten Link ist der Internet-Anschluss.

| Schicht | Paket | Aufgabe |
|---|---|---|
| Ports | `internal/netdetect` | Ethernet-Ports aus sysfs (ohne WLAN und virtuelle Geräte), sortiert: eingebaut vor USB, dann nach Bus-Position. Link-Ereignisse über Netlink (`RTMGRP_LINK`), nur mit der Standardbibliothek |
| Ablauf | `internal/wandetect` | **reiner Zustandsautomat**: bekommt Ereignisse (Link an/aus, Zeit, Ergebnis einer Probe, Eingabe), gibt Aktionen zurück. Kein I/O, die Zeit wird übergeben. Deshalb lassen sich auch Zeitüberschreitungen und Mehrdeutigkeiten deterministisch testen |
| Proben | `internal/probe` | DHCP-Discover und PPPoE-PADI (ohne und mit VLAN 7) über einen AF_PACKET-Rohsocket, Rahmen selbst gebaut. Übernimmt keine Adresse, verändert also nichts |
| Verdrahtung | `internal/wandetect/live` | verbindet Automat, Netlink, Proben und die Menüs der TUI |

Getestet wird das ohne Hardware im **Namespace-Labor** ([test/lab/netns-lab.sh](../test/lab/netns-lab.sh)):
Namespaces mit veth-Paaren, dnsmasq als DHCP-Server, pppoe-server auf VLAN 7. „Kabel stecken“ heißt dort
„Gegenseite einschalten“.

## 8. Updates und Rücknahme

Sicherheitsupdates spielt unattended-upgrades ein. Damit ein kaputtes Update den Router nicht lahmlegt
(D-13, [internal/snapshot](../internal/snapshot/snapshot.go)):

1. Ein apt-Hook (`packaging/apt/80vr-snapshot`) ruft vor jedem apt-Lauf `vrctl snapshot pre-update` auf:
   btrfs-Snapshot von `@` nach `@snapshots/<id>`, Boot-Eintrag „Stand vor dem Update“, Zustand
   `vr_pending=1` in einer grubenv auf der EFI-Partition (GRUB kann auf FAT schreiben, auf btrfs nicht).
2. Der nächste Start ist ein **Versuch**. `vr-health.service` (`vrctl health --mark-good --reboot-on-fail`)
   wartet bis zu 10 Minuten auf dieselbe Gesundheitsprüfung wie apply (Firewall, DNS/DHCP,
   Heimnetz-Adresse).
3. Gesund: Start bestätigen. Nicht gesund: Neustart, GRUB nimmt den Snapshot. Die Rücknahme wird durch
   Umbenennen der Subvolumes dauerhaft.
4. Neu gestartet wird **nur** nach einem Update-Versuch. Sonst gäbe es bei einem Dauerfehler eine
   Neustartschleife.

## 9. Stick-Ersteller für Windows

[cmd/vr-stick](../cmd/vr-stick) ist eine einzige `.exe` ohne Installation (PLAN.md 3.12, D-14). Das
Fenster ist reines Win32 über `syscall` (keine fremden Bibliotheken).

| Teil | Paket | Aufgabe |
|---|---|---|
| Abbild beschreiben | `internal/manifest` | `manifest.json` + `manifest.json.sig` (Ed25519): Datei, Größe, SHA-256, Lage der Reservierung `vr/stick.json` |
| Signieren | `cmd/vr-sign` | Schlüssel, Manifest, Prüfung. Der private Schlüssel gehört auf einen Rechner ohne Netz (Q-19) |
| Laden | `cmd/vr-stick/source.go` | Manifest holen und prüfen, Download mit Fortsetzen, Zwischenspeicher, Rückfall auf den letzten Stand ohne Netz, lokale Datei |
| Laufwerke | `internal/windisk` | nur USB-Bus, nie System-, Programm- oder Zwischenspeicher-Laufwerk, Volumes sperren und aushängen, ungepuffert schreiben |
| Schreiben | `internal/stickwrite` | vollständig prüfen **bevor** geschrieben wird, Partitionstabelle zuletzt (damit Windows nichts halb Geschriebenes einhängt), `stick.json` einsetzen, zurücklesen und vergleichen |
| Vorgaben aus Windows | `internal/windefaults` | Gebietsschema, Tastatur (KLID), Zeitzone (CLDR `windowsZones.xml`) → `vr/stick.json` |
| Ressourcen | `internal/winres` | Manifest (Administratorrechte, DPI) und Icon als `.syso`, ohne fremdes Werkzeug |

`stick.json` ist ein Trick, der den Ersteller einfach hält: Im Abbild liegt eine Datei fester Größe
(4096 Bytes, mit Leerzeichen gefüllt). Ihre Lage steht im Manifest. Der Ersteller überschreibt beim
Schreiben genau diese Bytes und muss kein Dateisystem verstehen.

Alles außer Fenster und Laufwerkszugriff ist plattformunabhängig und wird unter Linux getestet.

## 10. Pakete im Überblick

Wer wen importiert (vereinfacht, Pfeil = „benutzt“):

```
vrctl ─────────▶ apply ──▶ gen ──▶ config
vr-install ────▶ install ──▶ apply, gen, disk, config
               ▶ wandetect/live ──▶ wandetect, netdetect, probe
               ▶ tui, stick
vr-webui serve ▶ api ──▶ auth, sys, conf, helper (nur der Client)
vr-webui helper▶ helper ──▶ apply, gen, conf ──▶ config
vr-webui files ▶ files
vr-stick ──────▶ manifest, stickwrite, windisk, windefaults, stick   (winres, logo per go generate)
```

`config` hängt von keinem anderen Paket des Projekts ab, `gen` nur von `config`. Wer am Schema etwas
ändert, findet die Folgen deshalb mit `grep -rln '"vanilla-router/internal/config"'`.

| Paket | Inhalt | Tests |
|---|---|---|
| `config` | Schema, strenges Laden, `Validate`, Vorgaben, Umzug des Heimnetzes | Fehlerfälle |
| `gen` | Erzeuger für alle Dienste | Golden Files |
| `apply` | Einspielen, Generationen, Bestätigung, Rücknahme, Gesundheit, `setup` | mit Testordner und Runner-Aufzeichnung |
| `conf` | Übersetzung API ↔ `config.json` | über `helper` |
| `helper` | privilegierte Aktionen | Probelauf |
| `api` | HTTP, Middleware, Endpunkte | `httptest` |
| `auth` | Passwort, Sitzungen, Login-Bremse | Einheit |
| `sys` | Werte aus `/proc`, Leases, Verlauf, Online-Monitor, natives ICMP | nachgebaute Dateien |
| `files` | Dateidienst mit `os.Root` | echte Verzeichnisse, Ausbruchsversuche |
| `install`, `disk` | Installation, Datenträger | nachgebautes sysfs, Runner-Aufzeichnung |
| `netdetect`, `probe`, `wandetect` | WAN-Erkennung | nachgebaute Kernel-Nachrichten und Rahmen, künstliche Uhr |
| `snapshot` | Snapshots und GRUB-Zustand | Runner-Aufzeichnung |
| `stick`, `tui` | Vorgaben vom Stick, Textoberfläche | Einheit |
| `manifest`, `stickwrite`, `windefaults`, `windisk`, `winres` | Stick-Ersteller | Speicher-Gerät, `debug/pe`, Windows-Test nur lesend |
| `logo` | Logo als Geometrie → SVG, PNG, ICO | Dateien im Repo müssen zum Code passen |

## 11. Dateien und Dienste auf dem Router

| Pfad | Inhalt |
|---|---|
| `/etc/vanilla-router/config.json` | **die** Konfiguration |
| `/etc/vanilla-router/secrets/` | Geheimnisse (0700), z. B. PPPoE-Passwort |
| `/etc/vanilla-router/nft/vr.nft`, `dnsmasq.conf` | erzeugt |
| `/etc/vanilla-router/tls/` | Zertifikat der Oberfläche (selbstsigniert, Q-09) |
| `/etc/systemd/network/*-vr-*` | erzeugt |
| `/var/lib/vanilla-router/generations/` | Sicherungen vor jeder Änderung |
| `/var/lib/vanilla-router/pending.json` | wartende, unbestätigte Änderung |
| `/var/lib/vanilla-router/dnsmasq.leases` | DHCP-Leases |
| `/var/lib/vanilla-router/webui/` | Passwort-Hash, Verlauf, Gerätenamen (Benutzer `vr-webui`, 0700) |
| `/run/vr-webui/helper.sock` | Socket des Helpers |

| Unit | Aufgabe |
|---|---|
| `vr-firewall.service` | Firewall **vor** dem Netzwerk laden (`sysinit.target`) |
| `vr-firewall-reload.timer` | tägliches Neuladen (Zeitsperren, Sommerzeit) |
| `vr-dnsmasq.service` | dnsmasq aus `dnsmasq-base` (*main*), gehärtet |
| `vr-pppoe.service` | pppd, nur wenn PPPoE konfiguriert ist |
| `vr-webui-helper.service`, `vr-webui.service` | Oberfläche |
| `vr-health.service` | Gesundheitsprüfung nach dem Start |
| `vr-installer.service` | nur auf dem Stick: Assistent statt Anmeldung auf tty1 |

Was Ubuntu Server mitbringt und für einen Router stört (resolved-Stub auf Port 53, cloud-init, netplan,
snapd …), wird nicht entfernt, sondern einmalig per `vrctl setup` abgeschaltet (D-12, PLAN.md 3.2).

## 12. Sicherheitsmodell

| Grenze | Maßnahme |
|---|---|
| Internet → Router | Firewall lädt vor dem Netzwerk, eingehend alles verworfen, IPv4 und IPv6 in **einer** `inet`-Tabelle (D-09, damit IPv6 nicht vergessen wird). SSH ab Werk aus |
| Heimnetz → Oberfläche | nur an der Heimnetz-Adresse, TLS, CSRF über Origin und Kopfzeile `X-Routerui`, strenge CSP, Login-Bremse |
| Oberfläche → root | Webserver ohne root, Helper mit fester Aktionsliste, `SO_PEERCRED`, jede Aktion prüft selbst (D-04) |
| Fehlbedienung | Prüfen vor dem Einspielen, Generationen, Rücknahme-Timer, Gesundheitsprüfung |
| Kaputtes Update | Snapshot und automatische Rücknahme (Abschnitt 8) |
| Lieferkette | nur Go-Standardbibliothek, keine npm-Pakete, Ubuntu *main* (NF-09) |
| Stick-Ersteller | signiertes Manifest, Prüfung vor dem Schreiben, nur USB-Laufwerke |

Sicherheitslücken melden: siehe [SECURITY.md](../SECURITY.md).

## 13. Wo anfangen beim Lesen

Für einen ersten Rundgang durch den Code, in dieser Reihenfolge:

1. [internal/config/config.go](../internal/config/config.go): was ein Router überhaupt ist.
2. [internal/gen/gen.go](../internal/gen/gen.go) und eine Golden-File-Ausgabe, z. B.
   [testdata/dhcp/out/etc/vanilla-router/nft/vr.nft](../internal/gen/testdata/dhcp/out/etc/vanilla-router/nft/vr.nft).
3. [internal/apply/apply.go](../internal/apply/apply.go): wie Änderungen sicher werden.
4. [cmd/vrctl/main.go](../cmd/vrctl/main.go): alles davon von der Kommandozeile.
5. [internal/helper/protocol.go](../internal/helper/protocol.go) und
   [internal/helper/settings.go](../internal/helper/settings.go): wie die Oberfläche Änderungen auslöst.
6. [cmd/vr-install/main.go](../cmd/vr-install/main.go) und
   [internal/wandetect/machine.go](../internal/wandetect/machine.go): der Weg für Laien.

## 14. Stand: was auf echter Hardware geprüft ist

Ehrlich gesagt: noch wenig. Der genaue Stand je Aufgabe steht in PLAN.md 7.

| Teil | Stand |
|---|---|
| Unit-Tests, Golden Files, `nft -c`, `dnsmasq --test`, `systemd-analyze verify` | grün |
| Weboberfläche | im Browser gegen die lokale Testinstanz geprüft, nicht auf einem Router mit erzeugter Konfiguration |
| Erkennung der Ports (`vr-detect -list`) | auf echter Hardware geprüft |
| Stick-Ersteller | mit echtem Stick und Test-Abbild geprüft |
| Namespace-Labor | Skript fertig, Lauf steht aus |
| Abbilder (mkosi), Installation, Rücknahme nach Update | **ungetestet**, Testplan in PLAN.md 7.3 |
