package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/statewright/trove/internal/metadata"
	"github.com/statewright/trove/internal/server"
	"github.com/statewright/trove/internal/storage"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := loadConfig()

	db, err := sql.Open("pgx", cfg.PostgresURL)
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping postgres: %v", err)
	}

	store := metadata.New(db)
	if err := store.Migrate(); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	rootKey, rootSecret, err := ensureRootKey(store, cfg)
	if err != nil {
		log.Fatalf("failed to ensure root access key: %v", err)
	}
	if rootKey != "" {
		fmt.Fprintf(os.Stderr, "root access key: %s\n", rootKey)
		fmt.Fprintf(os.Stderr, "root secret key: %s\n", rootSecret)
	}

	blobs, err := storage.NewBlobStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("failed to initialize blob storage: %v", err)
	}

	srv := server.New(store, blobs)
	log.Printf("trove listening on %s", cfg.Listen)
	if err := srv.ListenAndServe(cfg.Listen); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

type config struct {
	Listen        string
	PostgresURL   string
	DataDir       string
	RootAccessKey string
	RootSecretKey string
}

func loadConfig() config {
	cfg := config{
		Listen:        envOr("TROVE_LISTEN", ":9000"),
		PostgresURL:   os.Getenv("TROVE_POSTGRES_URL"),
		DataDir:       envOr("TROVE_DATA_DIR", "./data"),
		RootAccessKey: os.Getenv("TROVE_ROOT_ACCESS_KEY"),
		RootSecretKey: os.Getenv("TROVE_ROOT_SECRET_KEY"),
	}

	if cfg.PostgresURL == "" {
		log.Fatal("TROVE_POSTGRES_URL is required")
	}

	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func ensureRootKey(store *metadata.Store, cfg config) (accessKey, secretKey string, err error) {
	existing, err := store.ListRootKeys()
	if err != nil {
		return "", "", err
	}
	if len(existing) > 0 {
		return "", "", nil // root key already exists, nothing to print
	}

	accessKey = cfg.RootAccessKey
	secretKey = cfg.RootSecretKey

	if accessKey == "" {
		accessKey = generateKey(20)
	}
	if secretKey == "" {
		secretKey = generateKey(40)
	}

	if err := store.CreateAccessKey(accessKey, secretKey, "root", true); err != nil {
		return "", "", fmt.Errorf("create root key: %w", err)
	}

	return accessKey, secretKey, nil
}

func generateKey(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("failed to generate random key: %v", err)
	}
	return hex.EncodeToString(b)[:length]
}
