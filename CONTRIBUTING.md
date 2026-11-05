# Mitmachen

Danke, dass du etwas beitragen willst. Dieses Dokument sagt, wie ein Beitrag aussieht, der ohne viel Hin
und Her übernommen werden kann.

> **Stand:** frühe Vorabversion. Fehlerberichte und Fragen gern als Issue. Größere Änderungen bitte vorher
> in einem Issue absprechen, damit niemand umsonst arbeitet. Die Regeln unten gelten für alle, auch für
> KI-Agenten.

## Bevor du anfängst

1. [README.md](README.md) für das Ziel, [docs/architektur.md](docs/architektur.md) für den Aufbau,
   [docs/entwicklung.md](docs/entwicklung.md) für die Einrichtung. `test/check-all.sh` muss bei dir grün
   sein, bevor du etwas änderst.
2. [PLAN.md](PLAN.md) ist führend. Prüfe, ob es zu deinem Vorhaben schon eine Anforderung (`F-…`), eine
   Entscheidung (`D-…`) oder eine offene Frage (`Q-…`) gibt.
3. Größere Änderungen erst besprechen (Issue), vor allem alles, was eine Entscheidung berührt.
   Entscheidungen werden nur mit Begründung geändert. Offene Fragen entscheidet der Projektinhaber.

## Leitlinie

**Komfort vor Optimierung, aber nie auf Kosten der Sicherheit** (P-10). Im Zweifel gewinnt das, was für
einen Laien verständlich und für einen Angreifer schwer ist, nicht das, was am wenigsten Speicher braucht.

## Regeln für Code

| Regel | Warum | Kennung |
|---|---|---|
| **Go nur mit der Standardbibliothek**, keine externen Module | keine Lieferkette, ein statisches Binary, lange wartbar | D-01, NF-03 |
| **Frontend ohne Frameworks, Build-Schritt und CDN** | dieselben Gründe, die Oberfläche läuft auch offline | D-01 |
| **SPDX-Zeile** `SPDX-License-Identifier: AGPL-3.0-or-later` am Anfang jeder Quelldatei | Lizenz eindeutig je Datei | NF-08 |
| **Pakete nur aus Ubuntu *main*** (`dnsmasq-base` statt `dnsmasq`) | nur *main* bekommt garantierte Sicherheitsupdates | D-10, NF-09 |
| **Eine Wahrheitsquelle** `config.json`. Dienstdateien werden immer vollständig erzeugt, nie per Textsuche bearbeitet | reproduzierbar, testbar | D-02 |
| **Prüfen vor Einspielen**: erzeugen → mit dem Werkzeug des Dienstes prüfen → atomar ersetzen → neu laden → Gesundheitsprüfung → bei Fehler zurück | ein Fehler darf den Router nicht lahmlegen | P-05 |
| **nftables:** eigene Tabellen je Aufgabe, ersetzen mit `table x; delete table x; table x { … }`, **nie `flush ruleset`** | Docker und andere bringen eigene Tabellen mit | D-03 |
| **Privilegientrennung:** Der Webserver läuft nie als root. Privilegiertes nur über eine feste Helper-Aktion, die ihre Parameter selbst prüft | eine Lücke im Webserver ist nicht sofort root | D-04 |
| **Keine Programme mit Datei-Capabilities** (z. B. `ping`) aus gehärteten Diensten starten, lieber nativ | sie scheitern an der Härtung | L-09 |
| **Frontend:** DOM nur über `h()`, nie `innerHTML` mit Daten. Keine `style`-Attribute, nur `el.style` | XSS, strenge CSP | |
| **Daten aus dem Netz** (Hostnamen, Logzeilen, Dateinamen) sind nicht vertrauenswürdig | | |
| Werkzeugausgaben (apt, ip …) nur mit `LC_ALL=C` auswerten. Fehlen Daten, schlägt die Prüfung fehl | Übersetzungen ändern die Ausgabe | L-17 |

## Tests

- **Jede Prüfregel und jeder Erzeuger** bekommt Unit-Tests, Erzeuger zusätzlich Golden Files (NF-04).
- **Zeit- und Kalenderlogik immer mit Tests** (L-16), die Zeit wird übergeben, nicht gelesen.
- Was das System anfasst, läuft hinter einem Interface und wird im Test ersetzt (siehe
  [entwicklung.md, 5.1](docs/entwicklung.md#51-unit-tests)). `go test ./...` braucht weder Root noch Netz.
- Die Testumgebung hat dieselbe systemd-Härtung wie der Betrieb (NF-05).

## Texte

- Oberfläche, Doku und Kommentare sind derzeit **deutsch** (Q-07 offen). Ausnahmen: `README.md` und
  `SECURITY.md` sind **englisch**, ebenso alles auf GitHub (Commit-Nachrichten, Issues, Pull Requests,
  Releases).
- Texte für Laien **ohne Fachbegriffe**: „Internet-Anschluss“ statt „WAN“, „Heimnetz“ statt „LAN“.
- Beispiele nur mit Dokumentations-Adressen: IPv4 `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`
  (RFC 5737), IPv6 `2001:db8::/32` (RFC 3849), Domain `home.arpa` oder `example.org`, MACs mit `02:`.

## Datenschutz im Repo

Das Projekt ist öffentlich. **Nie** ins Repo: echte MAC-, IP- oder IPv6-Adressen, Hostnamen,
Zugangsdaten, Schlüssel, Backups, Audit-Ergebnisse oder Logs aus einem echten Netz. Testpasswörter und
Testschlüssel gehören nach `dev/` (per `.gitignore` ausgeschlossen).

## Commits

Vor jedem Commit:

```bash
test/check-all.sh
```

- **Eine Sache je Commit.** Code, Tests und die zugehörige Änderung an PLAN.md gehören zusammen.
- **Kennungen nennen**, wo es passt: im Betreff oder Text (`F-INST-03`, `D-05`), ebenso in Kommentaren und Tests.
- Ändert ein Commit den Stand einer Aufgabe, aktualisiert er im selben Commit den Status in PLAN.md,
  dazu `version`, `updated` und den Änderungsverlauf unten in PLAN.md.
- Betreff kurz und auf **Englisch**, z. B. `M4: setup assistant with arrow keys and Enter only`, darunter
  bei Bedarf eine Liste der Änderungen.

## PLAN.md pflegen

- Kennungen (`P-`, `L-`, `F-…-`, `NF-`, `D-`, `Q-`, `R-`, `M`, `T-M…-`, `S-`) sind stabil. Nie
  umnummerieren, nie neu vergeben. Was entfällt, bekommt den Status `verworfen` und bleibt stehen.
- Neue Recherche-Ergebnisse mit Quelle unter `S-NN`. Forenquellen als solche kennzeichnen.
- Offene Fragen: eine Tendenz vorschlagen, aber nicht eigenmächtig umsetzen.

## Lizenz deiner Beiträge

Beiträge stehen unter derselben Lizenz wie das Projekt, [AGPL-3.0-or-later](LICENSE).

## Sicherheitslücken

Nicht als öffentliches Issue melden, siehe [SECURITY.md](SECURITY.md).
