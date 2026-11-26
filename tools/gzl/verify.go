package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
)

// Upstream is the pinned upstream build, from upstream.env.
type Upstream struct {
	TailscaleVersion string
	TailscaleCommit  string
	GoVersion        string
}

func loadUpstream(file string) (Upstream, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Upstream{}, err
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Upstream{}, fmt.Errorf("%s: bad line %q", file, line)
		}
		values[key] = value
	}
	u := Upstream{TailscaleVersion: values["TAILSCALE_VERSION"], TailscaleCommit: values["TAILSCALE_COMMIT"], GoVersion: values["GO_VERSION"]}
	if u.TailscaleVersion == "" || len(u.TailscaleCommit) != 40 || u.GoVersion == "" {
		return Upstream{}, fmt.Errorf("%s: TAILSCALE_VERSION, TAILSCALE_COMMIT and GO_VERSION are required", file)
	}
	return u, nil
}

// verify checks a built .gzl as the Store and the device read it, and checks that its
// Tailscale is the pinned upstream build with every license it needs.
func verify(specPath string, upstream Upstream, file string) error {
	spec, err := loadSpec(specPath)
	if err != nil {
		return err
	}
	zr, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()

	contents := map[string][]byte{}
	modes := map[string]fs.FileMode{}
	for _, f := range zr.File {
		if err := checkName(f.Name); err != nil {
			return err
		}
		if _, dup := contents[f.Name]; dup {
			return fmt.Errorf("%s appears twice", f.Name)
		}
		mode := f.Mode()
		if !mode.IsRegular() || mode&(fs.ModeSetuid|fs.ModeSetgid) != 0 {
			return fmt.Errorf("%s is not a plain regular file (%s)", f.Name, mode)
		}
		if f.Method != zip.Deflate && f.Method != zip.Store {
			return fmt.Errorf("%s uses compression method %d", f.Name, f.Method)
		}
		if f.Flags&0x1 != 0 || len(f.Extra) != 0 {
			return fmt.Errorf("%s is encrypted or has extra fields", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		contents[f.Name] = data
		modes[f.Name] = mode
	}

	var manifest Manifest
	raw, ok := contents["manifest.json"]
	if !ok {
		return fmt.Errorf("no manifest.json")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return fmt.Errorf("manifest.json: %w", err)
	}
	if len(spec.Packages) != 1 {
		return fmt.Errorf("expected one package in %s", specPath)
	}
	want := spec.manifest(spec.Packages[0], manifest.Icon)
	if !reflect.DeepEqual(manifest, want) {
		return fmt.Errorf("manifest.json does not match %s:\n got %+v\nwant %+v", specPath, manifest, want)
	}
	if manifest.Schema != 2 || manifest.Kind != "service" {
		return fmt.Errorf("manifest.json must be schema 2 with kind service")
	}
	if filepath.Base(file) != packageName(manifest) {
		return fmt.Errorf("%s should be named %s", filepath.Base(file), packageName(manifest))
	}
	if manifest.Icon != "" {
		if _, ok := contents[manifest.Icon]; !ok {
			return fmt.Errorf("the icon %s is missing", manifest.Icon)
		}
	}

	if err := verifyChecksums(contents); err != nil {
		return err
	}

	binaries := map[string]*debug.BuildInfo{}
	for _, name := range []string{manifest.Entry, "bin/tailscaled"} {
		data, ok := contents[name]
		if !ok {
			return fmt.Errorf("%s is missing", name)
		}
		if modes[name]&0o111 == 0 {
			return fmt.Errorf("%s is not executable", name)
		}
		if err := checkELF(data, manifest.Target); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		bi, err := buildinfo.Read(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		binaries[filepath.Base(name)] = bi
	}
	if cli, ok := contents["bin/tailscale"]; !ok || modes["bin/tailscale"]&0o111 == 0 || !bytes.HasPrefix(cli, []byte("#!/bin/sh\n")) {
		return fmt.Errorf("bin/tailscale is missing, not executable or not a shell script")
	}
	if err := verifyTailscaled(binaries["tailscaled"], contents["bin/tailscaled"], upstream); err != nil {
		return fmt.Errorf("bin/tailscaled: %w", err)
	}
	for name, bi := range binaries {
		if bi.GoVersion != "go"+upstream.GoVersion {
			return fmt.Errorf("%s was built with %s, not go%s", name, bi.GoVersion, upstream.GoVersion)
		}
		if setting(bi, "CGO_ENABLED") != "0" || setting(bi, "-trimpath") != "true" {
			return fmt.Errorf("%s was not built with CGO_ENABLED=0 and -trimpath", name)
		}
		if fips := setting(bi, "GOFIPS140"); (fips != "" && fips != "off") || setting(bi, "GOEXPERIMENT") != "" {
			return fmt.Errorf("%s was built with GOFIPS140=%q GOEXPERIMENT=%q; build with neither", name, fips, setting(bi, "GOEXPERIMENT"))
		}
	}

	list, err := components(binaries)
	if err != nil {
		return err
	}
	for _, c := range list {
		found := false
		for name := range contents {
			if strings.HasPrefix(name, c.Dir+"/") && isLicenseFile(filepath.Base(name)) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no license file for %s@%s in %s", c.Path, c.Version, c.Dir)
		}
	}
	for _, name := range []string{"licenses/README.md", "licenses/go/LICENSE", "licenses/go/PATENTS", "licenses/github.com/gizlet/tailscale/LICENSE", "licenses/github.com/gizlet/tailscale/NOTICE.md"} {
		if _, ok := contents[name]; !ok {
			return fmt.Errorf("%s is missing", name)
		}
	}
	return nil
}

func verifyChecksums(contents map[string][]byte) error {
	sums, ok := contents["CHECKSUMS"]
	if !ok {
		return fmt.Errorf("no CHECKSUMS")
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok {
			return fmt.Errorf("CHECKSUMS: bad line %q", line)
		}
		data, exists := contents[name]
		if !exists || name == "CHECKSUMS" {
			return fmt.Errorf("CHECKSUMS lists %s, which is not in the package", name)
		}
		if listed[name] {
			return fmt.Errorf("CHECKSUMS lists %s twice", name)
		}
		listed[name] = true
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != sum {
			return fmt.Errorf("CHECKSUMS: %s does not match", name)
		}
	}
	for name := range contents {
		if name != "CHECKSUMS" && !listed[name] {
			return fmt.Errorf("CHECKSUMS does not list %s", name)
		}
	}
	return nil
}

// verifyTailscaled checks that tailscaled is the pinned Tailscale release with the CLI linked in.
func verifyTailscaled(bi *debug.BuildInfo, data []byte, upstream Upstream) error {
	switch {
	case bi.Main.Path != "tailscale.com":
		return fmt.Errorf("main module is %s, not tailscale.com", bi.Main.Path)
	case bi.Path != "tailscale.com/cmd/tailscaled":
		return fmt.Errorf("built from %s, not tailscale.com/cmd/tailscaled", bi.Path)
	case bi.Main.Version != "v"+upstream.TailscaleVersion:
		return fmt.Errorf("module version is %s, not v%s", bi.Main.Version, upstream.TailscaleVersion)
	case setting(bi, "vcs.revision") != upstream.TailscaleCommit:
		return fmt.Errorf("built from commit %q, not %s", setting(bi, "vcs.revision"), upstream.TailscaleCommit)
	case setting(bi, "vcs.modified") != "false":
		return fmt.Errorf("built from a modified tree")
	case setting(bi, "GOOS") != "linux" || setting(bi, "GOARCH") != "arm64":
		return fmt.Errorf("built for %s/%s, not linux/arm64", setting(bi, "GOOS"), setting(bi, "GOARCH"))
	case setting(bi, "-tags") != "ts_include_cli,ts_omit_systray":
		return fmt.Errorf("built with tags %q, not ts_include_cli,ts_omit_systray", setting(bi, "-tags"))
	}
	stamp := upstream.TailscaleVersion + "-t" + upstream.TailscaleCommit[:9]
	if !bytes.Contains(data, []byte(stamp)) {
		return fmt.Errorf("does not carry the version stamp %s", stamp)
	}
	return nil
}

func setting(bi *debug.BuildInfo, key string) string {
	for _, s := range bi.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
