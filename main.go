package main

import (
	"context"
	"encoding/json"
	"errors"

	"log"
	"net/http"
	"os"
	"time"

	_ "net/http/pprof"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type server struct {
	db  *pgxpool.Pool
	rdb *redis.Client
}

const cacheTTL = 60 * time.Second

// Setting up the DB query server

func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	key := "item: " + id
	ctx := r.Context()

	b, err := s.rdb.Get(ctx, key).Bytes()

	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.Write(b)
		return
	}

	if !errors.Is(err, redis.Nil) {
		log.Printf("redis get %s: %v", key, err)
	}

	var it Item

	err = s.db.QueryRow(ctx,
		"SELECT id, name FROM items WHERE id=$1", id).Scan(&it.ID, &it.Name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("getItem id=%s: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	b, _ = json.Marshal(it)

	if err := s.rdb.Set(ctx, key, b, cacheTTL).Err(); err != nil {
		log.Printf("redis set %s: %v", key, err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	w.Write(b)

}

// Setting up the main server pool and connection config

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using system env")
	}

	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatalf("bad DATABASE_URL: %v", err)
	}
	cfg.MaxConns = 20
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("pool: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("db unreachable: %v", err)
	}
	log.Println("db connected")

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0"
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("bad REDIS_URL: %v", err)
	}
	opt.PoolSize = 50
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis unreachable: %v", err)
	}
	log.Println("redis connected")

	s := &server{db: db, rdb: rdb}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", s.getItem)
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	log.Println("Listening on :8080")
	log.Fatal(srv.ListenAndServe())
}

// Base test
// npm i -g autocannon
// autocannon -c 200 -a 1000000 http://localhost:8080/items/1
// # -c connections, -a total requests, -d seconds, -w worker threads
