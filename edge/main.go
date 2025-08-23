package main

import (
	"context"
	"encoding/json"
	// "io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type cacheEntry struct {
	data      []byte
	mime      string
	expiresAt time.Time
}

type edgeServer struct {
	nodeID       string
	baseLatency  time.Duration
	cacheTTL     time.Duration
	cache        map[string]cacheEntry
	cacheMu      sync.RWMutex
	assetsDir    string
	redis        *redis.Client
	streamName   string
	lastStreamID string
}

func main() {
	nodeID := env("NODE_ID", "edge")
	baseLatencyMs := mustAtoi(env("BASE_LATENCY_MS", "25"))
	cacheTTLSeconds := mustAtoi(env("CACHE_TTL_SECONDS", "60"))
	assetsDir := env("ASSETS_DIR", "./assets")
	redisAddr := env("REDIS_ADDR", "localhost:6379")

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	es := &edgeServer{
		nodeID:      nodeID,
		baseLatency: time.Duration(baseLatencyMs) * time.Millisecond,
		cacheTTL:    time.Duration(cacheTTLSeconds) * time.Second,
		cache:       make(map[string]cacheEntry),
		assetsDir:   assetsDir,
		redis:       rdb,
		streamName:  "invalidate",
	}

	// Start Redis Stream consumer for invalidations
	go es.consumeInvalidations()

	r := chi.NewRouter()
	r.Get("/ping", es.handlePing)
	r.Get("/assets/*", es.handleGetAsset)
	r.Post("/invalidate", es.handleInvalidate) // publishes to stream
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":8080"
	log.Printf("[%s] Starting edge on %s (latency=%s, cacheTTL=%s)\n", nodeID, addr, es.baseLatency, es.cacheTTL)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

func (es *edgeServer) handlePing(w http.ResponseWriter, r *http.Request) {
	// simulate the latency: base + tiny jitter [0 to .5ms]
	time.Sleep(es.baseLatency + time.Duration(rand.Intn(6))*time.Millisecond)
	writeJSON(w, map[string]any{"nodeId": es.nodeID, "latencyMs": es.baseLatency.Milliseconds()})
}

func (es *edgeServer) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/assets")
	if path == "" || path == "/" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	// simulated latency before response
	time.Sleep(es.baseLatency + time.Duration(rand.Intn(6))*time.Millisecond)

	// check the cache and lock for reading
	es.cacheMu.RLock()
	entry, ok := es.cache[path]
	es.cacheMu.RUnlock()

	now := time.Now()
	if ok && now.Before(entry.expiresAt) { // cache hit and not expired
		w.Header().Set("Content-Type", entry.mime)
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("X-Node", es.nodeID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(entry.data)
		return
	}

	// load from disk
	full := filepath.Join(es.assetsDir, filepath.Clean(path))
	data, mime, err := readFileWithMime(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// store to cache
	es.cacheMu.Lock()
	es.cache[path] = cacheEntry{data: data, mime: mime, expiresAt: now.Add(es.cacheTTL)}
	es.cacheMu.Unlock()

	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Cache", "MISS")
	w.Header().Set("X-Node", es.nodeID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (es *edgeServer) handleInvalidate(w http.ResponseWriter, r *http.Request) {
	var body struct{ Path string `json:"path"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, `{"error":"body {path} required"}`, http.StatusBadRequest)
		return
	}
	ctx := context.Background()
	args := &redis.XAddArgs{
		Stream: es.streamName,
		Values: map[string]any{"path": body.Path, "ts": time.Now().UnixMilli()},
	}
	id := es.redis.XAdd(ctx, args).Val()
	writeJSON(w, map[string]any{"published": true, "id": id, "path": body.Path})
}

func (es *edgeServer) consumeInvalidations() {
	ctx := context.Background()
	es.lastStreamID = "$" // start from new messages
	for {
		streams, err := es.redis.XRead(ctx, &redis.XReadArgs{
			Streams: []string{es.streamName, es.lastStreamID},
			Block:   0, // block indefinitely
			Count:   10,
		}).Result()
		if err != nil && err != redis.Nil {
			log.Printf("[%s] XREAD error: %v\n", es.nodeID, err)
			time.Sleep(time.Second)
			continue
		}
		for _, s := range streams {
			for _, m := range s.Messages {
				es.lastStreamID = m.ID
				path, _ := m.Values["path"].(string)
				if path != "" {
					es.cacheMu.Lock()
					delete(es.cache, path)
					es.cacheMu.Unlock()
					log.Printf("[%s] Invalidated cache for path: %s\n", es.nodeID, path)
				}
			}
		}
	}
}

func readFileWithMime(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime := "application/octet-stream"
	switch ext {
	case ".txt":
		mime = "text/plain; charset=utf-8"
	case ".css":
		mime = "text/css; charset=utf-8"
	case ".js":
		mime = "text/javascript; charset=utf-8"
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".svg":
		mime = "image/svg+xml"
	case ".html", ".htm":
		mime = "text/html; charset=utf-8"
	}
	return data, mime, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustAtoi(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("invalid int: %s", s)
	}
	return i
}

// // ensure assets dir exists (optional)
// func ensureDir(d string) error {
// 	return fs.ValidPath(d)
// }
