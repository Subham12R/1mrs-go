package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

// CACHE_MODE=db: Postgres only (Stage 1)
func (s *server) getItemDB(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var it Item
	err := s.db.QueryRow(r.Context(),
		"SELECT id, name FROM items WHERE id=$1", id).Scan(&it.ID, &it.Name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("getItemDB id=%s: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(it)
}

// CACHE_MODE=redis: Redis cache-aside, no L1 (Stage 3)
func (s *server) getItemRedis(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	key := "item:" + id
	ctx := r.Context()

	b, err := s.rdb.Get(ctx, key).Bytes()
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "L2")
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
		log.Printf("getItemRedis id=%s: %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	b, _ = json.Marshal(it)
	if err := s.rdb.Set(ctx, key, b, cacheTTL).Err(); err != nil {
		log.Printf("redis set %s: %v", key, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "DB")
	w.Write(b)
}
