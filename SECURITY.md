# Sicherheit

vanilla-router steht zwischen dem Internet und einem Heimnetz. Sicherheitslücken nehmen wir deshalb
besonders ernst.

## Lücken melden

> **Noch offen:** Der Meldeweg (Adresse, verschlüsselter Kontakt, ggf. „Private vulnerability reporting“
> der Plattform) wird vor der Veröffentlichung festgelegt (PLAN.md, Q-20). Bis dahin ist das Projekt
> nicht öffentlich.

Bitte melde Lücken nicht als öffentliches Issue. Hilfreich sind:

- betroffene Version oder Commit,
- welcher Teil (Weboberfläche, Helper, Firewall, Installer, Stick-Ersteller …),
- Schritte zum Nachstellen und was ein Angreifer damit erreichen kann.

## Was als Sicherheitslücke gilt

- Zugriff auf den Router oder das Heimnetz **aus dem Internet**, den die Konfiguration nicht erlaubt
  (Firewall, IPv4 wie IPv6).
- Zugriff auf die Weboberfläche **ohne gültige Anmeldung**, Umgehung von CSRF-Schutz, Login-Bremse oder
  Sitzungsablauf.
- Ein Weg vom Webserver (Benutzer `vr-webui`) zu **root**, z. B. über eine Helper-Aktion, die ihre
  Parameter nicht ausreichend prüft.
- Der Dateidienst gibt Dateien außerhalb der Freigabe preis.
- Der Stick-Ersteller schreibt ein Abbild **ohne gültige Signatur** oder auf einen anderen Datenträger
  als einen ausgewählten USB-Stick.
- Geheimnisse (PPPoE-Passwort, Passwort-Hash) werden lesbar für Unbefugte oder landen in einem Export.

Kein Sicherheitsproblem ist, was jemand mit dem Passwort der Oberfläche oder mit Root-Zugang am Router
tun kann: Das ist der Zweck dieser Zugänge.

## Wie das Projekt geschützt ist

Das Sicherheitsmodell mit allen Grenzen steht in
[docs/architektur.md, Abschnitt 12](docs/architektur.md#12-sicherheitsmodell). Kurz: Firewall vor dem
Netzwerk und für IPv4 und IPv6 in einer Tabelle, Webserver ohne root mit gehärteter systemd-Unit,
privilegierte Aktionen nur über einen Helper mit fester Aktionsliste, keine Fremdbibliotheken,
Pakete nur aus Ubuntu *main* mit automatischen Sicherheitsupdates, Snapshot und automatische Rücknahme
nach Updates.
