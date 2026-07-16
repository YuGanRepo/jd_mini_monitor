//go:build !windows

package license

func volumeSerial() string {
	return ""
}
