package winproxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveStateRoundTripIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "previous-proxy.json")
	want := State{Enabled: true, Server: "proxy.example:8080", Override: "<local>"}
	if err := SaveState(path, want); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if got != want {
		t.Fatalf("LoadState() = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary state file remains, stat error = %v", err)
	}
}

func TestParseRegQuery(t *testing.T) {
	state := parseRegQuery(`
HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Internet Settings
    ProxyEnable    REG_DWORD    0x1
    ProxyServer    REG_SZ    http=127.0.0.1:8899;https=127.0.0.1:8899
    ProxyOverride  REG_SZ    localhost;127.0.0.1;<local>
`)
	if !state.Enabled || state.Server != "http=127.0.0.1:8899;https=127.0.0.1:8899" || state.Override != "localhost;127.0.0.1;<local>" {
		t.Fatalf("parseRegQuery() = %+v", state)
	}
}

func TestMatchesEnabledProxy(t *testing.T) {
	wantServer := "127.0.0.1:8899"
	wantOverride := "localhost;127.0.0.1;<local>"
	tests := []struct {
		name  string
		state State
		want  bool
	}{
		{name: "exact match", state: State{Enabled: true, Server: wantServer, Override: wantOverride}, want: true},
		{name: "disabled", state: State{Server: wantServer, Override: wantOverride}, want: false},
		{name: "different server", state: State{Enabled: true, Server: "127.0.0.1:9000", Override: wantOverride}, want: false},
		{name: "different override", state: State{Enabled: true, Server: wantServer, Override: "<local>"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesEnabledProxy(test.state, wantServer, wantOverride); got != test.want {
				t.Fatalf("matchesEnabledProxy() = %t, want %t", got, test.want)
			}
		})
	}
}
