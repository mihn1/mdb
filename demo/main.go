package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/db"
	"golang.org/x/time/rate"
)

type scenario struct {
	name        string
	description string
	fn          func(context.Context, *scenarioConfig) (ScenarioResult, error)
}

type scenarioConfig struct {
	BaseDir           string
	DropData          bool
	EnableBloomFilter bool
	Seed              int64
}

type boolFlag struct {
	value bool
}

func (b *boolFlag) String() string {
	return strconv.FormatBool(b.value)
}

func (b *boolFlag) Set(val string) error {
	if val == "" {
		b.value = true
		return nil
	}
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		return err
	}
	b.value = parsed
	return nil
}

func (b *boolFlag) IsBoolFlag() bool { return true }

func (b *boolFlag) Value() bool { return b.value }

// ScenarioResult captures the metrics produced by a demo scenario.
type ScenarioResult struct {
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	DurationMillis      int64                  `json:"duration_ms"`
	WriteDurationMillis int64                  `json:"write_duration_ms,omitempty"`
	ReadDurationMillis  int64                  `json:"read_duration_ms,omitempty"`
	ItemsWritten        int                    `json:"items_written"`
	ItemsRead           int                    `json:"items_read"`
	PutErrors           int                    `json:"put_errors"`
	GetErrors           int                    `json:"get_errors"`
	WriteRate           float64                `json:"writes_per_sec"`
	ReadRate            float64                `json:"reads_per_sec"`
	Flushes             uint64                 `json:"flushes"`
	SSTables            int                    `json:"sstables"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
}

func newScenarioOptions(cfg *scenarioConfig) *common.Options {
	opts := common.NewDefaultOptions()
	opts.EnableDebugLogging = false
	opts.DataBlockSize = 8 * 1024
	opts.EnableBloomFilter = cfg.EnableBloomFilter
	return opts
}

const (
	benchmarkWriterCount   = 8
	benchmarkReaderCount   = 12
	benchmarkKeysPerWriter = 125000
	highLoadReadRatePerSec = 60000
	highLoadReadBurst      = 2000
)

func writerKey(writerID, keyIndex int) []byte {
	return []byte(fmt.Sprintf("writer:%02d:key:%05d", writerID, keyIndex))
}

func writerValue(writerID, keyIndex int) []byte {
	return []byte(fmt.Sprintf("value-%02d-%05d", writerID, keyIndex))
}

func verifyWriterValue(writerID, keyIndex int, value []byte) bool {
	if value == nil {
		return false
	}
	return bytes.Equal(value, writerValue(writerID, keyIndex))
}

func main() {
	scenarioFlag := flag.String("scenario", "all", "Scenario to run (basic, high_load, all)")
	baseDirFlag := flag.String("base_dir", "./db/demo_runs", "Directory for scenario data")
	keepDataFlag := flag.Bool("keep_data", false, "Keep existing scenario data instead of resetting")
	enableFilterFlag := &boolFlag{value: true}
	flag.Var(enableFilterFlag, "enable_filter", "Enable filter")
	reportFlag := flag.String("report", "", "Optional path to write a JSON report")
	timeoutFlag := flag.Duration("timeout", 0, "Optional overall timeout (e.g. 30s)")
	seedFlag := flag.Int64("seed", time.Now().UnixNano(), "Seed for random generators")
	flag.Parse()

	scenarios := map[string]scenario{
		"basic": {
			name:        "basic",
			description: "Sequential write/read correctness check with small memtables",
			fn:          runBasicScenario,
		},
		"write_bench": {
			name:        "write_bench",
			description: "Parallel writer benchmark (no concurrent reads)",
			fn:          runWriteBenchmark,
		},
		"read_bench": {
			name:        "read_bench",
			description: "Read-only benchmark against a preloaded dataset",
			fn:          runReadBenchmark,
		},
		"high_load": {
			name:        "high_load",
			description: "High-concurrency workload exercising flushes and compactions",
			fn:          runHighLoadScenario,
		},
	}
	scenarioOrder := []string{"basic", "write_bench", "read_bench", "high_load"}

	requested := parseScenarioSelection(*scenarioFlag)
	runList := buildRunList(requested, scenarios, scenarioOrder)
	if len(runList) == 0 {
		fmt.Println("No scenarios selected. Available options: basic, high_load, all")
		os.Exit(1)
	}

	if err := os.MkdirAll(*baseDirFlag, 0o755); err != nil {
		fmt.Printf("failed to prepare base directory %s: %v\n", *baseDirFlag, err)
		os.Exit(1)
	}

	ctx := context.Background()
	if *timeoutFlag > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeoutFlag)
		defer cancel()
	}

	runNames := make([]string, 0, len(runList))
	for _, sc := range runList {
		runNames = append(runNames, sc.name)
	}

	fmt.Println("MDB Scenario Runner")
	fmt.Println("===================")
	fmt.Printf("Base directory : %s\n", *baseDirFlag)
	fmt.Printf("Selected       : %s\n", strings.Join(runNames, ", "))
	fmt.Printf("Keep data      : %t\n", *keepDataFlag)
	fmt.Printf("Bloom filter   : %t\n", enableFilterFlag.Value())
	fmt.Println()

	cfg := scenarioConfig{
		BaseDir:           *baseDirFlag,
		DropData:          !*keepDataFlag,
		Seed:              *seedFlag,
		EnableBloomFilter: enableFilterFlag.Value(),
	}

	results := make([]ScenarioResult, 0, len(runList))
	for idx, sc := range runList {
		scenarioSeed := cfg.Seed + int64(idx)*1000
		scCfg := cfg
		scCfg.Seed = scenarioSeed

		fmt.Printf("Running scenario %q...\n", sc.name)
		res, err := sc.fn(ctx, &scCfg)
		if err != nil {
			fmt.Printf("Scenario %s failed: %v\n", sc.name, err)
			os.Exit(1)
		}
		results = append(results, res)
		displayResult(res)
	}

	if *reportFlag != "" {
		if err := writeJSONReport(*reportFlag, results); err != nil {
			fmt.Printf("failed to write JSON report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Report written to %s\n", *reportFlag)
	}
}

func runBasicScenario(ctx context.Context, cfg *scenarioConfig) (result ScenarioResult, err error) {
	result.Name = "basic"
	result.Description = "Sequential write/read correctness check with small memtables"
	scenarioPath := filepath.Join(cfg.BaseDir, result.Name)
	if cfg.DropData {
		if err := os.RemoveAll(scenarioPath); err != nil {
			return result, fmt.Errorf("reset scenario directory: %w", err)
		}
	}
	if err := os.MkdirAll(scenarioPath, 0o755); err != nil {
		return result, fmt.Errorf("create scenario directory: %w", err)
	}

	opts := newScenarioOptions(cfg)

	dbHandle, err := db.Open(scenarioPath, opts)
	if err != nil {
		return result, fmt.Errorf("open database: %w", err)
	}

	start := time.Now()
	defer func() {
		if result.DurationMillis == 0 {
			result.DurationMillis = int64(time.Since(start) / time.Millisecond)
		}
		stats := dbHandle.Stats()
		result.Flushes = stats.Flushes
		tables, tableErr := countSSTables(scenarioPath)
		if tableErr == nil {
			result.SSTables = tables
		} else if err == nil {
			err = tableErr
		}
		closeErr := dbHandle.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	const itemCount = 1500
	writeStart := time.Now()
	for i := 0; i < itemCount; i++ {
		if ctxErr := contextErr(ctx); ctxErr != nil {
			return result, ctxErr
		}
		key := []byte(fmt.Sprintf("key:%05d", i))
		value := []byte(fmt.Sprintf("value-%05d", i))
		if err := dbHandle.Put(key, value); err != nil {
			result.PutErrors++
			continue
		}
		result.ItemsWritten++
	}
	result.WriteDurationMillis = int64(time.Since(writeStart) / time.Millisecond)
	if result.WriteDurationMillis == 0 {
		result.WriteDurationMillis = 1
	}

	readStart := time.Now()
	for i := itemCount - 1; i >= 0; i-- {
		if ctxErr := contextErr(ctx); ctxErr != nil {
			return result, ctxErr
		}
		key := []byte(fmt.Sprintf("key:%05d", i))
		expected := fmt.Sprintf("value-%05d", i)
		value, err := dbHandle.Get(key)
		if err != nil {
			result.GetErrors++
			continue
		}
		if string(value) != expected {
			result.GetErrors++
			continue
		}
		result.ItemsRead++
	}
	result.ReadDurationMillis = int64(time.Since(readStart) / time.Millisecond)
	if result.ReadDurationMillis == 0 {
		result.ReadDurationMillis = 1
	}

	if result.Metadata == nil {
		result.Metadata = make(map[string]interface{})
	}
	result.Metadata["requested_items"] = itemCount
	result.Metadata["memtable_size_bytes"] = opts.MemTableSize

	result.DurationMillis = int64(time.Since(start) / time.Millisecond)
	if result.DurationMillis == 0 {
		result.DurationMillis = 1
	}
	if result.WriteDurationMillis > 0 {
		seconds := float64(result.WriteDurationMillis) / 1000
		result.WriteRate = float64(result.ItemsWritten) / seconds
	}
	if result.ReadDurationMillis > 0 {
		seconds := float64(result.ReadDurationMillis) / 1000
		result.ReadRate = float64(result.ItemsRead) / seconds
	}

	return result, nil
}

func runWriteBenchmark(ctx context.Context, cfg *scenarioConfig) (result ScenarioResult, err error) {
	result.Name = "write_bench"
	result.Description = "Parallel writer benchmark (no concurrent reads)"
	scenarioPath := filepath.Join(cfg.BaseDir, result.Name)
	if cfg.DropData {
		if err := os.RemoveAll(scenarioPath); err != nil {
			return result, fmt.Errorf("reset scenario directory: %w", err)
		}
	}
	if err := os.MkdirAll(scenarioPath, 0o755); err != nil {
		return result, fmt.Errorf("create scenario directory: %w", err)
	}

	opts := newScenarioOptions(cfg)

	dbHandle, err := db.Open(scenarioPath, opts)
	if err != nil {
		return result, fmt.Errorf("open database: %w", err)
	}

	start := time.Now()
	defer func() {
		if result.DurationMillis == 0 {
			result.DurationMillis = millisecondsSince(start)
		}
		stats := dbHandle.Stats()
		result.Flushes = stats.Flushes
		tables, tableErr := countSSTables(scenarioPath)
		if tableErr == nil {
			result.SSTables = tables
		} else if err == nil {
			err = tableErr
		}
		closeErr := dbHandle.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	writes, putErrors, writeDuration, sampleErrors := runWriterWorkload(ctx, dbHandle, benchmarkWriterCount, benchmarkKeysPerWriter, cfg.Seed)
	result.ItemsWritten = int(writes)
	result.PutErrors = int(putErrors)
	result.WriteDurationMillis = durationMillis(writeDuration)
	if result.WriteDurationMillis > 0 {
		seconds := float64(result.WriteDurationMillis) / 1000
		result.WriteRate = float64(result.ItemsWritten) / seconds
	}
	result.DurationMillis = millisecondsSince(start)
	if result.Metadata == nil {
		result.Metadata = make(map[string]interface{})
	}
	result.Metadata["writer_count"] = benchmarkWriterCount
	result.Metadata["keys_per_writer"] = benchmarkKeysPerWriter
	result.Metadata["benchmark_type"] = "write-only"
	if len(sampleErrors) > 0 {
		result.Metadata["sample_errors"] = sampleErrors
	}
	return result, nil
}

func runReadBenchmark(ctx context.Context, cfg *scenarioConfig) (result ScenarioResult, err error) {
	result.Name = "read_bench"
	result.Description = "Read-only benchmark against a preloaded dataset"
	scenarioPath := filepath.Join(cfg.BaseDir, result.Name)
	if cfg.DropData {
		if err := os.RemoveAll(scenarioPath); err != nil {
			return result, fmt.Errorf("reset scenario directory: %w", err)
		}
	}
	if err := os.MkdirAll(scenarioPath, 0o755); err != nil {
		return result, fmt.Errorf("create scenario directory: %w", err)
	}

	opts := newScenarioOptions(cfg)

	dbHandle, err := db.Open(scenarioPath, opts)
	if err != nil {
		return result, fmt.Errorf("open database: %w", err)
	}

	start := time.Now()
	defer func() {
		if result.DurationMillis == 0 {
			result.DurationMillis = millisecondsSince(start)
		}
		stats := dbHandle.Stats()
		result.Flushes = stats.Flushes
		tables, tableErr := countSSTables(scenarioPath)
		if tableErr == nil {
			result.SSTables = tables
		} else if err == nil {
			err = tableErr
		}
		closeErr := dbHandle.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	seededWrites, err := ensureDatasetForReadBenchmark(ctx, dbHandle, scenarioPath, benchmarkWriterCount, benchmarkKeysPerWriter, cfg.Seed)
	if err != nil {
		return result, err
	}
	opsPerReader := benchmarkWriterCount * benchmarkKeysPerWriter / benchmarkReaderCount
	if opsPerReader == 0 {
		opsPerReader = benchmarkKeysPerWriter
	}
	reads, getErrors, readDuration, sampleErrors := runReaderWorkload(ctx, dbHandle, benchmarkReaderCount, opsPerReader, benchmarkWriterCount, benchmarkKeysPerWriter, cfg.Seed+42)
	result.ItemsRead = int(reads)
	result.GetErrors = int(getErrors)
	result.ReadDurationMillis = durationMillis(readDuration)
	if result.ReadDurationMillis > 0 {
		seconds := float64(result.ReadDurationMillis) / 1000
		result.ReadRate = float64(result.ItemsRead) / seconds
	}
	result.DurationMillis = millisecondsSince(start)
	if result.Metadata == nil {
		result.Metadata = make(map[string]interface{})
	}
	result.Metadata["reader_count"] = benchmarkReaderCount
	result.Metadata["lookups_per_reader"] = opsPerReader
	result.Metadata["writer_count"] = benchmarkWriterCount
	result.Metadata["keys_per_writer"] = benchmarkKeysPerWriter
	result.Metadata["benchmark_type"] = "read-only"
	if seededWrites > 0 {
		result.Metadata["prefill_items"] = seededWrites
	}
	if len(sampleErrors) > 0 {
		result.Metadata["sample_errors"] = sampleErrors
	}
	return result, nil
}

func runHighLoadScenario(ctx context.Context, cfg *scenarioConfig) (result ScenarioResult, err error) {
	result.Name = "high_load"
	result.Description = "High-concurrency workload exercising flushes and compactions"
	scenarioPath := filepath.Join(cfg.BaseDir, result.Name)
	if cfg.DropData {
		if err := os.RemoveAll(scenarioPath); err != nil {
			return result, fmt.Errorf("reset scenario directory: %w", err)
		}
	}
	if err := os.MkdirAll(scenarioPath, 0o755); err != nil {
		return result, fmt.Errorf("create scenario directory: %w", err)
	}

	opts := newScenarioOptions(cfg)

	dbHandle, err := db.Open(scenarioPath, opts)
	if err != nil {
		return result, fmt.Errorf("open database: %w", err)
	}

	start := time.Now()
	defer func() {
		if result.DurationMillis == 0 {
			result.DurationMillis = int64(time.Since(start) / time.Millisecond)
		}
		stats := dbHandle.Stats()
		result.Flushes = stats.Flushes
		tables, tableErr := countSSTables(scenarioPath)
		if tableErr == nil {
			result.SSTables = tables
		} else if err == nil {
			err = tableErr
		}
		closeErr := dbHandle.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	randSeed := cfg.Seed
	baseRand := rand.New(rand.NewSource(randSeed))

	var writes uint64
	var reads uint64
	var putErrors uint64
	var getErrors uint64
	writerProgress := make([]int64, benchmarkWriterCount)

	errCh := make(chan error, benchmarkWriterCount*benchmarkKeysPerWriter)

	var writerWG sync.WaitGroup
	writerWG.Add(benchmarkWriterCount)
	writerStart := time.Now()

	for writerID := 0; writerID < benchmarkWriterCount; writerID++ {
		id := writerID
		go func(writerID int) {
			defer writerWG.Done()
			for i := 0; i < benchmarkKeysPerWriter; i++ {
				if ctxErr := contextErr(ctx); ctxErr != nil {
					recordError(errCh, ctxErr)
					return
				}
				key := writerKey(writerID, i)
				value := writerValue(writerID, i)
				if err := dbHandle.Put(key, value); err != nil {
					atomic.AddUint64(&putErrors, 1)
					recordError(errCh, fmt.Errorf("put %s: %w", key, err))
					continue
				}
				atomic.AddUint64(&writes, 1)
				atomic.StoreInt64(&writerProgress[writerID], int64(i+1))
			}
		}(id)
	}

	stopReaders := make(chan struct{})
	var readLimiter *rate.Limiter
	if highLoadReadRatePerSec > 0 {
		readLimiter = rate.NewLimiter(rate.Limit(highLoadReadRatePerSec), highLoadReadBurst)
	}

	var readerWG sync.WaitGroup
	readerWG.Add(benchmarkReaderCount)
	readerStart := time.Now()

	for readerID := 0; readerID < benchmarkReaderCount; readerID++ {
		id := readerID
		readerSeed := baseRand.Int63()
		go func(readerID int, seed int64) {
			defer readerWG.Done()
			readerRng := rand.New(rand.NewSource(seed))
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				if ctxErr := contextErr(ctx); ctxErr != nil {
					recordError(errCh, fmt.Errorf("reader %02d: %w", readerID, ctxErr))
					return
				}
				if readLimiter != nil {
					if err := readLimiter.Wait(ctx); err != nil {
						recordError(errCh, fmt.Errorf("reader %02d limiter: %w", readerID, err))
						return
					}
				}
				candidateWriter := readerRng.Intn(benchmarkWriterCount)
				candidateKey := readerRng.Intn(benchmarkKeysPerWriter)
				if atomic.LoadInt64(&writerProgress[candidateWriter]) <= int64(candidateKey) {
					continue
				}
				key := writerKey(candidateWriter, candidateKey)
				value, err := dbHandle.Get(key)
				if err != nil {
					atomic.AddUint64(&getErrors, 1)
					recordError(errCh, fmt.Errorf("reader %02d get %s: %w", readerID, key, err))
					continue
				}
				if !verifyWriterValue(candidateWriter, candidateKey, value) {
					atomic.AddUint64(&getErrors, 1)
					recordError(errCh, fmt.Errorf("reader %02d mismatch %s", readerID, key))
					continue
				}
				atomic.AddUint64(&reads, 1)
			}
		}(id, readerSeed)
	}

	writerWG.Wait()

	result.WriteDurationMillis = int64(time.Since(writerStart) / time.Millisecond)
	if result.WriteDurationMillis == 0 {
		result.WriteDurationMillis = 1
	}

	close(stopReaders)
	readerWG.Wait()

	result.ReadDurationMillis = int64(time.Since(readerStart) / time.Millisecond)
	if result.ReadDurationMillis == 0 {
		result.ReadDurationMillis = 1
	}

	close(errCh)

	result.DurationMillis = int64(time.Since(start) / time.Millisecond)
	if result.DurationMillis == 0 {
		result.DurationMillis = 1
	}

	sampleErrors := make([]string, 0, 4)
	for err := range errCh {
		if len(sampleErrors) < cap(sampleErrors) {
			sampleErrors = append(sampleErrors, err.Error())
		}
	}

	result.ItemsWritten = int(atomic.LoadUint64(&writes))
	result.ItemsRead = int(atomic.LoadUint64(&reads))
	result.PutErrors = int(atomic.LoadUint64(&putErrors))
	result.GetErrors = int(atomic.LoadUint64(&getErrors))

	if result.WriteDurationMillis > 0 {
		seconds := float64(result.WriteDurationMillis) / 1000
		result.WriteRate = float64(result.ItemsWritten) / seconds
	}
	if result.ReadDurationMillis > 0 {
		seconds := float64(result.ReadDurationMillis) / 1000
		result.ReadRate = float64(result.ItemsRead) / seconds
	}

	if result.Metadata == nil {
		result.Metadata = make(map[string]interface{})
	}
	result.Metadata["writer_count"] = benchmarkWriterCount
	result.Metadata["keys_per_writer"] = benchmarkKeysPerWriter
	result.Metadata["reader_count"] = benchmarkReaderCount
	result.Metadata["seed"] = cfg.Seed
	if highLoadReadRatePerSec > 0 {
		result.Metadata["read_rate_limit_per_sec"] = highLoadReadRatePerSec
	}
	if len(sampleErrors) > 0 {
		result.Metadata["sample_errors"] = sampleErrors
	}

	return result, nil
}

func runWriterWorkload(ctx context.Context, dbHandle *db.DB, writerCount, keysPerWriter int, seed int64) (writes uint64, putErrors uint64, duration time.Duration, sampleErrors []string) {
	errCh := make(chan error, writerCount*keysPerWriter)
	start := time.Now()
	var wg sync.WaitGroup
	for writerID := 0; writerID < writerCount; writerID++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < keysPerWriter; i++ {
				if ctxErr := contextErr(ctx); ctxErr != nil {
					recordError(errCh, ctxErr)
					return
				}
				key := writerKey(writerID, i)
				value := writerValue(writerID, i)
				if err := dbHandle.Put(key, value); err != nil {
					atomic.AddUint64(&putErrors, 1)
					recordError(errCh, fmt.Errorf("put %s: %w", key, err))
					continue
				}
				atomic.AddUint64(&writes, 1)
			}
		}(writerID)
	}
	wg.Wait()
	duration = time.Since(start)
	close(errCh)
	sampleErrors = collectSampleErrors(errCh)
	return
}

func runReaderWorkload(ctx context.Context, dbHandle *db.DB, readerCount, opsPerReader, writerCount, keysPerWriter int, seed int64) (reads uint64, getErrors uint64, duration time.Duration, sampleErrors []string) {
	if opsPerReader <= 0 {
		opsPerReader = keysPerWriter
	}
	baseRand := rand.New(rand.NewSource(seed))
	errCh := make(chan error, readerCount*opsPerReader)
	start := time.Now()
	var wg sync.WaitGroup
	for readerID := 0; readerID < readerCount; readerID++ {
		id := readerID
		readerSeed := baseRand.Int63()
		wg.Add(1)
		go func(readerID int, seed int64) {
			defer wg.Done()
			readerRng := rand.New(rand.NewSource(seed))
			for i := 0; i < opsPerReader; i++ {
				if ctxErr := contextErr(ctx); ctxErr != nil {
					recordError(errCh, fmt.Errorf("reader %02d: %w", readerID, ctxErr))
					return
				}
				candidateWriter := readerRng.Intn(writerCount)
				candidateKey := readerRng.Intn(keysPerWriter)
				key := writerKey(candidateWriter, candidateKey)
				value, err := dbHandle.Get(key)
				if err != nil {
					atomic.AddUint64(&getErrors, 1)
					recordError(errCh, fmt.Errorf("reader %02d get %s: %w", readerID, key, err))
					continue
				}
				if !verifyWriterValue(candidateWriter, candidateKey, value) {
					atomic.AddUint64(&getErrors, 1)
					recordError(errCh, fmt.Errorf("reader %02d mismatch %s", readerID, key))
					continue
				}
				atomic.AddUint64(&reads, 1)
			}
		}(id, readerSeed)
	}
	wg.Wait()
	duration = time.Since(start)
	close(errCh)
	sampleErrors = collectSampleErrors(errCh)
	return
}

func ensureDatasetForReadBenchmark(ctx context.Context, dbHandle *db.DB, scenarioPath string, writerCount, keysPerWriter int, seed int64) (uint64, error) {
	tables, err := countSSTables(scenarioPath)
	if err != nil {
		return 0, err
	}
	if tables > 0 {
		return 0, nil
	}
	writes, putErrors, _, samples := runWriterWorkload(ctx, dbHandle, writerCount, keysPerWriter, seed)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return writes, ctxErr
	}
	if putErrors > 0 {
		msg := fmt.Sprintf("prefill encountered %d put errors", putErrors)
		if len(samples) > 0 {
			msg = fmt.Sprintf("%s (e.g. %s)", msg, samples[0])
		}
		return writes, errors.New(msg)
	}
	return writes, nil
}

func durationMillis(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	ms := d / time.Millisecond
	if ms == 0 {
		ms = 1
	}
	return int64(ms)
}

func millisecondsSince(start time.Time) int64 {
	return durationMillis(time.Since(start))
}

func collectSampleErrors(ch <-chan error) []string {
	samples := make([]string, 0, 4)
	for err := range ch {
		if len(samples) < cap(samples) {
			samples = append(samples, err.Error())
		}
	}
	return samples
}

func parseScenarioSelection(raw string) []string {
	segments := strings.Split(raw, ",")
	selections := make([]string, 0, len(segments))
	for _, segment := range segments {
		trimmed := strings.ToLower(strings.TrimSpace(segment))
		if trimmed == "" {
			continue
		}
		selections = append(selections, trimmed)
	}
	if len(selections) == 0 {
		return []string{"all"}
	}
	return selections
}

func buildRunList(requested []string, registry map[string]scenario, order []string) []scenario {
	runList := make([]scenario, 0, len(requested))
	seen := make(map[string]struct{})
	for _, sel := range requested {
		if sel == "all" {
			runList = runList[:0]
			seen = make(map[string]struct{})
			for _, name := range order {
				if sc, ok := registry[name]; ok {
					runList = append(runList, sc)
					seen[name] = struct{}{}
				}
			}
			break
		}
		if _, duplicate := seen[sel]; duplicate {
			continue
		}
		if sc, ok := registry[sel]; ok {
			runList = append(runList, sc)
			seen[sel] = struct{}{}
		}
	}
	return runList
}

func displayResult(res ScenarioResult) {
	fmt.Printf("  Description   : %s\n", res.Description)
	fmt.Printf("  Duration      : %s\n", time.Duration(res.DurationMillis)*time.Millisecond)
	fmt.Printf("  Writes        : %d\n", res.ItemsWritten)
	fmt.Printf("  Reads         : %d\n", res.ItemsRead)
	if res.WriteRate > 0 {
		fmt.Printf("  Writes/sec    : %.2f\n", res.WriteRate)
	}
	if res.ReadRate > 0 {
		fmt.Printf("  Reads/sec     : %.2f\n", res.ReadRate)
	}
	fmt.Printf("  Put errors    : %d\n", res.PutErrors)
	fmt.Printf("  Get errors    : %d\n", res.GetErrors)
	fmt.Printf("  Flushes       : %d\n", res.Flushes)
	fmt.Printf("  SSTables      : %d\n", res.SSTables)
	if len(res.Metadata) > 0 {
		fmt.Println("  Metadata      :")
		keys := make([]string, 0, len(res.Metadata))
		for key := range res.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Printf("    %s = %v\n", key, res.Metadata[key])
		}
	}
	fmt.Println()
}

func writeJSONReport(path string, results []ScenarioResult) error {
	payload := struct {
		GeneratedAt time.Time        `json:"generated_at"`
		Results     []ScenarioResult `json:"results"`
	}{
		GeneratedAt: time.Now().UTC(),
		Results:     results,
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("prepare report directory: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		return fmt.Errorf("write report json: %w", err)
	}

	return nil
}

func countSSTables(basePath string) (int, error) {
	tablesDir := filepath.Join(basePath, "tables")
	entries, err := os.ReadDir(tablesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("list tables: %w", err)
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

func recordError(ch chan<- error, err error) {
	if err == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

func contextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
