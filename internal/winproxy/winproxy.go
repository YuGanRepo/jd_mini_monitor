package winproxy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const internetSettingsKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`

type State struct {
	Enabled  bool   `json:"enabled"`
	Server   string `json:"server"`
	Override string `json:"override"`
}

func Read() (State, error) {
	if runtime.GOOS != "windows" {
		return State{}, fmt.Errorf("system proxy control is only supported on Windows")
	}
	output, err := exec.Command("reg", "query", internetSettingsKey).CombinedOutput()
	if err != nil {
		return State{}, fmt.Errorf("query proxy registry failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseRegQuery(string(output)), nil
}

func Enable(server string, override string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("system proxy control is only supported on Windows")
	}
	if server == "" {
		return fmt.Errorf("proxy server is required")
	}
	if override == "" {
		override = "localhost;127.0.0.1;<local>"
	}

	if err := regAddDWORD("ProxyEnable", 1); err != nil {
		return err
	}
	if err := regAddString("ProxyServer", server); err != nil {
		return err
	}
	if err := regAddString("ProxyOverride", override); err != nil {
		return err
	}
	return notifySettingsChanged()
}

func Restore(state State) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("system proxy control is only supported on Windows")
	}
	enabled := 0
	if state.Enabled {
		enabled = 1
	}
	if err := regAddDWORD("ProxyEnable", enabled); err != nil {
		return err
	}
	if err := regAddString("ProxyServer", state.Server); err != nil {
		return err
	}
	if err := regAddString("ProxyOverride", state.Override); err != nil {
		return err
	}
	return notifySettingsChanged()
}

func SaveState(path string, state State) error {
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o600)
}

func LoadState(path string) (State, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(content, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func regAddDWORD(name string, value int) error {
	output, err := exec.Command("reg", "add", internetSettingsKey, "/v", name, "/t", "REG_DWORD", "/d", strconv.Itoa(value), "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("set %s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func regAddString(name string, value string) error {
	if value == "" {
		output, err := exec.Command("reg", "delete", internetSettingsKey, "/v", name, "/f").CombinedOutput()
		if err != nil && !strings.Contains(strings.ToLower(string(output)), "unable to find") {
			return fmt.Errorf("delete %s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	output, err := exec.Command("reg", "add", internetSettingsKey, "/v", name, "/t", "REG_SZ", "/d", value, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("set %s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func notifySettingsChanged() error {
	script := `
Add-Type -Namespace WinInet -Name Native -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("wininet.dll", SetLastError=true)]
public static extern bool InternetSetOption(System.IntPtr hInternet, int dwOption, System.IntPtr lpBuffer, int dwBufferLength);
'@
[WinInet.Native]::InternetSetOption([IntPtr]::Zero, 39, [IntPtr]::Zero, 0) | Out-Null
[WinInet.Native]::InternetSetOption([IntPtr]::Zero, 37, [IntPtr]::Zero, 0) | Out-Null
`
	output, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("notify proxy settings failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func parseRegQuery(output string) State {
	var state State
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			continue
		}
		name := fields[0]
		value := strings.Join(fields[2:], " ")
		switch strings.ToLower(name) {
		case "proxyenable":
			parsed, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(value), "0x"), 16, 64)
			state.Enabled = err == nil && parsed != 0
		case "proxyserver":
			state.Server = value
		case "proxyoverride":
			state.Override = value
		}
	}
	return state
}
