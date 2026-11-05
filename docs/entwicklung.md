# Entwicklung

Alles, was man braucht, um an vanilla-router zu arbeiten: einrichten, bauen, testen, ausprobieren.
Wie das Ganze aufgebaut ist, steht in [architektur.md](architektur.md). Die Regeln für Beiträge stehen in
[CONTRIBUTING.md](../CONTRIBUTING.md).

Jeder Befehl in diesem Dokument ist so gemeint, wie er dasteht: aus dem Wurzelordner des Repos, ohne
Root-Rechte, solange nicht `sudo` davorsteht. Nichts davon verändert den Rechner außerhalb des Repos,
außer dort, wo es ausdrücklich dabeisteht.

## Inhalt

1. [Kurzfassung](#1-kurzfassung)
2. [Voraussetzungen](#2-voraussetzungen)
3. [Erste Prüfung](#3-erste-prüfung)
4. [Wo was liegt](#4-wo-was-liegt)
5. [Testen](#5-testen)
6. [Ausprobieren ohne Router](#6-ausprobieren-ohne-router)
7. [Bauen](#7-bauen)
8. [Stick-Ersteller (Windows)](#8-stick-ersteller-windows)
9. [Häufige Probleme](#9-häufige-probleme)

## 1. Kurzfassung

Auf Ubuntu 26.04 (auch in WSL2 unter Windows):

```bash
sudo apt install golang-go git
```

```bash
git clone <Adresse des Repos> vanilla-router && cd vanilla-router
```

```bash
test/check-all.sh
```

```bash
test/dev/webui.sh
```

Der letzte Befehl startet die Weboberfläche auf http://127.0.0.1:9443. Das Passwort steht danach in
`dev/test-credentials.txt`.

## 2. Voraussetzungen

### Pflicht

| Was | Version | Woher |
|---|---|---|
| Linux, x86-64 | empfohlen **Ubuntu 26.04 LTS**, dieselbe Version wie das Zielsystem | auch in WSL2 unter Windows 11 |
| Go | mindestens die Version in [go.mod](../go.mod) (derzeit 1.26) | Ubuntu 26.04: `sudo apt install golang-go`, sonst https://go.dev/dl |
| git | beliebig | `sudo apt install git` |

Es gibt **keine** weiteren Abhängigkeiten: kein `go mod download`, kein npm, kein Docker. Das Projekt
benutzt nur die Go-Standardbibliothek (D-01). Ein frischer Klon baut ohne Netz, sobald Go installiert ist.

Andere Distributionen funktionieren für Bauen und Unit-Tests genauso. Nur die Prüfung der Paketquellen
(`test/check-main.sh`) braucht `apt-cache` eines Ubuntu 26.04 und wird sonst übersprungen.

### Je nach Aufgabe

| Aufgabe | Zusätzlich | Abschnitt |
|---|---|---|
| Erzeugte Firewall und dnsmasq-Konfiguration echt prüfen | `nftables`, `dnsmasq-base` | [5.3](#53-erzeugte-dateien-mit-den-echten-werkzeugen-prüfen) |
| Namespace-Labor der WAN-Erkennung | Root, `iproute2`, `dnsmasq-base`, `pppoe` | [5.4](#54-namespace-labor-wan-erkennung) |
| Paket `.deb` bauen | `dpkg-dev` | [7](#7-bauen) |
| Boot-Stick bauen | `mkosi` ≥ 25, Root oder User-Namespaces, Internet | [7](#7-bauen) |
| Test-Paket für den Stick-Ersteller | `dosfstools` | [8](#8-stick-ersteller-windows) |
| Stick-Ersteller unter Windows testen | Windows 10/11, Go für Windows | [8](#8-stick-ersteller-windows) |

### Editor

Jeder Editor mit Go-Unterstützung (gopls). Wichtig sind nur zwei Dinge:

- **gofmt beim Speichern.** `test/check-all.sh` lehnt unformatierten Code ab.
- **Zeilenenden LF.** [.gitattributes](../.gitattributes) erzwingt das beim Einchecken, auch unter
  Windows. Shell-Skripte mit CRLF laufen unter Linux nicht.

Unter Windows: Repo in WSL2 klonen (z. B. `~/vanilla-router`) und mit einem Editor öffnen, der WSL
kennt. Ein Klon auf einem Windows-Laufwerk (`/mnt/c/…`) funktioniert auch, ist aber deutlich langsamer.

## 3. Erste Prüfung

```bash
test/check-all.sh
```

Das Skript ist dasselbe, das vor jedem Commit grün sein muss. Es führt aus:

| Schritt | Prüft |
|---|---|
| `gofmt -l .` | Formatierung |
| `go vet ./...` | typische Fehler, für Linux |
| `GOOS=windows go vet …` | dasselbe für den Stick-Ersteller (läuft auch auf Linux) |
| `go test ./...` | alle Unit- und Golden-File-Tests |
| `test/check-main.sh` | SPDX-Zeile in jeder Quelldatei (NF-08), alle Pakete aus Ubuntu *main* (NF-09) |

Der erste Lauf dauert länger, weil Go seinen Cache aufbaut, danach geht es schnell.
`test/check-all.sh --quick` lässt die Windows-Prüfung und die Paketquellen weg.

Am Ende steht `ALLES BESTANDEN`. Zwei Tests überspringen sich in manchen Umgebungen selbst und sind dann
kein Fehler: der Netlink-Test (ohne Netlink, z. B. in Containern) und der ICMP-Test (wenn
`net.ipv4.ping_group_range` ungewöhnliche ICMP-Sockets verbietet).

## 4. Wo was liegt

```
cmd/            Programme: vrctl, vr-webui, vr-install, vr-detect, vr-stick (Windows), vr-sign
internal/       alle Pakete, ein Ordner je Aufgabe (Überblick: docs/architektur.md, Abschnitt 10)
web/static/     Frontend (Vanilla-JS, ohne Build-Schritt), per go:embed im Binary
packaging/      Paket .deb, systemd-Units, Stick-Ersteller (.exe)
image/          mkosi-Konfiguration für Zielsystem und Boot-Stick
test/           Prüfskripte, Labor, Testinstanz, Test-Paket für den Stick-Ersteller
docs/           diese Anleitungen, Logo
PLAN.md         Planpapier: Anforderungen, Entscheidungen, offene Fragen, Stand (führend)
CLAUDE.md       Arbeitsregeln, auch für KI-Agenten
```

Nicht im Repo (per [.gitignore](../.gitignore)): `dist/` (Build-Ergebnisse), `dev/` (Testpasswort,
Testschlüssel), `test/dev/work/`, `test/lab/work/`.

## 5. Testen

### 5.1 Unit-Tests

```bash
go test ./...
```

```bash
go test ./internal/gen -run TestGolden -v
```

Die Tests brauchen weder Root noch Netz noch installierte Dienste. Was das System anfasst, läuft hinter
einem Interface (`apply.Runner`, `install.Runner`, nachgebautes sysfs, künstliche Uhr) und wird in Tests
ersetzt. Neue Tests folgen demselben Muster. Ein Test, der Root braucht oder Pakete ins Netz sendet,
gehört ins Labor (5.4), nicht in `go test`.

### 5.2 Golden Files

Die Erzeuger (`internal/gen`) werden mit erwarteten Ausgaben verglichen: Für jeden Fall unter
`internal/gen/testdata/<fall>/` liegt eine `config.json` und daneben unter `out/` jede erzeugte Datei.

Nach einer **gewollten** Änderung an einem Erzeuger:

```bash
go test ./internal/gen -update
```

```bash
git diff internal/gen/testdata
```

Den Unterschied genau lesen: Er ist genau das, was sich auf jedem Router ändern wird. Einen neuen Fall
legt man an, indem man einen Ordner mit einer `config.json` anlegt und `-update` laufen lässt.

### 5.3 Erzeugte Dateien mit den echten Werkzeugen prüfen

Die Golden Files beweisen nur, dass sich nichts ungewollt ändert. Ob nftables und dnsmasq die Dateien
auch annehmen, prüfen deren eigene Werkzeuge. Das braucht `nftables` und `dnsmasq-base`:

```bash
for f in internal/gen/testdata/*/out/etc/vanilla-router/dnsmasq.conf; do dnsmasq --test --conf-file="$f" || echo "FEHLER: $f"; done
```

```bash
for f in internal/gen/testdata/*/out/etc/vanilla-router/nft/vr.nft; do sudo unshare --net nft -c -f "$f" || echo "FEHLER: $f"; done
```

`nft -c` braucht Root. `unshare --net` lässt es in einem leeren Netzwerk-Namespace laufen, die Firewall
des Rechners bleibt unberührt. `-c` prüft nur und lädt nichts.

### 5.4 Namespace-Labor (WAN-Erkennung)

Die Erkennung des Internet-Anschlusses arbeitet mit Netlink und Rohsockets. Das Labor baut dafür ein
Netz aus Network Namespaces und veth-Paaren: dnsmasq als DHCP-Server, pppoe-server auf VLAN 7, ein freier
Port. „Kabel einstecken“ heißt dort „Gegenseite einschalten“.

```bash
sudo apt install iproute2 dnsmasq-base pppoe
```

```bash
sudo test/lab/netns-lab.sh test
```

Das Labor legt nur die Namespaces `vrlab-router` und `vrlab-provider` an und räumt sie am Ende weg
(`sudo test/lab/netns-lab.sh down`, falls ein Lauf abbricht). Gedacht ist es für WSL2 oder einen
Linux-Rechner. Weitere Befehle (`up`, `plug`, `unplug`) stehen im Kopf des Skripts.

> **Stand:** Das Labor ist noch nie vollständig gelaufen (PLAN.md, T-M1-01). Wer es als Erster laufen
> lässt, trägt das Ergebnis dort ein.

### 5.5 Echte Hardware

Der Testplan für echte Hardware steht in [PLAN.md, Abschnitt 7.3](../PLAN.md#73-testplan-für-echte-hardware).
`vr-detect` ohne `-list` schaltet alle Ports ein und sendet DHCP- und PPPoE-Anfragen. Deshalb gilt:
**nie auf einem laufenden Router**, nur auf einem Installationsrechner oder im Labor.

## 6. Ausprobieren ohne Router

### 6.1 Weboberfläche

```bash
test/dev/webui.sh
```

Das Skript baut `vr-webui` und `vrctl`, legt unter `test/dev/work/` eine Beispielkonfiguration an und
startet:

- den **Helper im Probelauf**: Er schreibt erzeugte Dateien nur unter `test/dev/work/root/`, lädt keinen
  Dienst und startet nichts neu,
- den **Webserver mit `--dev`**: HTTP statt HTTPS, nur auf 127.0.0.1.

Dann http://127.0.0.1:9443 öffnen und mit dem Passwort aus `dev/test-credentials.txt` anmelden. Das
Passwort entsteht beim ersten Start zufällig und gilt nur für diese Testinstanz.

| Wunsch | So geht's |
|---|---|
| anderer Port | `test/dev/webui.sh 9500` |
| Einrichtung nach erkanntem PPPoE testen | `VR_DETECTED=pppoe test/dev/webui.sh` |
| sehen, was eine Änderung erzeugt | `test/dev/work/root/etc/…` nach dem Speichern in der Oberfläche |
| Fehlersuche | `test/dev/work/helper.log`, `test/dev/work/serve.log` |
| Frontend geändert | Skript neu starten (die Dateien sind ins Binary eingebettet) |

Live-Werte (Verkehr, Geräte) kommen aus dem Entwicklungsrechner selbst. Schnittstellen wie `wan0` gibt
es dort nicht, die Anzeigen bleiben dann leer. Das ist kein Fehler.

### 6.2 Konfiguration und Erzeuger

```bash
go run ./cmd/vrctl default --wan-mac 02:00:00:00:00:01 --lan-mac 02:00:00:00:00:02 > /tmp/vr-config.json
```

```bash
go run ./cmd/vrctl validate /tmp/vr-config.json
```

```bash
go run ./cmd/vrctl render --config /tmp/vr-config.json --secrets /tmp/vr-secrets --out /tmp/vr-out
```

Danach liegt unter `/tmp/vr-out` alles, was ein Router mit dieser Konfiguration bekäme. MACs mit `02:`
am Anfang sind lokal verwaltete Adressen und gehören zu keinem echten Gerät. `vrctl apply` ohne
`--dry-run` braucht Root und verändert das System. Das gehört nur auf einen Testrouter.

### 6.3 Installations-Assistent

```bash
go run ./cmd/vr-install --dry-run
```

Ohne Root zeigt der Probelauf den ganzen Ablauf mit Schein-Anschlüssen und einer Schein-Festplatte und
gibt am Ende den Plan aus, den er installieren würde. Geschrieben wird nichts. Bedienung wie auf dem
Router: Pfeiltasten und Enter.

### 6.4 Ports des Rechners

```bash
go run ./cmd/vr-detect -list
```

Zeigt die Ethernet-Anschlüsse so, wie der Assistent sie sieht (Name, MAC, Treiber, Bus, Link). Ändert
nichts und sendet nichts.

## 7. Bauen

| Ergebnis | Befehl | Braucht |
|---|---|---|
| einzelnes Programm | `go build -o dist/ ./cmd/vrctl` | Go |
| Paket `dist/vanilla-router_<version>_amd64.deb` | `packaging/build-deb.sh [version]` | `dpkg-dev` |
| Boot-Stick `dist/vanilla-router-stick.raw` | `image/build.sh [version]` | `mkosi`, Root oder User-Namespaces, Internet |
| Stick-Ersteller `dist/vanilla-router-stick.exe` | `packaging/build-stick-exe.sh [version]` | Go (Cross-Compile, auch unter Linux) |
| Logo-Dateien | `go run ./internal/logo/mklogo` | Go |

Hinweise:

- Alle Programme sind statisch (`CGO_ENABLED=0`). Eine Version setzt man mit `-ldflags "-X main.version=…"`,
  die Skripte machen das selbst.
- **Der Boot-Stick ist noch nie gebaut worden** (Stand siehe [image/README.md](../image/README.md)).
  Wer ihn als Erster baut, findet dort die offenen Punkte.
- Den Stick-Ersteller für eine Veröffentlichung baut man mit Download-Adresse und Schlüssel:
  `VR_MANIFEST_URL=… VR_PUBLIC_KEYS=… packaging/build-stick-exe.sh`. Beides ist noch offen (Q-19).
- Das Logo ist Code ([internal/logo](../internal/logo/logo.go)). Wer es ändert, erzeugt die Dateien neu.
  Ein Test schlägt fehl, solange `docs/logo/` und das Favicon nicht zum Code passen.

## 8. Stick-Ersteller (Windows)

Der Stick-Ersteller wird auf Linux gebaut und getestet wie alles andere. Nur das Fenster und der
Laufwerkszugriff brauchen Windows.

**Fenster ansehen, ohne Administratorrechte** (zum Arbeiten an Texten und Aussehen):

```bash
test/stick/preview.sh
```

`dist/vr-stick-preview.exe` unter Windows starten. Schreiben geht mit der Vorschau nicht.

**Schreiben ausprobieren, ohne echtes Abbild:**

```bash
sudo apt install dosfstools
```

```bash
test/stick/test-package.sh
```

Das legt einen Testschlüssel unter `dev/` an und baut in `dist/stick-test/`:

- eine Test-Exe mit diesem Schlüssel,
- ein 64-MB-Test-Abbild (nicht bootfähig, FAT16 mit `vr/stick.json`),
- das signierte Manifest.

Unter Windows: Exe starten, „Abbild-Datei wählen …“, die `.img.gz` nehmen. **Der gewählte Stick wird
vollständig gelöscht.** Danach steht auf dem Stick in `vr\stick.json`, was aus Windows übernommen wurde.

**Tests unter Windows** (Go für Windows installiert, in PowerShell im Repo):

```
go test ./cmd/vr-stick ./internal/windisk ./internal/winres ./internal/windefaults
```

`internal/windisk` liest dabei nur die Liste der Laufwerke und schreibt nichts. Liegt noch eine `.syso`
in `cmd/vr-stick` (siehe [9](#9-häufige-probleme)), verlangt der Test Administratorrechte.

## 9. Häufige Probleme

| Meldung | Ursache und Lösung |
|---|---|
| `go.mod requires go >= 1.26` | Go zu alt. Ubuntu 26.04: `golang-go`, sonst https://go.dev/dl |
| `gofmt` meldet Dateien | `gofmt -w .` |
| Golden-Test schlägt fehl | Erzeuger geändert: `go test ./internal/gen -update`, dann `git diff` lesen (5.2) |
| `TestDateienAktuell` (Logo) schlägt fehl | `go run ./internal/logo/mklogo` |
| `check-main.sh`: `SPDX fehlt` | jede neue `.go`, `.sh`, `.js`, `.css` braucht in den ersten Zeilen `SPDX-License-Identifier: AGPL-3.0-or-later` |
| `check-main.sh`: `UNIVERSE` oder `UNBEKANNT` | ein neues Paket in `image/packages*.txt` kommt nicht aus Ubuntu *main* (NF-09). Ausnahmen nur mit begründeter Entscheidung in PLAN.md |
| `check-main.sh`: `apt-cache fehlt` | kein Ubuntu: Paketquellen werden übersprungen, SPDX wird trotzdem geprüft |
| Testinstanz: `address already in use` | anderer Port: `test/dev/webui.sh 9500` |
| Windows: `go test ./cmd/vr-stick` fragt nach Administratorrechten | eine `.syso` liegt in `cmd/vr-stick` (aus `go generate`): `cmd/vr-stick/*.syso` löschen |
| Skript: `bad interpreter: /usr/bin/env: 'bash\r'` | Die Datei hat CRLF-Zeilenenden (meist vom Editor). Reparieren mit `sed -i 's/\r$//' <datei>`, Editor auf LF stellen |
| Labor: `Root-Rechte nötig` | `sudo test/lab/netns-lab.sh test` |
