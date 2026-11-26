// Command tailscale-service is the entry of the Tailscale service on Gizlet OS.
//
// Run with no arguments, as the service's unit does, it replaces itself with
// the package's tailscaled in userspace-networking mode, keeping its state in
// GIZLET_STATE_DIR and its LocalAPI socket in GIZLET_RUNTIME_DIR. Because it
// execs, systemd signals tailscaled directly and a stop shuts it down cleanly.
//
// Run as "tailscale-service cli ARGS", as bin/tailscale does, it runs the
// Tailscale CLI against that socket.
package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// Set by scripts/build.sh.
var (
	packageVersion   = "dev"
	tailscaleVersion = "unknown"
)

const (
	serviceID = "io.gizlet.tailscale"
	// Gizlet OS creates each service's runtime folder here, as systemd's RuntimeDirectory=.
	runtimeRoot = "/run/gizlet-services"
	socketName  = "tailscaled.sock"
	// sockaddr_un holds 108 bytes on Linux, including the terminating NUL.
	maxSocketPath = 107
)

type lookupFunc func(string) string

// command is a program to exec: its path, argv and environment.
type command struct {
	path string
	argv []string
	env  []string
	// Folders to create before the exec, and the LocalAPI socket.
	dirs   []string
	socket string
}

func main() {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fail(fmt.Errorf("cannot find this program: %w", err))
	}
	pkg := filepath.Dir(filepath.Dir(exe))
	args := os.Args[1:]

	var cmd command
	switch {
	case len(args) == 0:
		cmd, err = daemon(pkg, os.Getenv, os.Environ())
	case args[0] == "cli":
		cmd, err = cli(pkg, args[1:], os.Getenv, os.Environ())
		if err == nil {
			if _, statErr := os.Stat(cmd.socket); statErr != nil && wantsDaemon(args[1:]) {
				fmt.Fprintf(os.Stderr, "Tailscale is not running. Start it with: gizlet service start %s\n", serviceID)
			}
		}
	case len(args) == 1 && (args[0] == "--version" || args[0] == "version"):
		fmt.Printf("%s %s (Tailscale %s)\n", serviceID, packageVersion, tailscaleVersion)
		return
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [cli ARGS... | --version]\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	if err != nil {
		fail(err)
	}
	for _, dir := range cmd.dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fail(err)
		}
	}
	fail(syscall.Exec(cmd.path, cmd.argv, cmd.env))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "tailscale-service: %v\n", err)
	os.Exit(1)
}

// daemon is tailscaled as the service runs it.
func daemon(pkg string, getenv lookupFunc, environ []string) (command, error) {
	state, err := serviceDir(getenv, "GIZLET_STATE_DIR", "STATE_DIRECTORY")
	if err != nil {
		return command{}, err
	}
	runtime, err := serviceDir(getenv, "GIZLET_RUNTIME_DIR", "RUNTIME_DIRECTORY")
	if err != nil {
		return command{}, err
	}
	socket, err := socketPath(runtime)
	if err != nil {
		return command{}, err
	}
	return command{
		path: filepath.Join(pkg, "bin", "tailscaled"),
		argv: []string{
			"tailscaled",
			"--tun=userspace-networking",
			"--statedir=" + state,
			"--socket=" + socket,
		},
		// Logs that tailscaled buffers before upload stay with its state.
		env:    setenv(environ, map[string]string{"TS_LOGS_DIR": state, "TS_BE_CLI": ""}),
		dirs:   []string{state, runtime},
		socket: socket,
	}, nil
}

// cli is the Tailscale CLI connected to this service's daemon.
func cli(pkg string, args []string, getenv lookupFunc, environ []string) (command, error) {
	runtime := getenv("GIZLET_RUNTIME_DIR")
	if runtime == "" {
		runtime = filepath.Join(runtimeRoot, serviceID)
	} else if !filepath.IsAbs(runtime) {
		return command{}, fmt.Errorf("GIZLET_RUNTIME_DIR=%q is not an absolute path", runtime)
	}
	socket, err := socketPath(runtime)
	if err != nil {
		return command{}, err
	}
	return command{
		path: filepath.Join(pkg, "bin", "tailscaled"),
		// tailscaled runs as the CLI when its argv[0] is "tailscale".
		argv:   append([]string{"tailscale", "--socket=" + socket}, args...),
		env:    setenv(environ, map[string]string{"TS_BE_CLI": ""}),
		socket: socket,
	}, nil
}

// serviceDir is the absolute folder named by the first variable set, which
// Gizlet OS (or systemd, outside Gizlet OS) provides to a service.
func serviceDir(getenv lookupFunc, names ...string) (string, error) {
	for _, name := range names {
		value := getenv(name)
		if value == "" {
			continue
		}
		// systemd lists several folders separated by colons; the first is the unit's own.
		if name != names[0] {
			value, _, _ = strings.Cut(value, ":")
		}
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("%s=%q is not an absolute path", name, value)
		}
		return filepath.Clean(value), nil
	}
	return "", fmt.Errorf("%s is not set: run Tailscale as a Gizlet service (gizlet service start %s)", names[0], serviceID)
}

func socketPath(runtime string) (string, error) {
	socket := filepath.Join(runtime, socketName)
	if len(socket) > maxSocketPath {
		return "", errors.New("the socket path " + socket + " is too long for a Unix socket")
	}
	return socket, nil
}

// setenv returns environ with each key set to its value, or removed when the value is empty.
func setenv(environ []string, values map[string]string) []string {
	out := make([]string, 0, len(environ)+len(values))
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := values[key]; !ok {
			out = append(out, kv)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if values[key] != "" {
			out = append(out, key+"="+values[key])
		}
	}
	return out
}

// wantsDaemon reports whether the CLI command talks to tailscaled.
func wantsDaemon(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			if arg == "-h" || arg == "--help" || arg == "-help" {
				return false
			}
			continue
		}
		return arg != "version" && arg != "help"
	}
	return false
}
