// +build !windows,!linux,!darwin android

package clipboard

func Start() {
}

func Get() string {
	return ""
}

func GetPrimary() string {
	return ""
}

func Set(text string) {
}
