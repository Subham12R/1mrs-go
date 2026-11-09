package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"log"
	"net/http"
	"os"
	"time"

	_ "net/http/pprof"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type server struct {
	db  *pgxpool.Pool
	rdb *redis.Client
	l1  *l1Cache
	sf  singleflight.Group
}

type l1Entry struct {
	val []byte
	exp int64
}

type l1Cache struct {
	mu sync.RWMutex
	m  map[string]l1Entry
}

const l1TTL = 5 * time.Second
const cacheTTL = 60 * time.Second

func newL1() *l1Cache { return &l1Cache{m: make(map[string]l1Entry)} }

func (c *l1Cache) set(k string, v []byte, ttl time.Duration) {
	c.mu.Lock()
	c.m[k] = l1Entry{val: v, exp: time.Now().Add(ttl).UnixNano()}
	c.mu.Unlock()
}

func (c *l1Cache) get(k string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.m[k]
	c.mu.RUnlock()
	if !ok || time.Now().UnixNano() > e.exp {
		return nil, false
	}
	return e.val, true
}

type result struct {
	b   []byte
	src string // "L2" or "DB"
}

// Setting up the DB query server

// func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
// 	id := r.PathValue("id")
// 	key := "item: " + id
// 	ctx := r.Context()

// 	b, err := s.rdb.Get(ctx, key).Bytes()

// 	if err == nil {
// 		w.Header().Set("Content-Type", "application/json")
// 		w.Header().Set("X-Cache", "HIT")
// 		w.Write(b)
// 		return
// 	}

// 	if !errors.Is(err, redis.Nil) {
// 		log.Printf("redis get %s: %v", key, err)
// 	}

// 	var it Item

// 	err = s.db.QueryRow(ctx,
// 		"SELECT id, name FROM items WHERE id=$1", id).Scan(&it.ID, &it.Name)
// 	if err != nil {
// 		if errors.Is(err, pgx.ErrNoRows) {
// 			http.Error(w, "not found", http.StatusNotFound)
// 			return
// 		}
// 		log.Printf("getItem id=%s: %v", id, err)
// 		http.Error(w, "internal error", http.StatusInternalServerError)
// 		return
// 	}

// 	b, _ = json.Marshal(it)

// 	if err := s.rdb.Set(ctx, key, b, cacheTTL).Err(); err != nil {
// 		log.Printf("redis set %s: %v", key, err)
// 	}

// 	w.Header().Set("Content-Type", "application/json")
// 	w.Header().Set("X-Cache", "MISS")
// 	w.Write(b)

// }

func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	key := "item:" + id

	w.Header().Set("Content-Type", "application/json")

	// L1: memory, no network
	if b, ok := s.l1.get(key); ok {
		w.Header().Set("X-Cache", "L1")
		w.Write(b)
		return
	}

	// L2 + DB, collapsed: 1000 simultaneous misses -> 1 Redis/DB call
	v, err, _ := s.sf.Do(key, func() (any, error) {
		// shared work must not die if the first caller disconnects
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()

		b, err := s.rdb.Get(ctx, key).Bytes()
		if err == nil {
			return result{b, "L2"}, nil
		}
		if !errors.Is(err, redis.Nil) {
			log.Printf("redis get %s: %v", key, err)
		}

		var it Item
		if err := s.db.QueryRow(ctx,
			"SELECT id, name FROM items WHERE id=$1", id).Scan(&it.ID, &it.Name); err != nil {
			return nil, err
		}
		b, _ = json.Marshal(it)
		if err := s.rdb.Set(ctx, key, b, cacheTTL).Err(); err != nil {
			log.Printf("redis set %s: %v", key, err)
		}
		return result{b, "DB"}, nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("getItem id=%s: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	res := v.(result)
	s.l1.set(key, res.b, l1TTL)
	w.Header().Set("X-Cache", res.src)
	w.Write(res.b)
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

	s := &server{db: db, rdb: rdb, l1: newL1()}

	mux := http.NewServeMux()
	// CACHE_MODE picks the stage to benchmark: db | redis | l1 (default)
	mode := os.Getenv("CACHE_MODE")
	var handler http.HandlerFunc
	switch mode {
	case "db":
		handler = s.getItemDB // Stage 1: Postgres only
	case "redis":
		handler = s.getItemRedis // Stage 3: Redis cache-aside
	default:
		mode = "l1"
		handler = s.getItem // Stage 4: L1 + Redis + singleflight
	}
	log.Println("cache mode:", mode)
	mux.HandleFunc("GET /items/{id}", handler)
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
