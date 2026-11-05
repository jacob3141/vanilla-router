// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vanilla-router/internal/manifest"
	"vanilla-router/internal/stickwrite"
	"vanilla-router/internal/windisk"
)

// Sprache der Oberfläche nach der Windows-Sprache (F-STICK-09).
type lang int

const (
	langDE lang = iota
	langEN
)

var uiLang = langDE

// texts: Deutsch und Englisch. Laien-Texte ohne Fachbegriffe (CLAUDE.md, F-STICK-06).
var texts = map[string][2]string{
	"title":   {"vanilla-router: Stick erstellen", "vanilla-router: create USB stick"},
	"heading": {"USB-Stick für Ihren Router erstellen", "Create a USB stick for your router"},
	"intro": {"Stecken Sie einen USB-Stick mit mindestens 4 GB ein und wählen Sie ihn aus. Alles, was darauf gespeichert ist, wird gelöscht.",
		"Plug in a USB stick with at least 4 GB and select it. Everything stored on it will be erased."},
	"sticks":       {"USB-Stick:", "USB stick:"},
	"no_sticks":    {"Kein USB-Stick gefunden. Stecken Sie einen ein, die Liste aktualisiert sich von selbst.", "No USB stick found. Plug one in, the list updates by itself."},
	"drive":        {"Laufwerk", "drive"},
	"btn_create":   {"Stick erstellen", "Create stick"},
	"btn_cancel":   {"Abbrechen", "Cancel"},
	"btn_close":    {"Schließen", "Close"},
	"btn_file":     {"Abbild-Datei wählen …", "Choose image file …"},
	"btn_online":   {"Aktuelles Abbild laden", "Use latest image"},
	"file_filter":  {"Abbild-Dateien", "Image files"},
	"ready":        {"Bereit.", "Ready."},
	"ready_local":  {"Abbild-Datei: %s (Version %s)", "Image file: %s (version %s)"},
	"ph_manifest":  {"Suche das aktuelle Abbild …", "Looking for the latest image …"},
	"ph_download":  {"Lade das Abbild herunter: %s von %s", "Downloading the image: %s of %s"},
	"ph_check":     {"Prüfe das Abbild …", "Checking the image …"},
	"ph_open":      {"Bereite den Stick vor …", "Preparing the stick …"},
	"ph_write":     {"Schreibe auf den Stick. Bitte nicht abziehen.", "Writing to the stick. Please do not unplug it."},
	"ph_verify":    {"Kontrolliere den Stick. Bitte nicht abziehen.", "Verifying the stick. Please do not unplug it."},
	"offline_note": {"Keine Verbindung zum Internet. Ich verwende das zuletzt geladene Abbild (Version %s).", "No internet connection. Using the last downloaded image (version %s)."},
	"done": {"Fertig! Ziehen Sie den Stick ab und stecken Sie ihn in den Rechner, der Ihr Router werden soll.\r\n\r\n" +
		"So starten Sie vom Stick: Rechner einschalten und sofort mehrmals die Taste für das Boot-Menü drücken, meist F12, F11, F8 oder Esc. Dort den USB-Stick wählen.",
		"Done! Unplug the stick and plug it into the computer that will become your router.\r\n\r\n" +
			"To start from the stick: switch the computer on and immediately press the boot menu key several times, usually F12, F11, F8 or Esc. Choose the USB stick there."},
	"confirm_title": {"Stick löschen?", "Erase stick?"},
	"confirm": {"Alle Daten auf „%s“ werden gelöscht.\r\n\r\nFortfahren?",
		"All data on \"%s\" will be erased.\r\n\r\nContinue?"},
	"confirm_large": {"„%s“ ist für einen USB-Stick ungewöhnlich groß. Vielleicht ist es eine externe Festplatte mit Ihren Daten.\r\n\r\nIst das wirklich der richtige Datenträger?",
		"\"%s\" is unusually large for a USB stick. It may be an external hard disk with your data.\r\n\r\nIs this really the right drive?"},
	"err_title":      {"Stick erstellen", "Create stick"},
	"too_small":      {"Der Stick ist zu klein. Er braucht mindestens %s, hat aber nur %s.", "The stick is too small. It needs at least %s but has only %s."},
	"err_protected":  {"Der Stick ist schreibgeschützt. Manche Sticks haben dafür einen kleinen Schalter an der Seite.", "The stick is write-protected. Some sticks have a small switch on the side for this."},
	"err_removed":    {"Der Stick wurde abgezogen oder antwortet nicht mehr. Stecken Sie ihn wieder ein und versuchen Sie es noch einmal.", "The stick was unplugged or stopped responding. Plug it back in and try again."},
	"err_busy":       {"Der Stick wird noch von einem anderen Programm benutzt. Schließen Sie geöffnete Dateien und Fenster vom Stick und versuchen Sie es noch einmal.", "The stick is still in use by another program. Close open files and windows from the stick and try again."},
	"err_changed":    {"Der Stick hat sich verändert, seit er ausgewählt wurde. Wählen Sie ihn bitte noch einmal aus.", "The stick changed since it was selected. Please select it again."},
	"err_admin":      {"Dem Programm fehlen Administratorrechte. Starten Sie es neu und bestätigen Sie die Frage von Windows.", "The program lacks administrator rights. Restart it and confirm the Windows prompt."},
	"err_corrupt":    {"Das Abbild ist beschädigt. Es wird beim nächsten Versuch neu heruntergeladen.", "The image is damaged. It will be downloaded again on the next attempt."},
	"err_corrupt_lc": {"Die Abbild-Datei ist beschädigt oder passt nicht zum Manifest. Laden Sie sie noch einmal herunter.", "The image file is damaged or does not match the manifest. Download it again."},
	"err_notblank":   {"Dieses Abbild ist nicht für den Stick-Ersteller vorbereitet.", "This image is not prepared for the stick creator."},
	"err_verify":     {"Die Kontrolle nach dem Schreiben ist fehlgeschlagen. Der Stick ist vielleicht defekt. Versuchen Sie es noch einmal oder mit einem anderen Stick.", "Verification after writing failed. The stick may be faulty. Try again or use another stick."},
	"err_offline":    {"Keine Verbindung zum Download. Prüfen Sie die Internetverbindung und versuchen Sie es noch einmal.", "Cannot reach the download. Check the internet connection and try again."},
	"err_download":   {"Der Download wurde unterbrochen. Beim nächsten Versuch geht es an derselben Stelle weiter.", "The download was interrupted. The next attempt continues where it stopped."},
	"err_nosource":   {"In dieser Fassung ist keine Download-Adresse eingebaut. Wählen Sie eine Abbild-Datei.", "This build has no download address. Choose an image file."},
	"err_nokeys":     {"In dieser Fassung ist kein Schlüssel zum Prüfen eingebaut. Laden Sie das Programm von der Projektseite.", "This build has no verification key. Download the program from the project page."},
	"err_signature":  {"Das Abbild ist nicht vom Projekt signiert und wird nicht geschrieben.", "The image is not signed by the project and will not be written."},
	"err_local":      {"Neben der Abbild-Datei fehlen manifest.json und manifest.json.sig von der Download-Seite.", "manifest.json and manifest.json.sig from the download page are missing next to the image file."},
	"err_mismatch":   {"Die Abbild-Datei passt nicht zu manifest.json. Laden Sie beide noch einmal herunter.", "The image file does not match manifest.json. Download both again."},
	"err_onstick":    {"Die Abbild-Datei liegt auf dem Stick, der gelöscht werden soll. Kopieren Sie sie zuerst auf den Rechner.", "The image file is on the stick that is about to be erased. Copy it to the computer first."},
	"confirm_cancel": {"Vorgang abbrechen? Ein angefangener Stick muss danach neu erstellt werden.", "Cancel? A partly written stick must be created again afterwards."},
	"canceled":       {"Abgebrochen.", "Canceled."},
	"canceled_wrote": {"Abgebrochen. Der Stick ist unvollständig und muss neu erstellt werden.", "Canceled. The stick is incomplete and must be created again."},
	"err_other":      {"Es ist ein Fehler aufgetreten: %v", "An error occurred: %v"},
	"incomplete":     {"\r\n\r\nDer Stick ist unvollständig. Bitte erstellen Sie ihn noch einmal.", "\r\n\r\nThe stick is incomplete. Please create it again."},
}

// tr liefert einen Text in der Sprache der Oberfläche.
func tr(key string, args ...any) string {
	t, ok := texts[key]
	if !ok {
		return key
	}
	if len(args) == 0 {
		return t[uiLang]
	}
	return fmt.Sprintf(t[uiLang], args...)
}

// size schreibt Größen wie die Hersteller (1 GB = 10⁹ Bytes), mit Komma im Deutschen.
func size(b int64) string {
	var s string
	switch {
	case b >= 10e9:
		s = fmt.Sprintf("%.0f GB", float64(b)/1e9)
	case b >= 1e9:
		s = fmt.Sprintf("%.1f GB", float64(b)/1e9)
	default:
		s = fmt.Sprintf("%.0f MB", float64(b)/1e6)
	}
	if uiLang == langDE {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// diskLabel ist der Eintrag in der Liste und in der Warnung, z. B. „SanDisk Ultra (32 GB), Laufwerk E:“.
func diskLabel(d windisk.Disk) string {
	s := fmt.Sprintf("%s (%s)", d.Name(), size(d.Size))
	if len(d.Letters) > 0 {
		s += ", " + tr("drive") + " " + strings.Join(d.Letters, " ")
	}
	return s
}

// errText übersetzt einen Fehler in einen Laien-Text (F-STICK-06).
func errText(err error, local, wrote bool) string {
	var s string
	switch {
	case errors.Is(err, context.Canceled):
		if wrote {
			return tr("canceled_wrote")
		}
		return tr("canceled")
	case errors.Is(err, windisk.ErrWriteProtected):
		s = tr("err_protected")
	case errors.Is(err, windisk.ErrRemoved):
		s = tr("err_removed")
	case errors.Is(err, windisk.ErrBusy):
		s = tr("err_busy")
	case errors.Is(err, windisk.ErrChanged):
		s = tr("err_changed")
	case errors.Is(err, windisk.ErrNotAdmin):
		s = tr("err_admin")
	case errors.Is(err, stickwrite.ErrCorrupt) && local:
		s = tr("err_corrupt_lc")
	case errors.Is(err, stickwrite.ErrCorrupt):
		s = tr("err_corrupt")
	case errors.Is(err, stickwrite.ErrNotBlank):
		s = tr("err_notblank")
	case errors.Is(err, stickwrite.ErrVerify):
		s = tr("err_verify")
	case errors.Is(err, errOffline):
		s = tr("err_offline")
	case errors.Is(err, errDownload):
		s = tr("err_download")
	case errors.Is(err, errNoSource):
		s = tr("err_nosource")
	case errors.Is(err, errNoKeys):
		s = tr("err_nokeys")
	case errors.Is(err, manifest.ErrSignature):
		s = tr("err_signature")
	case errors.Is(err, errLocalManifest):
		s = tr("err_local")
	case errors.Is(err, errLocalMismatch):
		s = tr("err_mismatch")
	case errors.Is(err, errOnStick):
		s = tr("err_onstick")
	default:
		s = tr("err_other", err)
	}
	if wrote && !errors.Is(err, stickwrite.ErrVerify) {
		s += tr("incomplete")
	}
	return s
}
