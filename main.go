package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync"

	"github.com/Travis-Britz/dedup/internal/dup"
)

var config = struct {
	Dirs    []string
	MinSize int64
	Debug   bool
	Verbose bool
	Execute bool
	H       handler
}{
	Dirs:    []string{"."},
	MinSize: 2048,
	Debug:   false,
	Verbose: false,
	Execute: false,
	H:       deleteHandler,
}

func main() {

	flag.BoolVar(&config.Verbose, "v", config.Verbose, "Enable verbose logging")
	flag.BoolVar(&config.Debug, "vvv", config.Debug, "Enable debug-level logging")
	flag.BoolVar(&config.Execute, "x", config.Execute, "Execute. The default is dry-run, which prints every duplicate file to stdout.")
	flag.Parse()

	if len(flag.Args()) > 0 {
		config.Dirs = flag.Args()
	}
	for i, d := range config.Dirs {
		config.Dirs[i] = filepath.Clean(d)
	}

	slog.SetLogLoggerLevel(slog.LevelError)
	if config.Verbose {
		slog.SetLogLoggerLevel(slog.LevelInfo)
	}
	if config.Debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	if !config.Execute {
		config.H = dryRun(config.H)
	}

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if config.H == nil {
		return errors.New("nil handler")
	}

	if err := validConfig(); err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c
		slog.Info("received interrupt")
		cancel()
		<-c
		slog.Error("received second interrupt; forcing exit")
		os.Exit(1)
	}()

	fileResults := compileDirResults(ctx, config.Dirs)
	buckets := stageBuckets(ctx, fileResults)
	for sizeBucket := range buckets {
		slog.Debug("comparing files",
			"files", sizeBucket,
			"count", len(sizeBucket),
		)
		dups := dup.IndexesContext(ctx, sizeBucket, dup.FilenameFn)
		for _, i := range dups {
			slog.Debug("handling duplicate", "file", sizeBucket[i])
			err := config.H.handle(sizeBucket[i])
			if err != nil {
				slog.Error("handler error", "file", sizeBucket[i], "err", err)
			}
		}
	}

	return nil
}

type handlerFunc func(string) error

func (f handlerFunc) handle(s string) error {
	return f(s)
}

type handler interface {
	handle(string) error
}

type fileResult struct {
	path string
	size int64
}

func stageBuckets(ctx context.Context, fileResults <-chan fileResult) <-chan []string {
	buckets := make(map[int64][]string)
	for fr := range fileResults {
		if fr.size < config.MinSize {
			slog.Debug("skipping file below MinSize", "size", fr.size, "file", fr.path)
			continue
		}
		if slices.Contains(buckets[fr.size], fr.path) {
			// this shouldn't happen unless a directory was given twice or one of the given directories was a subdir of another
			// any other cases should be investigated
			slog.Debug("path appeared twice in file listing", "file", fr.path)
			continue
		}
		buckets[fr.size] = append(buckets[fr.size], fr.path)
	}
	slog.Debug("finished listing directories", "bucket_count", len(buckets))

	possibleDuplicates := make(chan []string)
	go func() {
		defer close(possibleDuplicates)
		for _, v := range buckets {
			if len(v) > 1 {
				select {
				case <-ctx.Done():
					return
				case possibleDuplicates <- v:
				}
			}
		}
	}()

	return possibleDuplicates
}

// compileDirResults walks each of dirs in a separate goroutine and combines the result.
// The returned channel will be closed when there are no more results.
// The dirs are split into goroutines because the assumption is that some of the directories may be on different physical disks.
func compileDirResults(ctx context.Context, dirs []string) <-chan fileResult {

	var wg sync.WaitGroup
	fr := make(chan fileResult, 10000)
	go func(dirs []string) {
		defer close(fr)
		for _, dir := range dirs {
			wg.Add(1)
			go func(d string) {
				defer wg.Done()
				dr := listDirFiles(ctx, d)
				for f := range dr {
					select {
					case <-ctx.Done():
						return
					case fr <- f:
					}
				}
			}(dir)
		}
		wg.Wait()
	}(dirs)
	return fr
}

func listDirFiles(ctx context.Context, rootDir string) <-chan fileResult {
	slog.Debug("walking directory", "dir", rootDir)
	ch := make(chan fileResult)
	go func(rootDir string) {
		defer close(ch)
		var walkDirFn fs.WalkDirFunc = func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				slog.Error("unable to access file", "path", path, "err", err)
				return err
			}

			if d.IsDir() {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				slog.Error("failed to get file info", "err", err)
				return nil
			}

			if isSymlink(fi) {
				return nil
			}

			fr := fileResult{
				path: filepath.Join(rootDir, path),
				size: fi.Size(),
			}
			select {
			case <-ctx.Done():
				return fs.SkipAll
			case ch <- fr:
			}
			return nil
		}
		dirFS := os.DirFS(rootDir)
		fs.WalkDir(dirFS, ".", walkDirFn)
	}(rootDir)

	return ch
}

func isSymlink(fi fs.FileInfo) bool {
	return fi.Mode()&fs.ModeSymlink != 0
}

var deleteHandler handlerFunc = func(file string) error {
	slog.Info("removing file", "file", file)
	return os.Remove(file)
}

func dryRun(h handler) handlerFunc {
	return func(file string) error {
		fmt.Println(file)
		return nil
	}
}

func validConfig() error {
	if config.H == nil {
		return errors.New("nil handler")
	}
	if len(config.Dirs) < 1 {
		return errors.New("no directories given")
	}
	return nil
}
