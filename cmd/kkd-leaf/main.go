package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/kn8-codes/knock-knock-deflock/internal/capture"
	"github.com/kn8-codes/knock-knock-deflock/internal/emit"
	"github.com/kn8-codes/knock-knock-deflock/internal/event"
	"github.com/kn8-codes/knock-knock-deflock/internal/iface"
	"github.com/kn8-codes/knock-knock-deflock/internal/oui"
)

func main() {
	cfg, err := configFromEnv()
	if err != nil {
		log.Fatalf("kkd-leaf: config: %v", err)
	}

	ouiDB, err := oui.Load()
	if err != nil {
		log.Fatalf("kkd-leaf: oui: %v", err)
	}

	w, err := emit.New(cfg.outputFile, cfg.rotateSz, cfg.rotateAge, cfg.rotateKeep)
	if err != nil {
		log.Fatalf("kkd-leaf: emit: %v", err)
	}

	sessionID := uuid.New().String()
	log.Printf("kkd-leaf: session=%s node=%s output=%s", sessionID, cfg.nodeID, cfg.outputFile)

	if len(cfg.wifiIfaces) == 0 && len(cfg.bleIfaces) == 0 {
		log.Fatal("kkd-leaf: no interfaces configured — set KKD_WIFI_IFACES and/or KKD_BLE_IFACES")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventCh := make(chan event.CaptureEvent, 512)

	// --- WiFi ---
	var activeWiFi []string
	wifiStats := make(map[string]*capture.WiFiStats)
	for _, ifname := range cfg.wifiIfaces {
		if err := iface.SetMonitor(ifname); err != nil {
			log.Printf("kkd-leaf: wifi %s: %v — skipping", ifname, err)
			continue
		}
		activeWiFi = append(activeWiFi, ifname)
		st := &capture.WiFiStats{}
		wifiStats[ifname] = st
		go func(name string) {
			if err := capture.RunWiFi(ctx, name, eventCh, ouiDB, sessionID, cfg.nodeID, st); err != nil {
				log.Printf("kkd-leaf: wifi %s: %v", name, err)
			}
		}(ifname)
		go capture.Hop(ctx, ifname)
		log.Printf("kkd-leaf: wifi %s: active", ifname)
	}

	// --- BLE ---
	var activeBLE []string
	bleStats := make(map[string]*capture.BLEStats)
	for _, ifname := range cfg.bleIfaces {
		st := &capture.BLEStats{}
		bleStats[ifname] = st
		go func(name string) {
			if err := capture.RunBLE(ctx, name, eventCh, ouiDB, sessionID, cfg.nodeID, st); err != nil {
				log.Printf("kkd-leaf: ble %s: %v", name, err)
			}
		}(ifname)
		activeBLE = append(activeBLE, ifname)
		log.Printf("kkd-leaf: ble %s: active", ifname)
	}

	if len(activeWiFi) == 0 && len(activeBLE) == 0 {
		log.Fatal("kkd-leaf: all interfaces failed to initialize")
	}

	// Fan-in: eventCh → emit writer
	var fanInWG sync.WaitGroup
	fanInWG.Add(1)
	go func() {
		defer fanInWG.Done()
		for {
			select {
			case ev := <-eventCh:
				if err := w.Write(ev); err != nil {
					log.Printf("kkd-leaf: emit: %v", err)
				}
			case <-ctx.Done():
				// drain buffered events before exit
				for {
					select {
					case ev := <-eventCh:
						_ = w.Write(ev)
					default:
						return
					}
				}
			}
		}
	}()

	// Stats logger
	go statsLoop(ctx, cfg.statsInterval, activeWiFi, wifiStats, activeBLE, bleStats)

	// Block until signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	log.Printf("kkd-leaf: shutting down")
	cancel()
	fanInWG.Wait()

	for _, ifname := range activeWiFi {
		if err := iface.SetManaged(ifname); err != nil {
			log.Printf("kkd-leaf: %s: restore managed: %v", ifname, err)
		}
	}

	// Final stats
	for _, ifname := range activeWiFi {
		capture.LogWiFiStats(ifname, wifiStats[ifname])
	}
	for _, ifname := range activeBLE {
		capture.LogBLEStats(ifname, bleStats[ifname])
	}

	if err := w.Close(); err != nil {
		log.Printf("kkd-leaf: close writer: %v", err)
	}
	log.Printf("kkd-leaf: clean exit")
}

func statsLoop(
	ctx context.Context,
	interval time.Duration,
	wifiIfaces []string,
	wifiStats map[string]*capture.WiFiStats,
	bleIfaces []string,
	bleStats map[string]*capture.BLEStats,
) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, ifname := range wifiIfaces {
				capture.LogWiFiStats(ifname, wifiStats[ifname])
			}
			for _, ifname := range bleIfaces {
				capture.LogBLEStats(ifname, bleStats[ifname])
			}
		}
	}
}

// --- config ---

type config struct {
	wifiIfaces    []string
	bleIfaces     []string
	nodeID        string
	outputFile    string
	statsInterval time.Duration
	rotateSz      int64
	rotateAge     time.Duration
	rotateKeep    int
}

func configFromEnv() (*config, error) {
	cfg := &config{
		outputFile:    envOr("KKD_OUTPUT_FILE", "./kkd-capture.ndjson"),
		statsInterval: mustDuration(envOr("KKD_STATS_INTERVAL", "60s")),
		rotateSz:      mustInt64(envOr("KKD_ROTATE_SIZE", "104857600")),
		rotateAge:     mustDuration(envOr("KKD_ROTATE_AGE", "86400s")),
		rotateKeep:    mustInt(envOr("KKD_ROTATE_KEEP", "7")),
	}
	if v := os.Getenv("KKD_WIFI_IFACES"); v != "" {
		cfg.wifiIfaces = splitCSV(v)
	}
	if v := os.Getenv("KKD_BLE_IFACES"); v != "" {
		cfg.bleIfaces = splitCSV(v)
	}
	cfg.nodeID = os.Getenv("KKD_NODE_ID")
	if cfg.nodeID == "" {
		h, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("hostname: %w", err)
		}
		cfg.nodeID = h
	}
	if cfg.nodeID == "" {
		return nil, fmt.Errorf("KKD_NODE_ID unset and hostname is empty")
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitCSV(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if t := strings.TrimSpace(f); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func mustDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Fatalf("kkd-leaf: invalid duration %q: %v", s, err)
	}
	return d
}

func mustInt64(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		log.Fatalf("kkd-leaf: invalid int64 %q: %v", s, err)
	}
	return n
}

func mustInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("kkd-leaf: invalid int %q: %v", s, err)
	}
	return n
}
