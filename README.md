<p align="center"><img src="docs/logo/logo.svg" alt="Logo: eine Kugel Vanilleeis im Hörnchen" width="160"></p>

# vanilla-router

**Ein Router aus einem ganz normalen Linux, für jeden Rechner mit zwei Netzwerkanschlüssen, eingerichtet von einem Boot-Stick, der dich Schritt für Schritt führt.**

> „Stecken Sie jetzt das Kabel vom Modem in einen der Netzwerkanschlüsse.“
> Der Port wird automatisch zum Internet-Anschluss, alle anderen werden dein Heimnetz.

## Warum

Router-Systeme wie OpenWrt sind für winzige Geräte mit wenigen MB Speicher gebaut und deshalb stark
eingeschränkt. Dabei kostet ein gebrauchter Büro-PC oder ein Mini-PC mit mehreren Netzwerkanschlüssen
heute nur noch 100–200 € und braucht kaum Strom. Darauf läuft ein vollwertiges Ubuntu LTS mit allen Paketen
und Sicherheitsupdates. vanilla-router macht daraus einen Router, ohne dass man Linux können muss.

## So soll es für Nutzer aussehen

1. **Stick erstellen:** Ein Windows-Programm ohne Installation. USB-Stick einstecken, auswählen, fertig.
   Sprache, Tastatur und Zeitzone nimmt es aus Windows mit.
2. **Installieren:** Den künftigen Router vom Stick starten. Der Assistent fragt nur zwei Dinge: welches
   Kabel ins Modem geht (einfach einstecken, er erkennt es) und welche Festplatte er nehmen darf.
3. **Einrichten:** Im Browser `https://router.home.arpa` öffnen, den Code vom Bildschirm des Routers
   eingeben, Passwort festlegen. Alles andere, zum Beispiel Zugangsdaten des Anbieters, Geräte,
   Portweiterleitungen und Kindersicherung, geht dort.

## Grundsätze

- **Unverändertes Ubuntu LTS** statt eines eigenen Router-Betriebssystems
- **Komfort vor Optimierung**: bekannte Werkzeuge und Anleitungen statt des kleinstmöglichen Systems, aber nie auf Kosten der Sicherheit
- **Sicher ab Werk**: Firewall für IPv4 und IPv6, keine offenen Dienste, automatische Sicherheitsupdates
- **Eine Konfigurationsdatei** als Wahrheitsquelle, alles andere wird daraus erzeugt und vor dem Einspielen geprüft
- **Rücknahme**: Änderungen, die das Netz kaputt machen, und Updates, nach denen der Router nicht mehr läuft, werden automatisch zurückgenommen
- **Keine Abhängigkeiten**: Go mit nur der Standardbibliothek, Frontend ohne Frameworks, Pakete nur aus Ubuntu *main*

## Stand

Entwicklung, **noch nicht einsatzbereit**. Der Code für alle Teile ist geschrieben und mit Unit-Tests
abgedeckt: Konfiguration, Erzeuger, Einspielen mit Rücknahme, Weboberfläche, Assistent, WAN-Erkennung,
Rücknahme nach Updates und Stick-Ersteller. Auf echter Hardware geprüft sind bisher nur die Erkennung der
Anschlüsse und der Stick-Ersteller. Der Boot-Stick ist noch nie gebaut worden. Den genauen Stand zeigt
[docs/architektur.md, Abschnitt 14](docs/architektur.md#14-stand-was-auf-echter-hardware-geprüft-ist),
den Plan [PLAN.md](PLAN.md).

Die Weboberfläche stammt aus einem von Hand eingerichteten Prototyp, der als Router und NAS einen echten
Haushalt versorgt.

## Dokumentation

| Für wen | Was | Wo |
|---|---|---|
| Alle | Ziel, Anforderungen, Entscheidungen, offene Fragen, Roadmap | [PLAN.md](PLAN.md) |
| Entwickler | Aufbau, Idee, Datenfluss, Sicherheitsmodell, wo man anfängt | [docs/architektur.md](docs/architektur.md) |
| Entwickler | Entwicklungsumgebung, Testen, Ausprobieren ohne Router, Bauen | [docs/entwicklung.md](docs/entwicklung.md) |
| Mitwirkende | Regeln für Code, Tests, Texte und Commits | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Sicherheitsforscher | Lücken melden | [SECURITY.md](SECURITY.md) |
| Bauen des Boot-Sticks | Abbilder mit mkosi | [image/README.md](image/README.md) |

## Schnellstart für Entwickler

Auf Ubuntu 26.04 (auch WSL2):

```bash
sudo apt install golang-go git
```

```bash
test/check-all.sh
```

```bash
test/dev/webui.sh
```

Das prüft alles und startet die Weboberfläche lokal auf http://127.0.0.1:9443, ohne Root und ohne den
Rechner zu verändern. Mehr in [docs/entwicklung.md](docs/entwicklung.md).

## Lizenz

[GNU Affero General Public License, Version 3 oder später](LICENSE) (`AGPL-3.0-or-later`).
Wer eine veränderte Fassung betreibt und anderen über das Netz zur Verfügung stellt, muss ihnen auch den
Quelltext dazu geben.

## Mitmachen

Noch nicht offen für Beiträge. Der Name wird vor der Veröffentlichung geprüft (PLAN.md, Q-13). Wie Beiträge
aussehen sollen, steht schon in [CONTRIBUTING.md](CONTRIBUTING.md).
