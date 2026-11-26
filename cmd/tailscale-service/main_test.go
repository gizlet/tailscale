package main

import (
	"slices"
	"strings"
	"testing"
)

func env(values map[string]string) lookupFunc {
	return func(key string) string { return values[key] }
}

func TestDaemonUsesGizletFolders(t *testing.T) {
	cmd, err := daemon("/opt/gizlet/services/io.gizlet.tailscale", env(map[string]string{
		"GIZLET_STATE_DIR":   "/opt/gizlet/service-data/io.gizlet.tailscale",
		"GIZLET_RUNTIME_DIR": "/run/gizlet-services/io.gizlet.tailscale",
		"STATE_DIRECTORY":    "/var/lib/other",
	}), []string{"PATH=/usr/bin", "TS_BE_CLI=1", "TS_LOGS_DIR=/elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.path != "/opt/gizlet/services/io.gizlet.tailscale/bin/tailscaled" {
		t.Errorf("path = %s", cmd.path)
	}
	want := []string{
		"tailscaled",
		"--tun=userspace-networking",
		"--statedir=/opt/gizlet/service-data/io.gizlet.tailscale",
		"--socket=/run/gizlet-services/io.gizlet.tailscale/tailscaled.sock",
	}
	if !slices.Equal(cmd.argv, want) {
		t.Errorf("argv = %q, want %q", cmd.argv, want)
	}
	wantEnv := []string{"PATH=/usr/bin", "TS_LOGS_DIR=/opt/gizlet/service-data/io.gizlet.tailscale"}
	if !slices.Equal(cmd.env, wantEnv) {
		t.Errorf("env = %q, want %q", cmd.env, wantEnv)
	}
	if !slices.Equal(cmd.dirs, []string{"/opt/gizlet/service-data/io.gizlet.tailscale", "/run/gizlet-services/io.gizlet.tailscale"}) {
		t.Errorf("dirs = %q", cmd.dirs)
	}
}

func TestDaemonFallsBackToSystemdFolders(t *testing.T) {
	cmd, err := daemon("/pkg", env(map[string]string{
		"STATE_DIRECTORY":   "/var/lib/smoke/tailscale:/var/lib/other",
		"RUNTIME_DIRECTORY": "/run/smoke/tailscale",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.argv[2] != "--statedir=/var/lib/smoke/tailscale" || cmd.socket != "/run/smoke/tailscale/tailscaled.sock" {
		t.Errorf("argv = %q, socket = %s", cmd.argv, cmd.socket)
	}
}

func TestDaemonRefusesMissingOrRelativeFolders(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"no state":         {"GIZLET_RUNTIME_DIR": "/run/x"},
		"no runtime":       {"GIZLET_STATE_DIR": "/state"},
		"relative state":   {"GIZLET_STATE_DIR": "state", "GIZLET_RUNTIME_DIR": "/run/x"},
		"relative runtime": {"GIZLET_STATE_DIR": "/state", "RUNTIME_DIRECTORY": "run/x"},
		"long socket":      {"GIZLET_STATE_DIR": "/state", "GIZLET_RUNTIME_DIR": "/run/" + strings.Repeat("x", 100)},
	} {
		if _, err := daemon("/pkg", env(values), nil); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestCLIConnectsToTheServiceSocket(t *testing.T) {
	cmd, err := cli("/pkg", []string{"up", "--qr"}, env(nil), []string{"TS_BE_CLI=1", "HOME=/root"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tailscale", "--socket=/run/gizlet-services/io.gizlet.tailscale/tailscaled.sock", "up", "--qr"}
	if cmd.path != "/pkg/bin/tailscaled" || !slices.Equal(cmd.argv, want) {
		t.Errorf("cmd = %s %q, want %q", cmd.path, cmd.argv, want)
	}
	if !slices.Equal(cmd.env, []string{"HOME=/root"}) {
		t.Errorf("env = %q", cmd.env)
	}
	cmd, err = cli("/pkg", []string{"status"}, env(map[string]string{"GIZLET_RUNTIME_DIR": "/run/test"}), nil)
	if err != nil || cmd.argv[1] != "--socket=/run/test/tailscaled.sock" {
		t.Errorf("argv = %q, err = %v", cmd.argv, err)
	}
	if _, err := cli("/pkg", nil, env(map[string]string{"GIZLET_RUNTIME_DIR": "run"}), nil); err == nil {
		t.Error("relative GIZLET_RUNTIME_DIR accepted")
	}
}

func TestWantsDaemon(t *testing.T) {
	for args, want := range map[string]bool{
		"":                   false,
		"version":            false,
		"--help":             false,
		"help":               false,
		"status":             true,
		"up --qr":            true,
		"--socket=/x up":     true,
		"--socket=/x --help": false,
	} {
		if got := wantsDaemon(strings.Fields(args)); got != want {
			t.Errorf("wantsDaemon(%q) = %v, want %v", args, got, want)
		}
	}
}
