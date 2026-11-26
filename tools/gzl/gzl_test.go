package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// tinyELF is a minimal 64-bit little-endian ELF header for machine, with one
// program header of type progType when progType is nonzero.
func tinyELF(machine uint16, progType uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	phnum := uint16(0)
	if progType != 0 {
		phnum = 1
	}
	binary.Write(&b, binary.LittleEndian, struct {
		Type, Machine                       uint16
		Version                             uint32
		Entry, Phoff, Shoff                 uint64
		Flags                               uint32
		Ehsize, Phentsize, Phnum, Shentsize uint16
		Shnum, Shstrndx                     uint16
	}{2, machine, 1, 0x400000, 64, 0, 0, 64, 56, phnum, 64, 0, 0})
	if progType != 0 {
		binary.Write(&b, binary.LittleEndian, struct {
			Type, Flags                             uint32
			Off, Vaddr, Paddr, Filesz, Memsz, Align uint64
		}{Type: progType})
	}
	return b.Bytes()
}

func writeFile(t *testing.T, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, mode); err != nil {
		t.Fatal(err)
	}
}

const testSpec = `id = "io.gizlet.example"
name = "Example"
version = "1.2.3"
kind = "service"
icon = "store/icon.png"

[[packages]]
target = "aarch64-unknown-linux-gnu"
tier = "lite"
entry = "bin/example"
`

func stageExample(t *testing.T, root string) (spec, stage string) {
	spec = filepath.Join(root, "gizlet.toml")
	writeFile(t, spec, []byte(testSpec), 0o644)
	writeFile(t, filepath.Join(root, "store/icon.png"), []byte("png"), 0o644)
	stage = filepath.Join(root, "stage")
	writeFile(t, filepath.Join(stage, "bin/example"), tinyELF(183, 0), 0o700)
	writeFile(t, filepath.Join(stage, "licenses/LICENSE"), []byte("text"), 0o600)
	return spec, stage
}

func TestPackIsReproducibleAndReadable(t *testing.T) {
	var packages [][]byte
	for range 2 {
		root := t.TempDir()
		spec, stage := stageExample(t, root)
		out, err := pack(spec, stage, root)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(out) != "io.gizlet.example-1.2.3-aarch64-unknown-linux-gnu-lite.gzl" {
			t.Errorf("package name %s", filepath.Base(out))
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		packages = append(packages, data)
	}
	if !bytes.Equal(packages[0], packages[1]) {
		t.Fatal("two packs of the same files differ")
	}
	zr, err := zip.NewReader(bytes.NewReader(packages[0]), int64(len(packages[0])))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	contents := map[string][]byte{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Flags&0x8 != 0 || len(f.Extra) != 0 {
			t.Errorf("%s has a data descriptor or extra fields", f.Name)
		}
		wantMode := os.FileMode(0o644)
		if f.Name == "bin/example" {
			wantMode = 0o755
		}
		if f.Mode() != wantMode {
			t.Errorf("%s mode %s, want %s", f.Name, f.Mode(), wantMode)
		}
		rc, _ := f.Open()
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		contents[f.Name] = buf.Bytes()
	}
	if got := strings.Join(names, " "); got != "bin/example icon.png licenses/LICENSE manifest.json CHECKSUMS" {
		t.Errorf("entries = %s", got)
	}
	wantManifest := `{
  "schema": 2,
  "kind": "service",
  "id": "io.gizlet.example",
  "name": "Example",
  "version": "1.2.3",
  "runtime": 1,
  "target": "aarch64-unknown-linux-gnu",
  "tier": "lite",
  "entry": "bin/example",
  "icon": "icon.png"
}
`
	if string(contents["manifest.json"]) != wantManifest {
		t.Errorf("manifest.json =\n%s", contents["manifest.json"])
	}
	if err := verifyChecksums(contents); err != nil {
		t.Error(err)
	}
	contents["licenses/LICENSE"] = []byte("changed")
	if err := verifyChecksums(contents); err == nil {
		t.Error("a changed file passed CHECKSUMS")
	}
	delete(contents, "licenses/LICENSE")
	if err := verifyChecksums(contents); err == nil {
		t.Error("a missing file passed CHECKSUMS")
	}
}

func TestPackRefusesBadEntries(t *testing.T) {
	for name, entry := range map[string][]byte{
		"x86-64":  tinyELF(62, 0),
		"dynamic": tinyELF(183, 3),
		"script":  []byte("#!/bin/sh\n"),
	} {
		root := t.TempDir()
		spec, stage := stageExample(t, root)
		writeFile(t, filepath.Join(stage, "bin/example"), entry, 0o755)
		if _, err := pack(spec, stage, root); err == nil {
			t.Errorf("%s entry accepted", name)
		}
	}
}

func TestLoadSpec(t *testing.T) {
	spec, err := loadSpec("../../gizlet.toml")
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != "io.gizlet.tailscale" || spec.Kind != "service" || len(spec.Packages) != 1 || spec.Packages[0].Entry != "bin/tailscale-service" {
		t.Errorf("gizlet.toml: %+v", spec)
	}
	for name, text := range map[string]string{
		"unknown key":      testSpec + "colour = \"#ffffff\"\n",
		"service category": strings.Replace(testSpec, `kind = "service"`, "kind = \"service\"\ncategory = \"app\"", 1),
		"bad kind":         strings.Replace(testSpec, `kind = "service"`, `kind = "daemon"`, 1),
		"bad id":           strings.Replace(testSpec, "io.gizlet.example", "Example", 1),
		"bad entry":        strings.Replace(testSpec, "bin/example", "../example", 1),
	} {
		file := filepath.Join(t.TempDir(), "gizlet.toml")
		writeFile(t, file, []byte(text), 0o644)
		if _, err := loadSpec(file); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestIdentify(t *testing.T) {
	for text, want := range map[string]string{
		"Permission is hereby granted, free of charge, to any person":                        "MIT",
		"Redistribution and use in source and binary forms ... Neither the name of Google":   "BSD-3-Clause",
		"Redistribution and use in source and binary forms, with or without\nmodification":   "BSD-2-Clause",
		"Apache License\n  Version 2.0, January 2004":                                        "Apache-2.0",
		"Permission to use, copy, modify, and/or distribute this software for any purpose":   "ISC",
		"GNU GENERAL PUBLIC LICENSE Version 2. Permission is hereby granted, free of charge": "",
		"All rights reserved.": "",
	} {
		if got := identify([][]byte{[]byte(text)}); got != want {
			t.Errorf("identify(%.40q) = %q, want %q", text, got, want)
		}
	}
}

func TestLicenseNames(t *testing.T) {
	for name, want := range map[string]bool{
		"LICENSE": true, "LICENSE.txt": true, "COPYING": true, "NOTICE": true, "PATENTS": true,
		"LICENSE.libyaml": true, "license_test.go": false, "licenses.go": false, "README.md": false,
	} {
		if got := isLicenseFile(name); got != want {
			t.Errorf("isLicenseFile(%q) = %v", name, got)
		}
	}
	if got := escapeModulePath("github.com/Kodeworks/golang-image-ico"); got != "github.com/!kodeworks/golang-image-ico" {
		t.Errorf("escapeModulePath = %s", got)
	}
	if got := licenseDir("gopkg.in/yaml.v3", "v3.0.1"); got != "licenses/modules/gopkg.in/yaml.v3/v3.0.1" {
		t.Errorf("licenseDir = %s", got)
	}
}

const (
	mitText = "Permission is hereby granted, free of charge, to any person obtaining a copy\n"
	bsdText = "Redistribution and use in source and binary forms, with or without modification.\nNeither the name of the copyright holder nor the names\n"
)

func TestCollectLicensesIncludesBundledCode(t *testing.T) {
	root := t.TempDir()
	goroot := filepath.Join(root, "go")
	writeFile(t, filepath.Join(goroot, "VERSION"), []byte("go1.27.4\ntime 2026-11-01\n"), 0o644)
	writeFile(t, filepath.Join(goroot, "LICENSE"), []byte(bsdText), 0o644)
	writeFile(t, filepath.Join(goroot, "src/vendor/golang.org/x/net/LICENSE"), []byte("not collected"), 0o644)
	main := filepath.Join(root, "main")
	writeFile(t, filepath.Join(main, "LICENSE"), []byte(bsdText), 0o644)
	writeFile(t, filepath.Join(main, ".cache/dep/LICENSE"), []byte("not collected"), 0o644)
	modcache := filepath.Join(root, "mod")
	dep := filepath.Join(modcache, "example.com/!upper/dep@v1.2.3")
	writeFile(t, filepath.Join(dep, "LICENSE"), []byte(bsdText), 0o644)
	writeFile(t, filepath.Join(dep, "zstd/internal/xxhash/LICENSE.txt"), []byte(mitText), 0o644)
	writeFile(t, filepath.Join(dep, "zstd/internal/xxhash/xxhash.go"), []byte("package xxhash"), 0o644)
	writeFile(t, filepath.Join(dep, "testdata/LICENSE"), []byte("not collected"), 0o644)
	writeFile(t, filepath.Join(dep, "_examples/LICENSE"), []byte("not collected"), 0o644)
	writeFile(t, filepath.Join(dep, "vendor/x/LICENSE"), []byte("not collected"), 0o644)

	bi := &debug.BuildInfo{
		GoVersion: "go1.27.4",
		Main:      debug.Module{Path: "example.com/main", Version: "(devel)"},
		Deps:      []*debug.Module{{Path: "example.com/Upper/dep", Version: "v1.2.3"}},
	}
	stage := filepath.Join(root, "stage")
	list, err := collectLicenses(stage, map[string]*debug.BuildInfo{"prog": bi}, licenseSources{
		goroot: goroot, modcache: modcache, modules: map[string]string{"example.com/main": main},
	})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, c := range list {
		files = append(files, c.Files...)
	}
	want := []string{
		"licenses/go/LICENSE",
		"licenses/modules/example.com/Upper/dep/v1.2.3/LICENSE",
		"licenses/modules/example.com/Upper/dep/v1.2.3/zstd/internal/xxhash/LICENSE.txt",
		"licenses/example.com/main/LICENSE",
	}
	if strings.Join(files, "\n") != strings.Join(want, "\n") {
		t.Errorf("collected\n%s\nwant\n%s", strings.Join(files, "\n"), strings.Join(want, "\n"))
	}
	if got := list[1].License; got != "MIT, BSD-3-Clause" {
		t.Errorf("dep terms = %q", got)
	}
	nested, err := os.ReadFile(filepath.Join(stage, "licenses/modules/example.com/Upper/dep/v1.2.3/zstd/internal/xxhash/LICENSE.txt"))
	if err != nil || string(nested) != mitText {
		t.Errorf("nested license = %q, %v", nested, err)
	}
	if _, err := os.Stat(filepath.Join(stage, "licenses/README.md")); err != nil {
		t.Error(err)
	}

	writeFile(t, filepath.Join(dep, "zstd/internal/xxhash/LICENSE.txt"), []byte("GNU General Public License"), 0o644)
	if _, err := collectLicenses(filepath.Join(root, "stage2"), map[string]*debug.BuildInfo{"prog": bi}, licenseSources{
		goroot: goroot, modcache: modcache, modules: map[string]string{"example.com/main": main},
	}); err == nil {
		t.Error("bundled code under unreviewed terms was accepted")
	}
}
