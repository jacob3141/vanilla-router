// SPDX-License-Identifier: AGPL-3.0-or-later

package gen

import (
	"fmt"
	"strings"

	"vanilla-router/internal/config"
)

// Share is the folder of the file share, the file service (vr-webui files) and Samba both work on it.
const Share = "/media/shares/guest"

// FileShareConf is the Samba configuration. It has its own place and its own unit (vr-smbd), as dnsmasq and hostapd do:
// the package's /etc/samba/smb.conf and smbd.service stay out of the way.
const FileShareConf = "/etc/vanilla-router/samba/smb.conf"

// FileShareOn says whether the file share is to run.
func FileShareOn(c *config.Config) bool {
	return c != nil && c.Services.Files != nil && c.Services.Files.Enabled
}

// fileShare generates smb.conf: SMB3 with signing and encryption, only in the home network, one share for one user.
// Switched off, there is no file at all.
func fileShare(c *config.Config) []File {
	if !FileShareOn(c) {
		return nil
	}
	lanNet, _, _ := c.LANPrefix()
	var b strings.Builder
	b.WriteString(header("#"))
	fmt.Fprintf(&b, `[global]
   workgroup = WORKGROUP
   server string = %%h
   server role = standalone server
   server min protocol = SMB3
   server signing = mandatory
   smb encrypt = required
   interfaces = lo %[1]s
   bind interfaces only = yes
   hosts allow = 127.0.0.1 %[2]s
   hosts deny = 0.0.0.0/0
   map to guest = never
   load printers = no
   printing = bsd
   printcap name = /dev/null
   disable spoolss = yes
   logging = file
   log file = /var/log/samba/log.%%m
   max log size = 1000

[guest]
   path = %[3]s
   read only = no
   browsable = yes
   valid users = smbuser
   create mask = 0664
   directory mask = 0775
`, LANIf, lanNet, Share)
	return []File{{Path: FileShareConf, Mode: 0o644, Group: GroupFiles, Content: []byte(b.String())}}
}
