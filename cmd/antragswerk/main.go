// Antragswerk – Webbasierte Antrags-Workflows für Benutzerverwaltung im Active Directory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"antragswerk/internal/app"
	"antragswerk/internal/config"
	"antragswerk/internal/dir"
	"antragswerk/internal/mail"
	"antragswerk/internal/orgsync"
	"antragswerk/internal/seed"
	"antragswerk/internal/store"
	"antragswerk/internal/web"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "config.yaml", "Pfad zur Konfiguration")
	showVersion := flag.Bool("version", false, "Version anzeigen")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, created, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("Konfiguration", "err", err)
		os.Exit(1)
	}
	if created {
		slog.Info("Demo-Konfiguration angelegt", "datei", *cfgPath)
	}
	st, err := store.Open(cfg.Path(cfg.DBPath))
	if err != nil {
		slog.Error("Datenbank", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := seed.Apply(ctx, st, cfg.Demo, cfg.BaseURL); err != nil {
		slog.Error("Grunddaten", "err", err)
		os.Exit(1)
	}
	d, err := dir.New(cfg)
	if err != nil {
		slog.Error("Verzeichnis", "err", err)
		os.Exit(1)
	}
	a := &app.App{Cfg: cfg, Store: st, Mailer: mail.New(cfg.SMTP)}
	srv, err := web.New(a, d, version)
	if err != nil {
		slog.Error("Oberfläche", "err", err)
		os.Exit(1)
	}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			a.Tick(ctx)
			orgsync.Tick(ctx, st)
			st.PurgeSessions(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	hs := &http.Server{Addr: cfg.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sc)
	}()
	mode := "LDAP"
	if cfg.Demo {
		mode = "Demo"
	}
	slog.Info("Antragswerk gestartet", "listen", cfg.Listen, "url", cfg.BaseURL, "modus", mode, "smtp", cfg.SMTP.Enabled())
	if cfg.TLS.CertFile != "" {
		err = hs.ListenAndServeTLS(cfg.Path(cfg.TLS.CertFile), cfg.Path(cfg.TLS.KeyFile))
	} else {
		err = hs.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Server", "err", err)
		os.Exit(1)
	}
}
