// SPDX-License-Identifier: AGPL-3.0-or-later

// vr-webui is the web interface of vanilla-router (M4, taken over from the prototype routerui).
//
//	vr-webui serve          HTTPS server, runs as user vr-webui (no root)
//	vr-webui helper         privileged helper with a fixed list of actions, runs as root
//	vr-webui prepare        as root before start: TLS certificate and setup code from the installer
//	vr-webui setup-code     shows the setup code (until the password is set)
//	vr-webui set-password   sets the password, reads it from stdin (emergency reset)
//	vr-webui files          file service for a share (extension, F-ADD)
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

// version and sourceURL are set by the build (-ldflags -X). sourceURL points to the source of exactly
// this version (AGPL §13, F-UI-07). Anyone who changes and offers the program sets their own location here.
var (
	version   = "dev"
	sourceURL = "https://github.com/jacob3141/vanilla-router"
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
	fmt.Fprintln(os.Stderr, "Usage: vr-webui serve|helper|prepare|setup-code|set-password|files [options]")
	os.Exit(2)
}

func logger() *slog.Logger {
	// systemd adds the time itself, hence no timestamp
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

// netInfo holds the values from config.json that the web server needs at start.
type netInfo struct {
	router  netip.Addr
	fqdn    string
	wan     string
	lan     string
	origins []string
	locale  string
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
		origins: []string{"https://" + fqdn, "https://" + router.String()}, locale: c.System.Locale}, nil
}

func cmdServe(args []string) error {
	fl := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fl.String("config", config.Path, "configuration")
	listen := fl.String("listen", "", "HTTPS address (default: router address in the home network :443)")
	redirect := fl.String("redirect", "", "HTTP→HTTPS redirect (default: router address :80, \"off\" = none)")
	data := fl.String("data", dataDir, "data folder")
	sock := fl.String("helper", helperSock, "socket of the helper")
	fsock := fl.String("files", filesSock, "socket of the file service")
	origins := fl.String("origins", "", "allowed origins, comma-separated (default from the configuration)")
	dev := fl.Bool("dev", false, "test instance: HTTP instead of HTTPS, only for 127.0.0.1 through an SSH tunnel")
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
	if *redirect == "off" {
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
			return fmt.Errorf("setup code: %w", err)
		}
		log.Info("no password set yet: show the setup code with 'sudo vr-webui setup-code'")
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
		Locale:    ni.locale,
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
		// Requests inherit the signal context: live streams (SSE) end immediately on shutdown
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	ln, err := listenFreebind(*listen)
	if err != nil {
		return err
	}

	errc := make(chan error, 2)
	if *dev {
		log.Info("test instance (HTTP)", "listen", *listen)
		go func() { errc <- hs.Serve(ln) }()
	} else {
		hs.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		log.Info("web interface ready", "listen", *listen, "name", ni.fqdn)
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
		log.Warn("not all connections ended in time", "err", err)
		hs.Close()
	}
	return nil
}

// listenFreebind allows binding to the router address even if lan0 has none yet at start.
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
			host = ni.fqdn // never redirect to foreign names (open redirect)
		}
		http.Redirect(w, r, "https://"+host+"/", http.StatusMovedPermanently)
	})
	return (&http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}).Serve(ln)
}

func cmdHelper(args []string) error {
	fl := flag.NewFlagSet("helper", flag.ExitOnError)
	sock := fl.String("socket", helperSock, "Socket")
	allow := fl.String("allow-user", webUser, "user allowed to call the helper")
	dryRun := fl.Bool("dry-run", false, "dry run: write files under --root, apply nothing")
	root := fl.String("root", "", "root for the dry run")
	fl.Parse(args)

	// In a dry run it works without root (development): the checks with nft and dnsmasq are then skipped.
	if os.Geteuid() != 0 && !*dryRun {
		return fmt.Errorf("the helper must run as root")
	}
	if *dryRun && *root == "" {
		return fmt.Errorf("--dry-run needs --root")
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

// cmdPrepare runs as root before the web server (ExecStartPre=+): creates or renews the TLS certificate
// when the name or address no longer match, and takes over the installer's setup code.
func cmdPrepare(args []string) error {
	fl := flag.NewFlagSet("prepare", flag.ExitOnError)
	cfgPath := fl.String("config", config.Path, "configuration")
	data := fl.String("data", dataDir, "data folder")
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
		return fmt.Errorf("TLS certificate: %w", err)
	}
	if made {
		fmt.Println("New TLS certificate for", ni.fqdn, "and", ni.router)
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
			os.Remove("/etc/vanilla-router/secrets/webui-setup-code") // fulfilled, do not keep it
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
	if err := helper.WriteIssue(helper.IssueFile, ni.fqdn, ni.router.String(), setupCode, ni.locale); err != nil {
		fmt.Fprintln(os.Stderr, "Note on the console:", err) // not important enough to prevent the start
	}
	return nil
}

func cmdSetupCode(args []string) error {
	fl := flag.NewFlagSet("setup-code", flag.ExitOnError)
	data := fl.String("data", dataDir, "data folder")
	fl.Parse(args)
	store := auth.NewStore(*data)
	if store.Configured() {
		return fmt.Errorf("the web interface is already set up, there is no setup code anymore")
	}
	code, err := store.SetupCode()
	if err != nil {
		return err
	}
	fmt.Println(code)
	return nil
}

// cmdFiles is the file service for a share (NAS extension, F-ADD). It runs as the owner of the
// share and is reachable only through a Unix socket for the user vr-webui.
func cmdFiles(args []string) error {
	fl := flag.NewFlagSet("files", flag.ExitOnError)
	root := fl.String("root", "", "folder of the share (required)")
	sock := fl.String("socket", filesSock, "Socket")
	allow := fl.String("allow-user", webUser, "user allowed to connect")
	fl.Parse(args)
	if *root == "" {
		return errors.New("--root is missing")
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
		return fmt.Errorf("group of the socket: %w", err)
	}
	if err := os.Chmod(*sock, 0o660); err != nil {
		return err
	}
	log.Info("file service ready", "root", *root, "socket", *sock)

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
	data := fl.String("data", dataDir, "data folder")
	fl.Parse(args)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("no password on stdin")
	}
	if err := auth.NewStore(*data).SetPassword(strings.TrimRight(line, "\r\n")); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Password set. Running sessions end at the next restart of the service.")
	return nil
}
