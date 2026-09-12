//go:build !windows

package main

import "os"

// Persist the new directory entry after the image file itself has been synced.
func syncImageDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
