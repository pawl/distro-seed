package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// Seed stats file
const seedStatsFile = "seed_stats.txt"

func main() {
	// Parse CLI arguments (for local testing)
	downloadDir := flag.String("dir", getEnv("DOWNLOAD_DIR", "./downloads"), "Directory to store downloaded files")
	torrentURLs := flag.String("url", getEnv("TORRENT_URLS", ""), "Comma-separated list of torrent URLs")
	flag.Parse()

	// Validate arguments
	if *torrentURLs == "" {
		log.Fatal("❌ No torrent URLs provided! Set -url flag or TORRENT_URLS env variable.")
	}
	torrentList := parseTorrentURLs(*torrentURLs)

	// Ensure the download directory exists
	if err := os.MkdirAll(*downloadDir, 0755); err != nil {
		log.Fatalf("❌ Failed to create downloads directory: %v", err)
	}

	// Configure torrent client
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = *downloadDir
	cfg.Seed = true

	// Create client
	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to create torrent client: %v", err)
	}
	defer client.Close()

	log.Println("🚀 Torrent client started. Downloading and seeding...")

	// Process each torrent
	for _, url := range torrentList {
		_, err := downloadAndSeed(client, url, *downloadDir)
		if err != nil {
			log.Printf("⚠️  Error processing torrent %s: %v", url, err)
		}
	}

	// Periodic status logging
	go logTorrentStatus(client, *downloadDir)

	// Keep running to seed torrents indefinitely
	select {}
}

// getEnv returns an environment variable or a default value
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// parseTorrentURLs splits and trims the URL list from CLI input or env variable
func parseTorrentURLs(input string) []string {
	var urls []string
	for _, url := range strings.Split(input, ",") {
		trimmedURL := strings.TrimSpace(url)
		if trimmedURL != "" {
			urls = append(urls, trimmedURL)
		}
	}
	return urls
}

// downloadAndSeed downloads the torrent file, adds it to the client, and starts seeding.
func downloadAndSeed(client *torrent.Client, url string, downloadDir string) (*torrent.Torrent, error) {
	torrentFile := filepath.Join(downloadDir, filepath.Base(url))

	// Check if the torrent file already exists
	if _, err := os.Stat(torrentFile); os.IsNotExist(err) {
		log.Printf("📥 Downloading torrent file: %s", url)

		// Download the torrent file
		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("❌ Failed to download torrent: %w", err)
		}
		defer resp.Body.Close()

		// Save to disk
		out, err := os.Create(torrentFile)
		if err != nil {
			return nil, fmt.Errorf("❌ Failed to create torrent file: %w", err)
		}
		defer out.Close()

		if _, err = out.ReadFrom(resp.Body); err != nil {
			return nil, fmt.Errorf("❌ Failed to save torrent file: %w", err)
		}
		log.Printf("✅ Torrent file saved: %s", torrentFile)
	}

	// Add torrent to client
	meta, err := metainfo.LoadFromFile(torrentFile)
	if err != nil {
		return nil, fmt.Errorf("❌ Failed to load torrent metadata: %w", err)
	}

	t, err := client.AddTorrent(meta)
	if err != nil {
		return nil, fmt.Errorf("❌ Failed to add torrent: %w", err)
	}

	// Wait for torrent info
	select {
	case <-t.GotInfo():
		t.DownloadAll()
		log.Printf("🌱 Now seeding: %s (Size: %d MB)", t.Name(), t.Length()/1024/1024)
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("⚠️ Timeout waiting for torrent metadata: %s", t.Name())
	}

	return t, nil
}

// logTorrentStatus periodically logs the status and updates seed stats
func logTorrentStatus(client *torrent.Client, downloadDir string) {
	seedStatsPath := filepath.Join(downloadDir, seedStatsFile)

	for {
		time.Sleep(30 * time.Second)
		log.Println("📊 Torrent Status:")

		var totalUploaded int64

		for _, t := range client.Torrents() {
			progress := float64(t.BytesCompleted()) / float64(t.Length()) * 100

			stats := t.Stats()
			uploaded := stats.ConnStats.BytesWrittenData.Int64()

			totalUploaded += uploaded

			log.Printf("➡️  %s - %.2f%% complete - %d peers - Uploaded: %.2f MB",
				t.Name(), progress, len(t.PeerConns()), float64(uploaded)/1024/1024)
		}

		// Save upload stats
		err := os.WriteFile(seedStatsPath, []byte(fmt.Sprintf("Total Uploaded: %.2f MB\n", float64(totalUploaded)/1024/1024)), 0644)
		if err != nil {
			log.Printf("⚠️  Failed to write seed stats: %v", err)
		}
	}
}
