package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

const seedStatsFile = "seed_stats.txt"

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupSignalHandling(cancel)

	downloadDir := flag.String("dir", getEnv("DOWNLOAD_DIR", "./downloads"), "Directory to store downloaded files")
	torrentURLs := flag.String("url", getEnv("TORRENT_URLS", ""), "Comma-separated list of torrent URLs")
	flag.Parse()

	if *torrentURLs == "" {
		log.Fatal("No torrent URLs provided. Set -url flag or TORRENT_URLS environment variable.")
	}

	torrentList := parseTorrentURLs(*torrentURLs)
	ensureDirectoryExists(*downloadDir)

	client := configureTorrentClient(*downloadDir)
	defer client.Close()

	processTorrents(ctx, client, torrentList, *downloadDir)

	go logPeriodicTorrentStatus(ctx, client, *downloadDir)

	<-ctx.Done()
	log.Println("Shutting down torrent client...")
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func parseTorrentURLs(input string) []string {
	urls := strings.Split(input, ",")
	for i, url := range urls {
		urls[i] = strings.TrimSpace(url)
	}
	return urls
}

func ensureDirectoryExists(path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		log.Fatalf("Failed to create directory '%s': %v", path, err)
	}
}

func configureTorrentClient(downloadDir string) *torrent.Client {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = downloadDir
	cfg.Seed = true
	cfg.NoUpload = false // Ensure maximum upload availability

	// **Optimized for maximum uploading**
	cfg.EstablishedConnsPerTorrent = 50 // Allow many concurrent connections
	cfg.HalfOpenConnsPerTorrent = 20    // Allow more incoming connections
	cfg.MaxUnverifiedBytes = 4 * 1024 * 1024 // 4MB (default) to optimize disk caching

	// **Enable Peer Discovery**
	cfg.NoDHT = false       // Enable DHT for finding more peers
	cfg.DisablePEX = false  // Enable Peer Exchange (PEX)

	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create torrent client: %v", err)
	}
	return client
}

func processTorrents(ctx context.Context, client *torrent.Client, urls []string, downloadDir string) {
	for _, url := range urls {
		if t, err := addTorrent(client, url, downloadDir); err != nil {
			log.Printf("Error adding torrent from URL '%s': %v", url, err)
		} else {
			go seedTorrent(ctx, t)
		}
	}
}

func addTorrent(client *torrent.Client, url, downloadDir string) (*torrent.Torrent, error) {
	torrentPath := filepath.Join(downloadDir, filepath.Base(url))

	// Download torrent file if it doesn't exist
	if _, err := os.Stat(torrentPath); os.IsNotExist(err) {
		log.Printf("📥 Downloading torrent file: %s", url)
		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to download torrent: %w", err)
		}
		defer resp.Body.Close()

		out, err := os.Create(torrentPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create torrent file: %w", err)
		}
		defer out.Close()

		if _, err = out.ReadFrom(resp.Body); err != nil {
			return nil, fmt.Errorf("failed to save torrent file: %w", err)
		}
		log.Printf("✅ Torrent file saved: %s", torrentPath)
	}

	meta, err := metainfo.LoadFromFile(torrentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load torrent metadata: %w", err)
	}

	t, err := client.AddTorrent(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to add torrent: %w", err)
	}

	return t, nil
}

func seedTorrent(ctx context.Context, t *torrent.Torrent) {
	<-t.GotInfo() // Wait for metadata before proceeding
	t.DownloadAll() // Download all pieces first, then start seeding
	log.Printf("🌱 Now seeding: %s (Size: %d MB)", t.Name(), t.Length()/1024/1024)

	<-ctx.Done() // Keep seeding until termination signal is received
}

func logPeriodicTorrentStatus(ctx context.Context, client *torrent.Client, downloadDir string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logCurrentTorrentStatus(client, downloadDir)
		}
	}
}

func logCurrentTorrentStatus(client *torrent.Client, downloadDir string) {
	var totalUploaded int64
	for _, t := range client.Torrents() {
		stats := t.Stats()
		totalUploaded += stats.ConnStats.BytesWrittenData.Int64()
	}
	log.Printf("📊 Total uploaded: %.2f MB", float64(totalUploaded)/1024/1024)
}

func setupSignalHandling(cancelFunc context.CancelFunc) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		cancelFunc()
	}()
}
