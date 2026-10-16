package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"mserp/internal/fleetreview"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type configuration struct {
	SourceURL         string             `json:"sourceUrl"`
	ReceiverURL       string             `json:"receiverUrl"`
	CompanyID         string             `json:"companyId"`
	CompanyName       string             `json:"companyName"`
	SourceCompanyName string             `json:"sourceCompanyName"`
	DOT               string             `json:"dot"`
	Username          string             `json:"username"`
	PasswordHash      string             `json:"passwordHash"`
	Origin            string             `json:"origin"`
	Directory         string             `json:"directory"`
	Listen            string             `json:"listen"`
	AllowApply        bool               `json:"allowApply"`
	Policy            fleetreview.Policy `json:"policy"`
}

func main() {
	file := flag.String("config", "", "private JSON configuration file")
	provision := flag.Bool("provision-receipts", false, "explicitly install receipt storage in both configured databases")
	credentials := flag.String("generate-credentials", "", "write generated credentials to a private file, then exit")
	flag.Parse()
	if *credentials != "" {
		u, p, h, e := fleetreview.Credentials()
		if e != nil {
			panic(e)
		}
		b, _ := json.MarshalIndent(map[string]string{"username": u, "password": p, "passwordHash": h}, "", "  ")
		f, e := os.OpenFile(*credentials, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			panic(e)
		}
		if _, e = f.Write(b); e != nil {
			panic(e)
		}
		f.Close()
		fmt.Println("Credentials saved to the requested private file")
		return
	}
	b, e := os.ReadFile(*file)
	if e != nil {
		slog.Error("could not read private configuration")
		os.Exit(1)
	}
	var c configuration
	if json.Unmarshal(b, &c) != nil {
		slog.Error("invalid configuration")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	source, e := pgxpool.New(ctx, c.SourceURL)
	if e != nil {
		slog.Error("invalid source connection configuration")
		os.Exit(1)
	}
	defer source.Close()
	receiver, e := pgxpool.New(ctx, c.ReceiverURL)
	if e != nil {
		slog.Error("invalid receiver connection configuration")
		os.Exit(1)
	}
	defer receiver.Close()
	db := &fleetreview.DB{Source: source, Receiver: receiver, CompanyID: c.CompanyID, CompanyName: c.CompanyName, DOT: c.DOT, SourceCompanyName: c.SourceCompanyName}
	if *provision {
		for _, p := range []*pgxpool.Pool{source, receiver} {
			if _, e = p.Exec(ctx, fleetreview.ReceiptSchema); e != nil {
				slog.Error("receipt provisioning failed")
				os.Exit(1)
			}
		}
		fmt.Println("Receipt storage installed in both databases")
		return
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8096"
	}
	secure := true
	if c.Origin == "http://127.0.0.1:8096" || c.Origin == "http://localhost:8096" {
		secure = false
	}
	app, e := fleetreview.NewServer(ctx, db, fleetreview.Config{Origin: c.Origin, Directory: c.Directory, Username: c.Username, PasswordHash: c.PasswordHash, Secure: secure, AllowApply: c.AllowApply, Policy: c.Policy})
	if e != nil {
		slog.Error("portal startup failed; verify database connectivity, schema and private state")
		os.Exit(1)
	}
	srv := &http.Server{Addr: c.Listen, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Info("fleet review portal listening", "address", c.Listen)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("portal HTTP server failed")
		os.Exit(1)
	}
	// Do not stop the worker between database commits during a graceful restart.
	for app.Close() != nil {
		time.Sleep(time.Second)
	}
}
