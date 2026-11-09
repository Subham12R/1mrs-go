package main

import (
	"context"
	"encoding/json"
	"errors"

	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
    _ "net/http/pprof"
)


type Item struct {
	ID int `json:"id"`
	Name string `json:"name"`
}

type server struct{db *pgxpool.Pool}

// Setting up the DB query server

func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var it Item
	err := s.db.QueryRow(r.Context(),
		"SELECT id, name FROM items WHERE id=$1", id).Scan(&it.ID, &it.Name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("getItem id=%s: %v", id, err) // real error shows in the server terminal
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(it)
}
// Setting up the main server pool and connection config

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using system env")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	cfg, _ := pgxpool.ParseConfig(dbURL)
	cfg.MaxConns = 20
	db, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}

	s := &server{db : db}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", s.getItem)

	srv := &http.Server{
		Addr: 	":8080",
		Handler: mux,
		ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout: 60 * time.Second,
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