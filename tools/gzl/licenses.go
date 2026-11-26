package main

import (
	"bytes"
	"debug/buildinfo"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"unicode"
)

// licenseFile matches the files that carry a module's license terms and notices;
// sourceFile excludes code that happens to share their names, such as license_test.go.
var (
	licenseFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|patents|unlicense)([.\-_][A-Za-z0-9.\-_]*)?$`)
	sourceFile  = regexp.MustCompile(`(?i)\.(go|s|c|h|py|sh|js|ts|rs)$`)
)

func isLicenseFile(name string) bool {
	return licenseFile.MatchString(name) && !sourceFile.MatchString(name)
}

// Component is one body of code linked into the package's binaries.
type Component struct {
	Path    string
	Version string
	// Dir is the package folder holding its license files.
	Dir     string
	Files   []string
	License string
	UsedBy  []string
}

// licenseDir is where a component's license files go in the package.
func licenseDir(modulePath, version string) string {
	if modulePath == "go" {
		return "licenses/go"
	}
	clean := portablePath(modulePath)
	if version == "" || version == "(devel)" {
		return "licenses/" + clean
	}
	return "licenses/modules/" + clean + "/" + version
}

// portablePath replaces anything but letters, digits, "/" and "._+-" with "_".
func portablePath(p string) string {
	return strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (r == '/' || r == '.' || r == '-' || r == '_' || r == '+' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return '_'
	}, p)
}

// moduleLicenseFiles lists the license files of the module in dir, at any depth, as
// slash-separated paths relative to dir: code bundled inside a module can carry its
// own terms. Folders the go command never builds from are skipped: testdata, vendor
// and those starting with "." or "_". The Go toolchain's own entry is its root
// LICENSE and PATENTS, which cover the whole standard library.
func moduleLicenseFiles(dir string, rootOnly bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == dir {
				return nil
			}
			name := d.Name()
			if rootOnly || name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && isLicenseFile(d.Name()) {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// components lists what the binaries link, from their Go build information.
func components(binaries map[string]*debug.BuildInfo) ([]*Component, error) {
	byKey := map[string]*Component{}
	add := func(modulePath, version, binary string) {
		key := modulePath + "@" + version
		c := byKey[key]
		if c == nil {
			c = &Component{Path: modulePath, Version: version, Dir: licenseDir(modulePath, version)}
			byKey[key] = c
		}
		if len(c.UsedBy) == 0 || c.UsedBy[len(c.UsedBy)-1] != binary {
			c.UsedBy = append(c.UsedBy, binary)
		}
	}
	names := make([]string, 0, len(binaries))
	for name := range binaries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bi := binaries[name]
		add("go", bi.GoVersion, name)
		add(bi.Main.Path, mainVersion(bi), name)
		for _, dep := range bi.Deps {
			mod := dep
			if dep.Replace != nil {
				mod = dep.Replace
			}
			if mod.Version == "" {
				return nil, fmt.Errorf("%s: %s is replaced by a local folder", name, dep.Path)
			}
			add(mod.Path, mod.Version, name)
		}
	}
	list := make([]*Component, 0, len(byKey))
	for _, c := range byKey {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if (list[i].Path == "go") != (list[j].Path == "go") {
			return list[i].Path == "go"
		}
		return list[i].Path+"@"+list[i].Version < list[j].Path+"@"+list[j].Version
	})
	return list, nil
}

// mainVersion is the main module's version, "(devel)" when it was not stamped.
func mainVersion(bi *debug.BuildInfo) string {
	if bi.Main.Version == "" {
		return "(devel)"
	}
	return bi.Main.Version
}

// escapeModulePath is the module cache's case-encoding of a module path or version.
func escapeModulePath(s string) string {
	var b strings.Builder
	for _, r := range s {
		if 'A' <= r && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type licenseSources struct {
	goroot   string
	modcache string
	// Folders of the main modules, which are not in the module cache.
	modules map[string]string
}

func (s licenseSources) dir(c *Component) (string, error) {
	if c.Path == "go" {
		version, err := os.ReadFile(filepath.Join(s.goroot, "VERSION"))
		if err != nil {
			return "", err
		}
		if first, _, _ := strings.Cut(string(version), "\n"); first != c.Version {
			return "", fmt.Errorf("binaries were built with %s, but %s is %s", c.Version, s.goroot, first)
		}
		return s.goroot, nil
	}
	if dir, ok := s.modules[c.Path]; ok {
		return dir, nil
	}
	if c.Version == "(devel)" {
		return "", fmt.Errorf("no folder given for the main module %s", c.Path)
	}
	return filepath.Join(s.modcache, escapeModulePath(c.Path)+"@"+escapeModulePath(c.Version)), nil
}

// collectLicenses copies every component's license files under out (the package's licenses
// folder) and writes an index. It fails if any component has none.
func collectLicenses(stage string, binaries map[string]*debug.BuildInfo, sources licenseSources) ([]*Component, error) {
	list, err := components(binaries)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, c := range list {
		dir, err := sources.dir(c)
		if err != nil {
			return nil, err
		}
		files, err := moduleLicenseFiles(dir, c.Path == "go")
		if err != nil {
			return nil, fmt.Errorf("%s@%s: %w", c.Path, c.Version, err)
		}
		var texts [][]byte
		for _, rel := range files {
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
			if err != nil {
				return nil, err
			}
			name := path.Join(c.Dir, portablePath(rel))
			dest := filepath.Join(stage, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(dest, data, 0o644); err != nil {
				return nil, err
			}
			c.Files = append(c.Files, name)
			texts = append(texts, data)
		}
		if len(c.Files) == 0 {
			missing = append(missing, c.Path+"@"+c.Version)
			continue
		}
		c.License = identify(texts)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no license file found for %s", strings.Join(missing, ", "))
	}
	var unknown []string
	for _, c := range list {
		if c.License == "" {
			unknown = append(unknown, c.Path+"@"+c.Version)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unrecognized license terms for %s; review them before shipping", strings.Join(unknown, ", "))
	}
	index := licenseIndex(list)
	if err := os.WriteFile(filepath.Join(stage, "licenses", "README.md"), index, 0o644); err != nil {
		return nil, err
	}
	return list, nil
}

// identify names the license of a module from its license texts. Only permissive terms
// are recognized; anything else must be reviewed by hand and added here.
func identify(texts [][]byte) string {
	all := bytes.ToLower(bytes.Join(texts, []byte("\n")))
	has := func(s string) bool { return bytes.Contains(all, []byte(strings.ToLower(s))) }
	norm := regexp.MustCompile(`\s+`).ReplaceAll(all, []byte(" "))
	hasNorm := func(s string) bool { return bytes.Contains(norm, []byte(strings.ToLower(s))) }
	var found []string
	switch {
	case has("GNU General Public License") || has("GNU Lesser General Public") || has("GNU Affero"):
		return ""
	}
	if hasNorm("Apache License") && hasNorm("Version 2.0") {
		found = append(found, "Apache-2.0")
	}
	if hasNorm("Mozilla Public License Version 2.0") || hasNorm("Mozilla Public License, version 2.0") {
		found = append(found, "MPL-2.0")
	}
	if hasNorm("Permission is hereby granted, free of charge") {
		found = append(found, "MIT")
	}
	if hasNorm("Redistribution and use in source and binary forms") {
		if hasNorm("Neither the name") || hasNorm("names of its contributors may") || hasNorm("name of the copyright holder nor") {
			found = append(found, "BSD-3-Clause")
		} else {
			found = append(found, "BSD-2-Clause")
		}
	}
	if hasNorm("Permission to use, copy, modify, and/or distribute this software for any purpose") ||
		hasNorm("Permission to use, copy, modify, and distribute this software for any purpose") {
		found = append(found, "ISC")
	}
	if hasNorm("This is free and unencumbered software released into the public domain") {
		found = append(found, "Unlicense")
	}
	if hasNorm("CC0 1.0 Universal") {
		found = append(found, "CC0-1.0")
	}
	if len(found) == 0 {
		return ""
	}
	return strings.Join(found, ", ")
}

func licenseIndex(list []*Component) []byte {
	var b bytes.Buffer
	b.WriteString("# Licenses\n\n")
	b.WriteString("Every component linked into this package's programs, as recorded in their Go build information, and its license files. The terms column names the license texts found in those files; the files themselves are authoritative. See github.com/gizlet/tailscale/NOTICE.md for the sources.\n\n")
	b.WriteString("| Component | Version | License terms | Used by | Files |\n| --- | --- | --- | --- | --- |\n")
	for _, c := range list {
		name := c.Path
		if name == "go" {
			name = "Go standard library and runtime"
		}
		files := make([]string, len(c.Files))
		for i, f := range c.Files {
			files[i] = strings.TrimPrefix(f, "licenses/")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", name, c.Version, c.License, strings.Join(c.UsedBy, ", "), strings.Join(files, ", "))
	}
	return b.Bytes()
}

func readBuildInfo(file string) (*debug.BuildInfo, error) {
	bi, err := buildinfo.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return bi, nil
}
