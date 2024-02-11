// Package dup contains functions for finding duplicate files.
package dup

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func Indexes[T any](input []T, compareFn CompareFunc[T]) []int {
	fn := func(_ context.Context, left, right T) (selection, error) {
		return compareFn(left, right)
	}
	return IndexesContext(context.Background(), input, fn)
}

var SkipRemaining = errors.New("skip remaining")

// IndexesContext returns a slice of indexes from input that contain duplicate items as determined by compareFn.
//
// Results are returned in O(n^2) time
func IndexesContext[T any](ctx context.Context, input []T, compareFn CompareFuncContext[T]) (duplicates []int) {
	n := len(input)
	size := (n*n - n) / 2
	skipMatrix := make([]bool, size)

	for row := 0; row < n-1; row++ {
		for col := row + 1; col < n; col++ {
			// a previous duplicate match  means we can skip this comparison
			if skipMatrix[Offset(n, row, col)] {
				slog.Debug("skipping comparison",
					"left", input[row],
					"right", input[col],
				)
				continue
			}

			dup, err := compareFn(ctx, input[row], input[col])
			// if errors.Is(err, SkipRemaining) {
			// 	return duplicates // this should probably return an error to indicate indexing didn't complete
			// }
			if err != nil {
				slog.Error("comparison failure",
					"left", input[row],
					"right", input[col],
					"err", err,
				)
				continue
			}
			slog.Info("comparison",
				"left", input[row],
				"right", input[col],
				"duplicate", dup.String(),
			)
			switch dup {
			case None:
				continue
			case Left: // when the first arg given to selectDup was decided to be the duplicate file
				for c := col + 1; c < n; c++ {
					skipMatrix[Offset(n, row, c)] = true
				}
				duplicates = append(duplicates, row)
			case Right: // when the second arg given to selectDup was decided to be the duplicate file
				for r, c := col, col+1; c < n; c++ {
					skipMatrix[Offset(n, r, c)] = true
				}
				duplicates = append(duplicates, col)
			default:
				panic(fmt.Sprintf("invalid selection option %d", dup))
			}

		}
	}
	return duplicates
}

type CompareFunc[T any] func(T, T) (selection, error)
type CompareFuncContext[T any] func(context.Context, T, T) (selection, error)

type selection uint8

const (
	None selection = iota
	Left
	Right
)

func (s selection) String() string {
	switch s {
	case None:
		return "None"
	case Left:
		return "Left"
	case Right:
		return "Right"
	default:
		return fmt.Sprintf("invalid selection (%d)", s)
	}
}

// FilenameFn is used with IndexesContext to compare two files by their full file path,
// using os.Open to read and compare the contents of each file.
//
// If ctx is cancelled early then comparison will return early, but not immediately, with ctx.Err().
// More bytes may have been read by the internal buffer than were compared.
//
// selection will always be None when err is not nil.
func FilenameFn(ctx context.Context, left, right string) (selection selection, err error) {

	if left == right {
		return None, errSameItem
	}

	f1, err := os.Open(left)
	if err != nil {
		return None, err
	}
	defer f1.Close()
	f2, err := os.Open(right)
	if err != nil {
		return None, err
	}
	defer f2.Close()

	eq, err := equalFile(ctx, f1, f2)
	if !eq || err != nil {
		return None, err
	}

	return selectDup(f1, f2)
}

var errSameItem = errors.New("comparing item with itself")

func equalFile(ctx context.Context, f1, f2 fs.File) (bool, error) {
	// bufSize should be large enough to reduce head thrashing on spinning disks,
	// but small enough to exit quickly on comparison failure while keeping memory usage reasonable.
	const bufSize = 4096 * 4000
	br1 := bufio.NewReaderSize(f1, bufSize)
	br2 := bufio.NewReaderSize(f2, bufSize)

	buf1 := make([]byte, 4096)
	buf2 := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		n1, err1 := br1.Read(buf1)

		n2, err2 := io.ReadFull(br2, buf2[:n1])

		if n1 != n2 {
			return false, fmt.Errorf("read size mismatch: %w", errors.Join(err1, err2))
		}

		if !bytes.Equal(buf1[:n1], buf2[:n2]) {
			return false, nil
		}

		// two identical files should reach EOF at the same time
		if err1 == io.EOF {
			// io.ReadFull returns 0,nil if length of buf was 0.
			// length of buf should only be 0 if n1 was 0.
			if n1 == 0 && n2 == 0 && err2 == nil {
				return true, nil
			}

			// I don't think this case would trigger unless the underlying io.Reader returned bytes along with EOF on the last call?
			if err2 == io.EOF {
				_, sourceFile, sourceLine, ok := runtime.Caller(0)
				if ok {
					slog.Debug("unexpected condition reached", "source_file", sourceFile, "line", sourceLine)
				} else {
					slog.Debug("unexpected condition reached", "ctrl_f", "deaedc27-fee9-468f-9d0f-0efff8bee79e")
				}
				return true, nil
			}
		}

		// any errors that aren't EOF are a comparison failure
		if err1 != nil || err2 != nil {
			return false, fmt.Errorf("n1=%d, n2=%d, reader 1 error: %w, reader 2 error: %w; ", n1, n2, err1, err2)
		}
	}
}

func Offset(n, row, col int) int {
	// n*row+col
	return (n*row + col) - (((row+1)*(row+1)-(row+1))/2 + row + 1)
}

// selectDup decides which is considered a duplicate based on a set of heuristics.
func selectDup(f1, f2 fs.File) (selection, error) {
	fi1, err := f1.Stat()
	if err != nil {
		return None, err
	}
	fi2, err := f2.Stat()
	if err != nil {
		return None, err
	}
	if fi1.Size() != fi2.Size() {
		return None, errImpossible{errors.New("comparison on differently sized files")}
	}
	if fi1.Size() == 0 || fi2.Size() == 0 {
		return None, errImpossible{errors.New("duplicate selection on empty files")}
	}
	if fi1.IsDir() || fi2.IsDir() {
		return None, errImpossible{errors.New("duplicate comparison contained a directory")}
	}
	if isSymlink(fi1) || isSymlink(fi2) {
		return None, errImpossible{errors.New("duplicate comparison contained a symlink")}
	}

	f1BaseName, f1Counter, f1Ext := SplitFileBaseName(fi1.Name())
	f2BaseName, f2Counter, f2Ext := SplitFileBaseName(fi2.Name())

	if f1Counter > f2Counter {
		return Left, nil
	}
	if f1Counter < f2Counter {
		return Right, nil
	}

	if f1Ext != "" && f2Ext == "" {
		return Right, nil
	}
	if f1Ext == "" && f2Ext != "" {
		return Left, nil
	}

	if isDigits(f1BaseName) && !isDigits(f2BaseName) {
		return Left, nil
	}
	if !isDigits(f1BaseName) && isDigits(f2BaseName) {
		return Right, nil
	}

	if fi1.ModTime().Before(fi2.ModTime()) {
		return Right, nil
	}
	if fi1.ModTime().After(fi2.ModTime()) {
		return Left, nil
	}

	return Right, nil

}

// SplitFileBaseName splits a filename like "flowers (1).jpg" into ("flowers", 1, "jpg").
// counter is determined heuristically to guess how many copies deep the filename is,
// e.g. "flowers - Copy (3) - Copy - Copy.jpg" is guessed to be the 5th copy.
// prefix is the guessed original name without the extension.
func SplitFileBaseName(name string) (prefix string, counter int, ext string) {
	defer func() {
		// if we were about to return garbage,
		// just give up and return the original name
		if prefix == "" && ext == "" {
			prefix = name
			counter = 0
			ext = ""
		}
	}()
	ext = filepath.Ext(name)
	prefix = name[:len(name)-len(ext)]
	for {
		wmatch := windowsPattern.FindStringSubmatch(prefix)
		if wmatch != nil {
			prefix = strings.TrimSuffix(prefix, wmatch[0])
			n, _ := strconv.Atoi(wmatch[1])
			switch {
			case n == 0:
				counter++
			case n == 1:
				// special case: windows would skip Copy (1) through ctrl+v
				counter += 2
			case n < 0:
				panic("should not have been able to match a negative number")
			default:
				counter += n
			}
			continue // prevent the chrome match from running before checking for windows pattern again
		}
		cmatch := chromePattern.FindStringSubmatch(prefix)
		if cmatch != nil {
			prefix = strings.TrimSuffix(prefix, cmatch[0])
			n, _ := strconv.Atoi(cmatch[1])
			switch {
			case n == 0:
				counter++
			case n < 0:
				panic("should not have been able to match a negative number")
			default:
				counter += n
			}
		}

		if wmatch == nil && cmatch == nil {
			return
		}
	}
}

var windowsPattern = regexp.MustCompile(` - Copy(?: \((\d+)\))?$`)
var chromePattern = regexp.MustCompile(` \((\d+)\)$`)

func isDigits(s string) bool {
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	return false
}

func isSymlink(fi fs.FileInfo) bool {
	return fi.Mode()&fs.ModeSymlink != 0
}

type errImpossible struct {
	e error
}

func (e errImpossible) Unwrap() error { return e.e }
func (e errImpossible) Error() string { return "error should not be possible: " + e.Error() }
