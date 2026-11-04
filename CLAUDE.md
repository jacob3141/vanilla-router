# vanilla-router: Anleitung für KI-Agenten

Ein Router aus einem unveränderten Debian auf Standardhardware, mit einem Installer, der Laien Schritt
für Schritt führt. **Führend ist [PLAN.md](PLAN.md).** Lies es vor jeder größeren Aufgabe.

## Mit PLAN.md arbeiten

- IDs (`P-`, `L-`, `F-…-`, `NF-`, `D-`, `Q-`, `R-`, `M`, `T-M…-`, `S-`) sind stabil. Nie umnummerieren,
  nie neu vergeben. Was entfällt, bekommt den Status `verworfen` und bleibt stehen.
- Beziehe dich in Commits, Code-Kommentaren und Tests auf IDs, z. B. `F-INST-03`, `D-05`.
- Ändert eine Aufgabe den Stand, aktualisiere den Status in PLAN.md im selben Commit, dazu `version`,
  `updated` und den Änderungsverlauf.
- Entscheidungen (`D-`) nur mit Begründung ändern. Offene Fragen (`Q-`) entscheidet der Projektinhaber.
  Schlage eine Tendenz vor, aber setze sie nicht eigenmächtig um.
- Neue Recherche-Ergebnisse mit Quelle unter `S-NN` eintragen. Forenquellen als solche kennzeichnen.

## Grundregeln für Code

- **Go nur mit der Standardbibliothek** (D-01, NF-03). Keine externen Module. Frontend ohne Frameworks,
  ohne Build-Schritt, ohne CDN.
- **Eine Wahrheitsquelle** (`config.json`, D-02). Konfigurationsdateien von Diensten werden immer
  vollständig erzeugt, nie per Textsuche bearbeitet.
- **Prüfen vor Einspielen** (P-05): erzeugen → mit dem Werkzeug des Dienstes prüfen → atomar ersetzen →
  neu laden → Gesundheitsprüfung → bei Fehler zurück.
- **nftables:** eigene Tabellen je Aufgabe, ersetzen mit `table x; delete table x; table x { … }`,
  **niemals `flush ruleset`** (D-03). Vergleiche von Regelwerken immer mit `nft -s` (ohne Zähler, L-10).
- **Privilegientrennung** (D-04): Der Webserver läuft nie als root. Alles Privilegierte geht über den
  Helper mit fester Aktionsliste. Jede Aktion prüft ihre Parameter selbst, egal was der Webserver geprüft hat.
- **Keine externen Programme mit Datei-Capabilities** (z. B. `ping`) aus gehärteten Diensten starten,
  native Umsetzung bevorzugen (L-09).
- **Frontend:** DOM nur über die `h()`-Hilfsfunktion, nie `innerHTML` mit Daten. Keine `style`-Attribute
  (CSP), nur `el.style`. Daten aus dem Netz (Hostnamen, Logzeilen) sind nicht vertrauenswürdig.
- **Tests:**
  - Prüfregeln und Generatoren mit Unit-Tests und Golden Files (NF-04).
  - Zeit- und Kalenderlogik immer mit Tests (L-16).
  - Die Testumgebung hat dieselbe systemd-Härtung wie der Betrieb (NF-05).

## Sprache und Texte

- Oberfläche, Doku und Kommentare derzeit auf Deutsch (Q-07 offen). Texte für Laien ohne Fachbegriffe:
  „Internet-Anschluss“ statt „WAN“, „Heimnetz“ statt „LAN“.
- Beispiele nur mit Dokumentations-Adressen: IPv4 `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`
  (RFC 5737), IPv6 `2001:db8::/32` (RFC 3849), Domain `home.arpa` oder `example.org`.

## Datenschutz im Repo

Das Projekt wird öffentlich. **Keine** echten MAC-, IP- oder IPv6-Adressen, Hostnamen, Zugangsdaten,
Schlüssel, Backups, Audit-Ergebnisse oder Logs aus einem echten Netz ins Repo. Der Prototyp liegt in einem
privaten Repo; was von dort übernommen wird, wird vorher bereinigt (PLAN.md 3.11).

## Lokale Hinweise

Umgebungsspezifisches (Werkzeuge auf dem Entwicklungsrechner, Zugang zum Prototyp) steht in
`CLAUDE.local.md`. Die Datei ist per `.gitignore` ausgeschlossen und wird nie committet.
