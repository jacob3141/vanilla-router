# vanilla-router

**Ein Router aus einem ganz normalen Linux, für jeden Rechner mit zwei Netzwerkanschlüssen, eingerichtet von einem Boot-Stick, der dich Schritt für Schritt führt.**

> „Stecken Sie jetzt das Kabel vom Modem in einen der Netzwerkanschlüsse.“
> Der Port wird automatisch zum Internet-Anschluss, alle anderen werden dein Heimnetz.

## Warum

Router-Systeme wie OpenWrt sind für winzige Geräte mit wenigen MB Speicher gebaut und deshalb stark
eingeschränkt. Dabei kostet ein gebrauchter Büro-PC oder ein Mini-PC mit mehreren Netzwerkanschlüssen
heute nur noch 100–200 € und braucht kaum Strom. Darauf läuft ein vollwertiges Ubuntu LTS mit allen Paketen
und Sicherheitsupdates. vanilla-router macht daraus einen Router, ohne dass man Linux können muss.

## Grundsätze

- **Unverändertes Ubuntu LTS** statt eines eigenen Router-Betriebssystems
- **Komfort vor Optimierung**: bekannte Werkzeuge und Anleitungen statt des kleinstmöglichen Systems, aber nie auf Kosten der Sicherheit
- **Für Laien**: Boot-Stick, ein paar verständliche Fragen, fertig
- **Sicher ab Werk**: Firewall für IPv4 und IPv6, keine offenen Dienste, automatische Sicherheitsupdates
- **Eine Konfigurationsdatei** als Wahrheitsquelle, alles andere wird daraus erzeugt und vor dem Einspielen geprüft
- **Rücknahme**: Updates und Änderungen lassen sich rückgängig machen
- **Weboberfläche** im Stil gängiger Router mit Geräten, festen Adressen, Kindersicherung, Portweiterleitungen, Online-Monitor und mehr

## Stand

Frühe Planungsphase. Es gibt einen funktionierenden, von Hand eingerichteten Prototyp mit Weboberfläche.
Der Installer entsteht ab jetzt. Plan, Anforderungen und Entscheidungen stehen in [PLAN.md](PLAN.md).

## Lizenz

[GNU Affero General Public License, Version 3 oder später](LICENSE) (`AGPL-3.0-or-later`).
Wer eine veränderte Fassung betreibt und anderen über das Netz zur Verfügung stellt, muss ihnen auch den
Quelltext dazu geben.

## Mitmachen

Noch nicht offen für Beiträge. Der Name wird vor der Veröffentlichung geprüft (PLAN.md, Q-13).
