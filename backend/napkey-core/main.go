// Command napkey-core is the NapKey control plane: users, sessions, API keys, and
// the push of those keys to the kiro-go data plane.
//
// It owns Postgres. kiro-go stays the data plane and keeps serving proxied traffic
// on its own, so a restart here does not interrupt a customer's stream.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"napkey-core/internal/config"
	"napkey-core/internal/httpapi"
	"napkey-core/internal/dataplane"
	"napkey-core/internal/kiro"
	"napkey-core/internal/logger"
	"napkey-core/internal/mail"
	"napkey-core/internal/payments"
	"napkey-core/internal/store"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations and exit")
	flag.Parse()

	if err := run(*migrateOnly); err != nil {
		logger.Errorf("%v", err)
		os.Exit(1)
	}
}

func run(migrateOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.SetLevel(logger.ParseLevel(cfg.LogLevel))

	logger.Infof("napkey-core starting")
	logger.Infof("database: %s", cfg.RedactedDatabaseURL())
	logger.Infof("data plane: %s", cfg.KiroAdminURL)
	logger.Infof("console origin: %s", cfg.PublicBaseURL)
	if cfg.MailProvider == "log" {
		// Left unmissable: with this setting nobody receives a verification email
		// and signup can only be completed by reading the log.
		logger.Warnf("MAIL_PROVIDER=log: verification emails are written to this log, not delivered")
	}
	if len(cfg.AdminEmails) == 0 {
		logger.Warnf("ADMIN_EMAILS is empty, so no account can reach the admin endpoints")
	}
	if !cfg.SecureCookies {
		logger.Warnf("SECURE_COOKIES=false: session cookies will be sent over plain http, use this only for local development")
	}

	// A generous ceiling for startup; migrations on a cold database can take a
	// while, and failing fast here would just crash-loop.
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelStartup()

	st, err := store.Open(startupCtx, cfg.DatabaseURL, cfg.MaxOpenConns, cfg.MaxIdleConns)
	if err != nil {
		return err
	}
	defer st.Close()
	logger.Infof("connected to Postgres")

	if err := store.Migrate(startupCtx, st.DB()); err != nil {
		return err
	}
	if migrateOnly {
		logger.Infof("migrations complete, exiting as requested")
		return nil
	}

	kiroClient := kiro.New(cfg.KiroAdminURL, cfg.KiroAdminPassword)
	// Checked at startup so a bad admin password surfaces in the deploy log rather
	// than on the first customer who tries to create a key.
	probeCtx, cancelProbe := context.WithTimeout(startupCtx, 10*time.Second)
	if err := kiroClient.Health(probeCtx); err != nil {
		if errors.Is(err, dataplane.ErrUnauthorized) {
			cancelProbe()
			return fmt.Errorf("the data plane rejected KIRO_ADMIN_PASSWORD; key provisioning would fail for every user")
		}
		logger.Warnf("data plane is not reachable yet (%v); key creation will fail until it is", err)
	} else {
		logger.Infof("data plane reachable")
	}
	cancelProbe()

	var mailer mail.Sender
	switch cfg.MailProvider {
	case "smtp":
		mailer = &mail.SMTPSender{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUser,
			Password: cfg.SMTPPassword,
			From:     cfg.MailFrom,
		}
	default:
		mailer = mail.LogSender{}
	}

	server := httpapi.New(cfg, st, kiroClient, mailer)

	httpServer := &http.Server{
		Addr:    cfg.Addr(),
		Handler: server.Handler(),
		// Timeouts are set explicitly because Go's defaults are none, and a
		// connection that never finishes its headers would otherwise hold a
		// goroutine forever.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// Signal handling has to be in place before the workers start, so a fast
	// Ctrl-C during startup still shuts down cleanly.
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	syncer := dataplane.NewSyncer(st, kiroClient, cfg.KiroSyncInterval)
	go syncer.Run(rootCtx)
	go payments.NewWorker(st).Run(rootCtx)
	go payments.NewReconciler(st,cfg.CassoAPIKey).Run(rootCtx)
	go runJanitor(rootCtx, st)
	go runWalletReconciler(rootCtx, st)

	serverErr := make(chan error, 1)
	go func() {
		logger.Infof("listening on %s", cfg.Addr())
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-rootCtx.Done():
		logger.Infof("shutdown signal received, draining connections")
	}

	// Drain in-flight requests instead of cutting them off. A request that is
	// mid-transaction should be allowed to commit or roll back on its own.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	logger.Infof("napkey-core stopped")
	return nil
}

func runWalletReconciler(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		count, err := st.ReconcileWalletBalances(ctx)
		if err != nil {
			logger.Warnf("wallet balance reconciliation failed: %v", err)
		} else if count > 0 {
			logger.Errorf("wallet balance reconciliation found drift in %d wallets", count)
		}
		if err := st.RefreshOperationsAlerts(ctx); err != nil {
			logger.Warnf("refreshing operations alerts failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runJanitor prunes expired rows.
//
// Sessions and tokens are filtered by expiry in SQL on every read, so this is
// housekeeping to keep the tables small, not a correctness requirement. That is
// why a failure only logs.
func runJanitor(ctx context.Context, st *store.Store) {
	const interval = time.Hour
	sweep := func() {
		sweepCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		if n, err := st.PruneSessions(sweepCtx); err != nil {
			logger.Warnf("pruning expired sessions failed: %v", err)
		} else if n > 0 {
			logger.Infof("pruned %d expired session(s)", n)
		}
		if n, err := st.PruneEmailTokens(sweepCtx); err != nil {
			logger.Warnf("pruning email tokens failed: %v", err)
		} else if n > 0 {
			logger.Infof("pruned %d stale email token(s)", n)
		}
		// Kept longer than the rate-limit window so the counters stay meaningful.
		if n, err := st.PruneAuthAttempts(sweepCtx, 24*time.Hour); err != nil {
			logger.Warnf("pruning auth attempts failed: %v", err)
		} else if n > 0 {
			logger.Debugf("pruned %d old auth attempt(s)", n)
		}
		if n, err := st.ReleaseExpiredHolds(sweepCtx, 500); err != nil {
			logger.Warnf("releasing expired wallet holds failed: %v", err)
		} else if n > 0 {
			logger.Infof("released %d expired wallet hold(s)", n)
		}
		if n, err := st.ExpirePromotionalCredits(sweepCtx, 500); err != nil {
			logger.Warnf("expiring promotional credits failed: %v", err)
		} else if n > 0 {
			logger.Infof("processed %d expired promotional wallet(s)", n)
		}
		if n,err:=st.CountStaleUnmatchedPayments(sweepCtx,30*time.Minute);err!=nil{logger.Warnf("checking unmatched Casso payments failed: %v",err)}else if n>0{logger.Warnf("%d Casso payment(s) have been unmatched for more than 30 minutes",n)}
	}

	sweep()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
