//go:build tinygo

package fontscan

import (
	"fmt"
	"os"
	"path/filepath"
)

func platformCacheDir() (string, error) {
	// if no path is provided we cannot get cache dir with tinygo, so just make one up.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving index cache path: %s", err)
	}
	return filepath.Join(homeDir, ".cache"), nil
}
