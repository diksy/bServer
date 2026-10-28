package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/pkg/browser"

	"bserver/internal/config"
	"bserver/internal/httpapi"
	"bserver/internal/metrics"
	"bserver/internal/sftpx"
	"bserver/internal/sshx"
	"bserver/internal/store"
	"bserver/internal/svcs"
	"bserver/internal/vault"
	"bserver/web"
)

func main() {
	var (
		port   = flag.Int("port", 0, "port HTTP (0 = acak)")
		dataD  = flag.String("data", "", "direktori data (default <dir-exe>/data)")
		noOpen = flag.Bool("no-open", false, "jangan buka browser otomatis")
		mIv    = flag.Duration("metric-interval", 60*time.Second, "interval sampling riwayat metrik")
	)
	flag.Parse()

	cfg, err := config.New(*dataD)
	if err != nil {
		log.Fatalf("data dir: %v", err)
	}

	// Cegah dua instance memakai data dir yang sama.
	lock, err := config.AcquireLock(cfg.DataDir)
	if err != nil {
		if errors.Is(err, config.ErrLocked) {
			log.Fatalf("%v. Tutup instance tersebut atau jalankan dengan --data lain.", err)
		}
		log.Fatalf("lock data dir: %v", err)
	}
	defer lock.Release()

	v := vault.New(cfg.Path("vault.json"))

	db, err := store.New(cfg.Path("app.db"), v)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	// Token sesi tidak boleh bocor ke command_log.
	db.SetSensitive(cfg.Token)

	if m, err := strconv.Atoi(db.Setting("autoLockMin", "10")); err == nil && m > 0 {
		v.SetAutoLock(time.Duration(m) * time.Minute)
	}

	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatalf("aset UI: %v", err)
	}

	dialer := &sshx.Dialer{Store: db}
	api := &httpapi.API{
		Cfg:     cfg,
		Vault:   v,
		Store:   db,
		Dialer:  dialer,
		Sftp:    &sftpx.Lister{Dialer: dialer, Store: db},
		Svcs:    &svcs.Lister{Dialer: dialer, Store: db},
		Metrics: &metrics.Collector{Dialer: dialer, Store: db},
		Assets:  sub,
		Start:   time.Now(),
	}

	// Sampler riwayat metrik: jalan di background, dilewati saat vault terkunci.
	sampler := &metrics.Sampler{
		Store:     db,
		Collector: api.Metrics,
		Vault:     v,
		Interval:  *mIv,
	}
	samplerCtx, samplerCancel := context.WithCancel(context.Background())
	defer samplerCancel()
	go sampler.Run(samplerCtx)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	realPort := ln.Addr().(*net.TCPAddr).Port
	api.Port = realPort // dipakai untuk validasi Host/Origin
	url := fmt.Sprintf("http://127.0.0.1:%d/#t=%s", realPort, cfg.Token)

	runtime := fmt.Sprintf(`{"url":%q,"port":%d,"token":%q,"pid":%d,"started_at":%q}`,
		fmt.Sprintf("http://127.0.0.1:%d/", realPort), realPort, cfg.Token, os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(cfg.Path("runtime.json"), []byte(runtime), 0o600); err != nil {
		log.Printf("peringatan: gagal menulis runtime.json: %v", err)
	}

	srv := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	fmt.Printf("\n  bServer prototype berjalan\n  → %s\n  data: %s\n\n", url, cfg.DataDir)
	if !*noOpen {
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := browser.OpenURL(url); err != nil {
				log.Printf("buka browser manual: %s", url)
			}
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	samplerCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	os.Remove(cfg.Path("runtime.json"))
}
