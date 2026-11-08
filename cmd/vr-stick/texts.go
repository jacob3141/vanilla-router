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

// texts: plain English without jargon (CLAUDE.md, F-STICK-06).
var texts = map[string]string{
	"title":        "vanilla-router: create USB stick",
	"heading":      "Create a USB stick for your router",
	"intro":        "Plug in a USB stick with at least 4 GB and select it. Everything stored on it will be erased.",
	"sticks":       "USB stick:",
	"no_sticks":    "No USB stick found. Plug one in, the list updates by itself.",
	"drive":        "drive",
	"btn_create":   "Create stick",
	"btn_cancel":   "Cancel",
	"btn_close":    "Close",
	"btn_file":     "Choose image file …",
	"btn_online":   "Use latest image",
	"file_filter":  "Image files",
	"ready":        "Ready.",
	"ready_local":  "Image file: %s (version %s)",
	"ph_manifest":  "Looking for the latest image …",
	"ph_download":  "Downloading the image: %s of %s",
	"ph_check":     "Checking the image …",
	"ph_open":      "Preparing the stick …",
	"ph_write":     "Writing to the stick. Please do not unplug it.",
	"ph_verify":    "Verifying the stick. Please do not unplug it.",
	"offline_note": "No internet connection. Using the last downloaded image (version %s).",
	"done": "Done! Unplug the stick and plug it into the computer that will become your router.\r\n\r\n" +
		"To start from the stick: switch the computer on and immediately press the boot menu key several times, usually F12, F11, F8 or Esc. Choose the USB stick there.",
	"confirm_title":  "Erase stick?",
	"confirm":        "All data on \"%s\" will be erased.\r\n\r\nContinue?",
	"confirm_large":  "\"%s\" is unusually large for a USB stick. It may be an external hard disk with your data.\r\n\r\nIs this really the right drive?",
	"err_title":      "Create stick",
	"too_small":      "The stick is too small. It needs at least %s but has only %s.",
	"err_protected":  "The stick is write-protected. Some sticks have a small switch on the side for this.",
	"err_removed":    "The stick was unplugged or stopped responding. Plug it back in and try again.",
	"err_busy":       "The stick is still in use by another program. Close open files and windows from the stick and try again.",
	"err_changed":    "The stick changed since it was selected. Please select it again.",
	"err_admin":      "The program lacks administrator rights. Restart it and confirm the Windows prompt.",
	"err_corrupt":    "The image is damaged. It will be downloaded again on the next attempt.",
	"err_corrupt_lc": "The image file is damaged or does not match the manifest. Download it again.",
	"err_notblank":   "This image is not prepared for the stick creator.",
	"err_verify":     "Verification after writing failed. The stick may be faulty. Try again or use another stick.",
	"err_offline":    "Cannot reach the download. Check the internet connection and try again.",
	"err_download":   "The download was interrupted. The next attempt continues where it stopped.",
	"err_nosource":   "This build has no download address. Choose an image file.",
	"err_nokeys":     "This build has no verification key. Download the program from the project page.",
	"err_signature":  "The image is not signed by the project and will not be written.",
	"err_local":      "manifest.json and manifest.json.sig from the download page are missing next to the image file.",
	"err_mismatch":   "The image file does not match manifest.json. Download both again.",
	"err_onstick":    "The image file is on the stick that is about to be erased. Copy it to the computer first.",
	"confirm_cancel": "Cancel? A partly written stick must be created again afterwards.",
	"canceled":       "Canceled.",
	"canceled_wrote": "Canceled. The stick is incomplete and must be created again.",
	"err_other":      "An error occurred: %v",
	"incomplete":     "\r\n\r\nThe stick is incomplete. Please create it again.",
}

// tr returns a text.
func tr(key string, args ...any) string {
	t, ok := texts[key]
	if !ok {
		return key
	}
	if len(args) == 0 {
		return t
	}
	return fmt.Sprintf(t, args...)
}

// size writes sizes the way manufacturers do (1 GB = 10⁹ bytes).
func size(b int64) string {
	switch {
	case b >= 10e9:
		return fmt.Sprintf("%.0f GB", float64(b)/1e9)
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(b)/1e9)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/1e6)
}

// diskLabel is the entry in the list and in the warning, e.g. "SanDisk Ultra (32 GB), drive E:".
func diskLabel(d windisk.Disk) string {
	s := fmt.Sprintf("%s (%s)", d.Name(), size(d.Size))
	if len(d.Letters) > 0 {
		s += ", " + tr("drive") + " " + strings.Join(d.Letters, " ")
	}
	return s
}

// errText translates an error into a plain-language text (F-STICK-06).
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
