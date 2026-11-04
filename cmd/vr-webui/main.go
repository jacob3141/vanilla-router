// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-webui ist die Weboberfläche von vanilla-router (M4, aus dem Prototyp routerui übernommen).
//
//	vr-webui serve          HTTPS-Server, läuft als Benutzer vr-webui (ohne Root)
//	vr-webui helper         privilegierter Helper mit fester Aktionsliste, läuft als root
//	vr-webui prepare        als root vor dem Start: TLS-Zertifikat und Einrichtungscode vom Installer
//	vr-webui setup-code     zeigt den Einrichtungscode (bis das Passwort gesetzt ist)
//	vr-webui set-password   setzt das Passwort, liest es von stdin (Notfall-Reset)
//	vr-webui files          Dateidienst für eine Freigabe (Erweiterung, F-ADD)
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vanilla-router/internal/api"
	"vanilla-router/internal/auth"
	"vanilla-router/internal/conf"
	"vanilla-router/internal/config"
	"vanilla-router/internal/files"
	"vanilla-router/internal/gen"
	"vanilla-router/internal/helper"
	"vanilla-router/internal/sys"
	"vanilla-router/web"
)

const (
	dataDir    = "/var/lib/vanilla-router/webui"
	tlsDir     = "/etc/vanilla-router/tls"
	helperSock = "/run/vr-webui/helper.sock"
	filesSock  = "/run/vr-webui-files/files.sock"
	webUser    = "vr-webui"
)

// version und sourceURL setzt der Build (-ldflags -X). sourceURL zeigt auf den Quelltext genau dieser
// Fassung (AGPL §13, F-UI-07). Wer das Programm verändert und anbietet, setzt hier seinen eigenen Ort.
var (
	version   = "dev"
	sourceURL = "https://example.org/vanilla-router"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "helper":
		err = cmdHelper(os.Args[2:])
	case "prepare":
		err = cmdPrepare(os.Args[2:])
	case "setup-code":
		err = cmdSetupCode(os.Args[2:])
	case "set-password":
		err = cmdSetPassword(os.Args[2:])
	case "files":
		err = cmdFiles(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vr-webui:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Aufruf: vr-webui serve|helper|prepare|setup-code|set-password|files [optionen]")
	os.Exit(2)
}

func logger() *slog.Logger {
	// systemd setzt die Zeit selbst, deshalb ohne Zeitstempel
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	}))
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// netInfo sind die Werte aus config.json, die der Webserver beim Start braucht.
type netInfo struct {
	router  netip.Addr
	fqdn    string
	wan     string
	lan     string
	origins []string
}

func loadNet(path string) (netInfo, error) {
	c, err := config.Load(path)
	if err != nil {
		return netInfo{}, err
	}
	conf.SetNetwork(c)
	_, router, _ := c.LANPrefix()
	fqdn := c.System.Hostname + "." + c.DNS.Domain
	return netInfo{router: router, fqdn: fqdn, wan: gen.UplinkIf(c), lan: gen.LANIf,
		origins: []string{"https://" + fqdn, "https://" + router.String()}}, nil
}

func cmdServe(args []string) error {
	fl := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fl.String("config", config.Path, "Konfiguration")
	listen := fl.String("listen", "", "Adresse für HTTPS (Vorgabe: Router-Adresse im Heimnetz :443)")
	redirect := fl.String("redirect", "", "HTTP→HTTPS-Weiterleitung (Vorgabe: Router-Adresse :80, \"aus\" = keine)")
	data := fl.String("data", dataDir, "Datenordner")
	sock := fl.String("helper", helperSock, "Socket des Helpers")
	fsock := fl.String("files", filesSock, "Socket des Dateidienstes")
	origins := fl.String("origins", "", "erlaubte Origins, kommagetrennt (Vorgabe aus der Konfiguration)")
	dev := fl.Bool("dev", false, "Testinstanz: HTTP statt HTTPS, nur für 127.0.0.1 über SSH-Tunnel")
	fl.Parse(args)

	ni, err := loadNet(*cfgPath)
	if err != nil {
		return err
	}
	if *listen == "" {
		*listen = net.JoinHostPort(ni.router.String(), "443")
	}
	if *redirect == "" {
		*redirect = net.JoinHostPort(ni.router.String(), "80")
	}
	if *redirect == "aus" {
		*redirect = ""
	}
	orig := ni.origins
	if *origins != "" {
		orig = strings.Split(*origins, ",")
	}

	log := logger()
	store := auth.NewStore(*data)
	if !store.Configured() {
		if _, err := store.SetupCode(); err != nil {
			return fmt.Errorf("Einrichtungscode: %w", err)
		}
		log.Info("noch kein Passwort gesetzt: Einrichtungscode mit 'sudo vr-webui setup-code' anzeigen")
	}

	static, err := fs.Sub(web.Files, "static")
	if err != nil {
		return err
	}
	sampler := sys.NewWANLANSampler(ni.wan, ni.lan)
	stop := make(chan struct{})
	go sampler.Run(stop, 2*time.Second)
	defer close(stop)

	history := sys.NewHistory(*data)
	monitor := sys.NewMonitor(*data)
	go func() {
		t := time.NewTicker(2 * time.Second)
		for range t.C {
			history.Add(sampler.Live())
		}
	}()
	go monitor.Run(stop)
	go func() {
		for range time.Tick(10 * time.Minute) {
			history.Save()
			monitor.Save()
		}
	}()
	defer history.Save()
	defer monitor.Save()

	sessions := auth.NewSessions(30*time.Minute, 12*time.Hour)
	go func() {
		for range time.Tick(5 * time.Minute) {
			sessions.Cleanup()
		}
	}()

	srv := api.New(api.Config{
		Dev:       *dev,
		Origins:   orig,
		Static:    static,
		Store:     store,
		Sessions:  sessions,
		Limiter:   auth.NewLimiter(),
		Helper:    &helper.Client{Socket: *sock},
		Files:     *fsock,
		Sampler:   sampler,
		History:   history,
		Monitor:   monitor,
		DataDir:   *data,
		Log:       log,
		Version:   version,
		SourceURL: sourceURL,
		RouterIP:  ni.router.String(),
		Name:      ni.fqdn,
		WANIf:     ni.wan,
	})
	srv.Start(stop)

	ctx, cancel := signalContext()
	defer cancel()

	hs := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		// Anfragen erben den Signal-Kontext: Live-Streams (SSE) enden beim Herunterfahren sofort
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	ln, err := listenFreebind(*listen)
	if err != nil {
		return err
	}

	errc := make(chan error, 2)
	if *dev {
		log.Info("testinstanz (HTTP)", "listen", *listen)
		go func() { errc <- hs.Serve(ln) }()
	} else {
		hs.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		log.Info("weboberfläche bereit", "listen", *listen, "name", ni.fqdn)
		go func() {
			errc <- hs.ServeTLS(ln, filepath.Join(tlsDir, "cert.pem"), filepath.Join(tlsDir, "key.pem"))
		}()
		if *redirect != "" {
			go func() { errc <- serveRedirect(*redirect, ni) }()
		}
	}

	select {
	case <-ctx.Done():
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	shutdown, c2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer c2()
	if err := hs.Shutdown(shutdown); err != nil {
		log.Warn("nicht alle Verbindungen rechtzeitig beendet", "err", err)
		hs.Close()
	}
	return nil
}

// listenFreebind erlaubt das Binden an die Router-Adresse, auch wenn lan0 beim Start noch keine hat.
func listenFreebind(addr string) (net.Listener, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_FREEBIND, 1)
		})
		return serr
	}}
	return lc.Listen(context.Background(), "tcp", addr)
}

func serveRedirect(addr string, ni netInfo) error {
	ln, err := listenFreebind(addr)
	if err != nil {
		return err
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != ni.fqdn && host != ni.router.String() {
			host = ni.fqdn // nie auf fremde Namen umleiten (Open Redirect)
		}
		http.Redirect(w, r, "https://"+host+"/", http.StatusMovedPermanently)
	})
	return (&http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}).Serve(ln)
}

func cmdHelper(args []string) error {
	fl := flag.NewFlagSet("helper", flag.ExitOnError)
	sock := fl.String("socket", helperSock, "Socket")
	allow := fl.String("allow-user", webUser, "Benutzer, der den Helper aufrufen darf")
	dryRun := fl.Bool("dry-run", false, "Probelauf: Dateien unter --root schreiben, nichts anwenden")
	root := fl.String("root", "", "Wurzel für den Probelauf")
	fl.Parse(args)

	// Im Probelauf geht es ohne root (Entwicklung): Prüfungen mit nft und dnsmasq entfallen dann.
	if os.Geteuid() != 0 && !*dryRun {
		return fmt.Errorf("der Helper muss als root laufen")
	}
	if *dryRun && *root == "" {
		return fmt.Errorf("--dry-run braucht --root")
	}
	u, err := user.Lookup(*allow)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	ctx, cancel := signalContext()
	defer cancel()
	return helper.NewServer(helper.Config{
		Socket: *sock, AllowUID: uid, SocketGID: gid,
		DryRun: *dryRun, Root: *root, Log: logger(),
	}).Run(ctx)
}

// cmdPrepare läuft als root vor dem Webserver (ExecStartPre=+): legt das TLS-Zertifikat an bzw. erneuert
// es, wenn Name oder Adresse nicht mehr passen, und übernimmt den Einrichtungscode des Installers.
func cmdPrepare(args []string) error {
	fl := flag.NewFlagSet("prepare", flag.ExitOnError)
	cfgPath := fl.String("config", config.Path, "Konfiguration")
	data := fl.String("data", dataDir, "Datenordner")
	fl.Parse(args)
	ni, err := loadNet(*cfgPath)
	if err != nil {
		return err
	}
	u, err := user.Lookup(webUser)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	made, err := ensureCert(tlsDir, ni.fqdn, ni.router, gid)
	if err != nil {
		return fmt.Errorf("TLS-Zertifikat: %w", err)
	}
	if made {
		fmt.Println("Neues TLS-Zertifikat für", ni.fqdn, "und", ni.router)
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		return err
	}
	os.Chown(*data, uid, gid)
	code, err := os.ReadFile("/etc/vanilla-router/secrets/webui-setup-code")
	if err == nil {
		store := auth.NewStore(*data)
		if err := store.ImportSetupCode(string(code)); err != nil {
			return err
		}
		os.Chown(filepath.Join(*data, "setup-code"), uid, gid)
		if store.Configured() {
			os.Remove("/etc/vanilla-router/secrets/webui-setup-code") // erfüllt, nicht aufheben
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	store := auth.NewStore(*data)
	setupCode := ""
	if !store.Configured() {
		if setupCode, err = store.SetupCode(); err != nil {
			return err
		}
		os.Chown(filepath.Join(*data, "setup-code"), uid, gid)
	}
	if err := helper.WriteIssue(helper.IssueFile, ni.fqdn, ni.router.String(), setupCode); err != nil {
		fmt.Fprintln(os.Stderr, "Hinweis auf der Konsole:", err) // nicht wichtig genug, um den Start zu verhindern
	}
	return nil
}

func cmdSetupCode(args []string) error {
	fl := flag.NewFlagSet("setup-code", flag.ExitOnError)
	data := fl.String("data", dataDir, "Datenordner")
	fl.Parse(args)
	store := auth.NewStore(*data)
	if store.Configured() {
		return fmt.Errorf("die Oberfläche ist bereits eingerichtet, es gibt keinen Einrichtungscode mehr")
	}
	code, err := store.SetupCode()
	if err != nil {
		return err
	}
	fmt.Println(code)
	return nil
}

// cmdFiles ist der Dateidienst für eine Freigabe (Erweiterung NAS, F-ADD). Er läuft als Besitzer der
// Freigabe und ist nur über einen Unix-Socket für den Benutzer vr-webui erreichbar.
func cmdFiles(args []string) error {
	fl := flag.NewFlagSet("files", flag.ExitOnError)
	root := fl.String("root", "", "Ordner der Freigabe (Pflicht)")
	sock := fl.String("socket", filesSock, "Socket")
	allow := fl.String("allow-user", webUser, "Benutzer, der verbinden darf")
	fl.Parse(args)
	if *root == "" {
		return errors.New("--root fehlt")
	}

	log := logger()
	u, err := user.Lookup(*allow)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	srv, err := files.New(*root, log)
	if err != nil {
		return err
	}
	os.Remove(*sock)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: *sock, Net: "unix"})
	if err != nil {
		return err
	}
	if err := os.Chown(*sock, -1, gid); err != nil {
		return fmt.Errorf("Gruppe des Sockets: %w", err)
	}
	if err := os.Chmod(*sock, 0o660); err != nil {
		return err
	}
	log.Info("dateidienst bereit", "root", *root, "socket", *sock)

	ctx, cancel := signalContext()
	defer cancel()
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); hs.Close() }()
	if err := hs.Serve(&files.PeerListener{UnixListener: ln, AllowUID: uid, Log: log}); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func cmdSetPassword(args []string) error {
	fl := flag.NewFlagSet("set-password", flag.ExitOnError)
	data := fl.String("data", dataDir, "Datenordner")
	fl.Parse(args)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("kein Passwort auf stdin")
	}
	if err := auth.NewStore(*data).SetPassword(strings.TrimRight(line, "\r\n")); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Passwort gesetzt. Laufende Sitzungen enden beim nächsten Neustart des Dienstes.")
	return nil
}
