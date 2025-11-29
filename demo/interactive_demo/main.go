package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/db"
)

const (
	defaultDBPath          = "./demo_runs/interactive"
	defaultSeedCount       = 10000
	defaultInteractiveHelp = `Available commands:
  help                         Show this help message
  put <key> <value>            Insert/update a key
  get <key>                    Retrieve and display a value
  del <key>                    Delete a key
  preload [count]              Insert count synthetic keys (default 1000)
  stats                        Display basic database statistics
  exit | quit                  Close the database and exit`
	walFileName = "wal.log"
)

type session struct {
	db      *db.DB
	baseDir string
}

func main() {
	dbPath := flag.String("db_path", defaultDBPath, "Directory where the interactive database lives")
	preloadCount := flag.Int("preload", defaultSeedCount, "Number of keys to seed when initializing an empty database")
	reset := flag.Bool("reset", false, "Reset the database before starting the session")
	bloom := flag.Bool("bloom", true, "Enable Bloom filters for SSTables")
	flag.Parse()

	if *reset {
		if err := os.RemoveAll(*dbPath); err != nil {
			fmt.Printf("failed to reset %s: %v\n", *dbPath, err)
			os.Exit(1)
		}
	}

	if err := os.MkdirAll(*dbPath, 0o755); err != nil {
		fmt.Printf("failed to prepare db path %s: %v\n", *dbPath, err)
		os.Exit(1)
	}

	opts := common.NewDefaultOptions()
	opts.EnableBloomFilter = *bloom

	handle, err := db.Open(*dbPath, opts)
	if err != nil {
		fmt.Printf("failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer handle.Close()

	fresh := *reset || !databaseHasData(*dbPath)
	if fresh {
		fmt.Printf("Initializing database with %d seed keys...\n", *preloadCount)
		start := time.Now()
		sample, err := seedDatabase(handle, *preloadCount, "seed")
		if err != nil {
			fmt.Printf("seed failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Seed data loaded in %s. Sample key: %s\n", time.Since(start).Truncate(time.Millisecond), sample)
	}

	fmt.Println("Interactive MDB shell. Type 'help' for a list of commands.")
	sess := &session{db: handle, baseDir: *dbPath}
	if err := sess.repl(); err != nil {
		fmt.Printf("session ended: %v\n", err)
	}
}

func (s *session) repl() error {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("mdb> ")
		if !scanner.Scan() {
			fmt.Println()
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := s.handleCommand(line); err != nil {
			fmt.Printf("error: %v\n", err)
		}
	}
}

func (s *session) handleCommand(line string) error {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	cmd := strings.ToLower(fields[0])

	switch cmd {
	case "help":
		fmt.Println(defaultInteractiveHelp)
	case "put":
		if len(fields) < 3 {
			return errors.New("usage: put <key> <value>")
		}
		key := fields[1]
		value := strings.Join(fields[2:], " ")
		if err := s.db.Put([]byte(key), []byte(value)); err != nil {
			return fmt.Errorf("put failed: %w", err)
		}
		fmt.Printf("ok (%s)\n", key)
	case "get":
		if len(fields) != 2 {
			return errors.New("usage: get <key>")
		}
		key := fields[1]
		value, err := s.db.Get([]byte(key))
		if err != nil {
			return fmt.Errorf("get failed: %w", err)
		}
		if value == nil {
			fmt.Println("(nil)")
		} else {
			fmt.Printf("%s\n", string(value))
		}
	case "del", "delete":
		if len(fields) != 2 {
			return errors.New("usage: delete <key>")
		}
		if err := s.db.Delete([]byte(fields[1])); err != nil {
			return fmt.Errorf("delete failed: %w", err)
		}
		fmt.Println("deleted")
	case "preload":
		count := 1000
		if len(fields) > 1 {
			n, err := strconv.Atoi(fields[1])
			if err != nil || n <= 0 {
				return errors.New("usage: preload [positive count]")
			}
			count = n
		}
		fmt.Printf("loading %d synthetic keys...\n", count)
		start := time.Now()
		label := fmt.Sprintf("batch_%d", time.Now().UnixNano())
		sample, err := seedDatabase(s.db, count, label)
		if err != nil {
			return fmt.Errorf("preload failed: %w", err)
		}
		fmt.Printf("done in %s (sample key: %s)\n", time.Since(start).Truncate(time.Millisecond), sample)
	case "stats":
		s.printStats()
	case "exit", "quit":
		fmt.Println("bye")
		os.Exit(0)
	default:
		fmt.Println("unknown command, type 'help' for options")
	}
	return nil
}

func (s *session) printStats() {
	stats := s.db.Stats()
	tables, _ := countSSTables(s.baseDir)
	fmt.Printf("Memtable bytes : %d\n", stats.MemTableSize)
	fmt.Printf("Flushes        : %d\n", stats.Flushes)
	fmt.Printf("SSTables       : %d\n", tables)
	fmt.Printf("Data path      : %s\n", s.baseDir)
}

func seedDatabase(handle *db.DB, count int, label string) (string, error) {
	var sampleKey string
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("%s:%06d", label, i)
		value := fmt.Sprintf("value-%s-%06d", label, i)
		if err := handle.Put([]byte(key), []byte(value)); err != nil {
			return "", err
		}
		if i == count/2 {
			sampleKey = key
		}
	}
	if sampleKey == "" && count > 0 {
		sampleKey = fmt.Sprintf("%s:%06d", label, 0)
	}
	return sampleKey, nil
}

func databaseHasData(basePath string) bool {
	tablesDir := filepath.Join(basePath, "tables")
	if entries, err := os.ReadDir(tablesDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sst" {
				return true
			}
		}
	}
	if _, err := os.Stat(filepath.Join(basePath, walFileName)); err == nil {
		return true
	}
	return false
}

func countSSTables(basePath string) (int, error) {
	tablesDir := filepath.Join(basePath, "tables")
	entries, err := os.ReadDir(tablesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".sst" {
			count++
		}
	}
	return count, nil
}
