package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"cas.local/cas/internal/cas"
)

func main() {
	dbURL := os.Getenv("CAS_DATABASE_URL")
	if dbURL == "" { log.Fatal("CAS_DATABASE_URL wajib diisi") }
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil { log.Fatal("gagal membuat koneksi database") }
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil { log.Fatal("database tidak dapat dihubungi") }
	if err := cas.Migrate(ctx, pool); err != nil { log.Fatal("migrasi database gagal") }
	if id, key := os.Getenv("CAS_BOOTSTRAP_APP_ID"), os.Getenv("CAS_BOOTSTRAP_APP_KEY"); id != "" && key != "" {
		if err := cas.BootstrapApp(ctx, pool, id, key); err != nil { log.Fatal("bootstrap aplikasi gagal") }
	}

	storage := os.Getenv("CAS_STORAGE_DIR")
	if storage == "" { storage = "./data" }
	if err := os.MkdirAll(storage, 0700); err != nil { log.Fatal("storage tidak dapat disiapkan") }
	maxUpload := int64(50 << 20)
	if v := os.Getenv("CAS_MAX_UPLOAD_BYTES"); v != "" { if n, e := parsePositiveInt(v); e == nil { maxUpload = n } }
	base := strings.TrimRight(os.Getenv("USERGATE_BASE_URL"), "/")
	if base == "" { base = "https://usergate.sandalgurun.web.id/api/v1" }
	service := cas.New(pool, storage, maxUpload, cas.UserGate{BaseURL: base, APIKey: os.Getenv("USERGATE_API_KEY")})
	srv := &http.Server{Addr: envOr("CAS_ADDR", ":8080"), Handler: service.Handler(), ReadHeaderTimeout: 5*time.Second, ReadTimeout: 2*time.Minute, WriteTimeout: 2*time.Minute, IdleTimeout: 60*time.Second}
	go func() { <-ctx.Done(); shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel(); _ = srv.Shutdown(shutdown) }()
	log.Printf("CAS berjalan di %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed { log.Fatal("server CAS berhenti") }
}

func envOr(k, fallback string) string { if v := os.Getenv(k); v != "" { return v }; return fallback }
func parsePositiveInt(s string) (int64, error) { n, err := strconv.ParseInt(s, 10, 64); if err != nil || n <= 0 { return 0, fmt.Errorf("nilai tidak valid") }; return n, nil }
