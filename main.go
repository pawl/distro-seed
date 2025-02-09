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

const (
	statusInterval   = 30 * time.Second // Frequency of status logging
	announceInterval = 10 * time.Minute // Re-announce to trackers/DHT
)

func main() {
	// Disable the default timestamp in log package to avoid duplicate dates
	log.SetFlags(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupSignalHandling(cancel)

	downloadDir := flag.String("dir", getEnv("DOWNLOAD_DIR", "./downloads"), "Directory to store downloaded files")
	torrentURLs := flag.String("url", getEnv("TORRENT_URLS", ""), "Comma-separated list of torrent URLs or magnet links")
	flag.Parse()

	// Set the path for seedStatsFile dynamically based on downloadDir
	seedStatsFile := filepath.Join(*downloadDir, "seed_stats.txt")

	if *torrentURLs == "" {
		log.Fatal("❌ No torrent URLs or magnet links provided. Set -url flag or TORRENT_URLS environment variable.")
	}

	torrentList := parseTorrentURLs(*torrentURLs)
	ensureDirectoryExists(*downloadDir)

	client := configureTorrentClient(*downloadDir)
	defer client.Close()

	// Initialize the grand total uploaded amount from the stats file
	totalUploaded := readTotalUploaded(seedStatsFile)

	// Periodic tasks
	go logPeriodicTorrentStatus(ctx, client, seedStatsFile, &totalUploaded)
	go periodicAnnounce(ctx, client)

	processTorrents(ctx, client, torrentList, *downloadDir)

	<-ctx.Done()
	log.Println("🛑 Shutting down torrent client...")
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
		log.Fatalf("❌ Failed to create directory '%s': %v", path, err)
	}
}

func configureTorrentClient(downloadDir string) *torrent.Client {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = downloadDir
	cfg.Seed = true
	cfg.NoUpload = false // Allow uploading

	// **Increase Connection Limits**
	cfg.EstablishedConnsPerTorrent = 100 // Allow more concurrent connections
	cfg.HalfOpenConnsPerTorrent = 50     // Allow more incoming connections

	// **Enable Peer Discovery**
	cfg.NoDHT = false      // Enable DHT for decentralized peer discovery
	cfg.DisablePEX = false // Enable Peer Exchange (PEX)

	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to create torrent client: %v", err)
	}
	return client
}

func processTorrents(ctx context.Context, client *torrent.Client, urls []string, downloadDir string) {
	for _, url := range urls {
		if strings.HasPrefix(url, "magnet:?") {
			// Handle magnet URLs
			log.Printf("📥 Adding magnet URL: %s", url)
			t, err := client.AddMagnet(url)
			if err != nil {
				log.Printf("⚠️ Error adding magnet URL '%s': %v", url, err)
				continue
			}
			go waitForMagnetMetadata(ctx, t)
		} else {
			// Handle regular torrent file URLs
			if t, err := addTorrent(client, url, downloadDir); err != nil {
				log.Printf("⚠️ Error adding torrent from URL '%s': %v", url, err)
			} else {
				go seedTorrent(ctx, t)
			}
		}
	}
}

func waitForMagnetMetadata(ctx context.Context, t *torrent.Torrent) {
	log.Printf("⏳ Waiting for metadata: %s", t.InfoHash().HexString())
	<-t.GotInfo() // Wait for metadata
	log.Printf("✅ Metadata retrieved: %s", t.Name())
	go seedTorrent(ctx, t)
}

func addTorrent(client *torrent.Client, url, downloadDir string) (*torrent.Torrent, error) {
	// Handle regular torrent file URLs
	torrentPath := filepath.Join(downloadDir, filepath.Base(url))

	// Download torrent file if it doesn't exist
	if _, err := os.Stat(torrentPath); os.IsNotExist(err) {
		log.Printf("📥 Downloading torrent file: %s", url)
		resp, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("❌ Failed to download torrent: %w", err)
		}
		defer resp.Body.Close()

		out, err := os.Create(torrentPath)
		if err != nil {
			return nil, fmt.Errorf("❌ Failed to create torrent file: %w", err)
		}
		defer out.Close()

		if _, err = out.ReadFrom(resp.Body); err != nil {
			return nil, fmt.Errorf("❌ Failed to save torrent file: %w", err)
		}
		log.Printf("✅ Torrent file saved: %s", torrentPath)
	}

	meta, err := metainfo.LoadFromFile(torrentPath)
	if err != nil {
		return nil, fmt.Errorf("❌ Failed to load torrent metadata: %w", err)
	}

	t, err := client.AddTorrent(meta)
	if err != nil {
		return nil, fmt.Errorf("❌ Failed to add torrent: %w", err)
	}

	return t, nil
}

func seedTorrent(ctx context.Context, t *torrent.Torrent) {
	<-t.GotInfo()   // Wait for metadata before proceeding
	t.DownloadAll() // Ensure we have the entire file before seeding
	log.Printf("🌱 Seeding: %s (Size: %d MB)", t.Name(), t.Length()/1024/1024)

	// Keep running until termination signal
	<-ctx.Done()
}

func readTotalUploaded(seedStatsFile string) int64 {
	file, err := os.Open(seedStatsFile)
	if err != nil {
		log.Printf("Warning: Could not open seed stats file for reading: %v", err)
		return 0
	}
	defer file.Close()

	var totalUploaded int64
	_, err = fmt.Fscanf(file, "%d", &totalUploaded)
	if err != nil {
		log.Printf("Warning: Failed to read total uploaded from file: %v", err)
		return 0
	}

	return totalUploaded
}

func logPeriodicTorrentStatus(ctx context.Context, client *torrent.Client, seedStatsFile string, totalUploaded *int64) {
	ticker := time.NewTicker(statusInterval)
	defer ticker.Stop()

	// Track the previously recorded total uploaded for each torrent
	previousUploads := make(map[string]int64)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logCurrentTorrentStatus(client, seedStatsFile, totalUploaded, previousUploads)
		}
	}
}

func logCurrentTorrentStatus(client *torrent.Client, seedStatsFile string, totalUploaded *int64, previousUploads map[string]int64) {
	var sessionUpload int64

	for _, t := range client.Torrents() {
		stats := t.Stats()
		uploaded := stats.ConnStats.BytesWrittenData.Int64()

		// Get the previously recorded upload for this torrent
		prevUploaded := previousUploads[t.InfoHash().HexString()]

		// Calculate the total uploaded for this session
		increment := uploaded - prevUploaded

		// Update the map with the latest upload value for this torrent
		previousUploads[t.InfoHash().HexString()] = uploaded

		// Add the increment to the session's total upload
		sessionUpload += increment

		// Log per-torrent stats (total uploaded since program started)
		log.Printf("➡️ %s - %d peers - Total Uploaded: %.2f MB",
			t.Name(), len(t.PeerConns()), float64(uploaded)/1024/1024)
	}

	// Update the grand total uploaded with the session's upload
	*totalUploaded += sessionUpload

	log.Printf("📊 Total uploaded: %.2f MB (all runs)", float64(*totalUploaded)/1024/1024)

	// Write the updated total uploaded to the stats file
	file, err := os.Create(seedStatsFile)
	if err != nil {
		log.Printf("Error: Could not open seed stats file for writing: %v", err)
		return
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "%d", *totalUploaded)
	if err != nil {
		log.Printf("Error: Failed to write total uploaded to file: %v", err)
	}
}

// Periodically re-announce to DHT and trackers
func periodicAnnounce(ctx context.Context, client *torrent.Client) {
	ticker := time.NewTicker(15 * time.Minute) // Announce every 15 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Println("🔄 Re-announcing torrents to trackers and DHT...")

			for _, t := range client.Torrents() {
				if t.Stats().TotalPeers < 10 { // Only re-announce if we have few peers
					log.Printf("🔄 Re-announcing: %s", t.Name())

					// Re-announce to all trackers
					for _, tracker := range t.Metainfo().AnnounceList {
						t.ModifyTrackers([][]string{tracker})
					}

					// Re-announce to DHT
					var infoHash [20]byte
					copy(infoHash[:], t.InfoHash().Bytes())
					for _, dhtServer := range client.DhtServers() {
						dhtServer.Announce(infoHash, client.LocalPort(), true)
					}
				}
			}
		}
	}
}

// Handle SIGINT and SIGTERM for graceful shutdown
func setupSignalHandling(cancelFunc context.CancelFunc) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		log.Println("🛑 Received shutdown signal...")
		cancelFunc()
	}()
}
