package license

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"runtime"
	"strings"
)

// DeviceID returns a deterministic hardware-bound fingerprint. It combines the
// local hostname with the system drive's volume serial number (Windows) or a
// fallback identifier on other platforms. When the hardware fingerprint
// cannot be determined a random per-machine fallback is used (not implemented
// here — the caller should fall back to a random UUID stored on disk, see
// DeviceIDOrDefault).
func DeviceID() string {
	return deviceID()
}

// DeviceIDOrDefault returns DeviceID(); if the result is empty the caller
// should generate and persist a random fallback (e.g. a v4 UUID written to
// <BaseDir>/device-id).
func DeviceIDOrDefault(fallback string) string {
	id := DeviceID()
	if strings.TrimSpace(id) != "" {
		return id
	}
	return fallback
}

// --------------------------------------------------------------------------
// Hardware fingerprint algorithm
// --------------------------------------------------------------------------

// deviceID builds a fingerprint from the hostname and (on Windows) the C:
// volume serial. On non-Windows systems it uses the hostname only — the
// calling layer should mix in stored entropy for a stable per-machine id.
func deviceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	// Normalise: strip domain suffix often appended by corp networks.
	if idx := strings.IndexByte(hostname, '.'); idx >= 0 {
		hostname = hostname[:idx]
	}

	volSerial := volumeSerial()
	macs := firstMACs(2)

	raw := strings.ToLower(strings.Join(
		[]string{hostname, volSerial},
		"|",
	))

	// When volume serial is unavailable (non-admin, non-NTFS, non-Windows),
	// mix in MAC addresses as additional entropy.
	if volSerial == "" && len(macs) > 0 {
		raw = strings.ToLower(strings.Join(
			[]string{hostname, strings.Join(macs, ",")},
			"|",
		))
	}

	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:16]) // 32 hex chars = 128 bits
}

// firstMACs returns the first n non-loopback hardware MAC addresses.
func firstMACs(n int) []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addr := iface.HardwareAddr.String()
		if addr == "" {
			continue
		}
		out = append(out, addr)
		if len(out) >= n {
			break
		}
	}
	return out
}

// volumeSerial returns the C: drive volume serial on Windows, empty on other
// platforms (stub in disk_other.go).
func volumeSerial() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	return diskVolumeSerialWindows()
}
