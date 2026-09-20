//go:build !android && !tinygo

package fontscan

import (
	"fmt"
	"os"
)

func platformCacheDir() (string, error) {
	configDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving index cache path: %s", err)
	}
	return configDir, nil
}
