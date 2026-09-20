package fontscan

import "fmt"

func platformCacheDir() (string, error) {
	// There is no stable way to infer the proper place to store the cache
	// with access to the Java runtime for the application. Rather than
	// clutter our API with that, require the caller to provide a path.
	return "", fmt.Errorf("user must provide cache directory on android")
}
