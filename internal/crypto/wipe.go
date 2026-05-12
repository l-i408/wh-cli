package crypto

import (
	"fmt"
	"os"
)

// ZeroFile overwrites a file with zeros before removing it.
func ZeroFile(path string) error {
	// #nosec G304 -- wipe only receives explicit wh-cli runtime paths selected by callers.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open for wipe: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat for wipe: %w", err)
	}
	size := info.Size()
	if size > 0 {
		zeros := make([]byte, min(size, 1<<20))
		written := int64(0)
		for written < size {
			chunk := zeros
			if int64(len(chunk)) > size-written {
				chunk = zeros[:size-written]
			}
			n, err := f.WriteAt(chunk, written)
			if err != nil {
				_ = f.Close()
				return fmt.Errorf("zero write: %w", err)
			}
			written += int64(n)
		}
		_ = f.Sync()
	}
	_ = f.Close()
	return os.Remove(path)
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
