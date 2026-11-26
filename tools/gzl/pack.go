package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Every entry gets this DOS timestamp (1980-01-01 00:00) so that packages are reproducible.
const (
	dosDate = 1<<5 | 1
	dosTime = 0
)

var elfMachines = map[string]elf.Machine{
	"aarch64": elf.EM_AARCH64,
	"x86_64":  elf.EM_X86_64,
	"riscv64": elf.EM_RISCV,
}

// pack writes manifest.json, the icon and CHECKSUMS into stage and zips it into outDir.
func pack(specPath, stage, outDir string) (string, error) {
	spec, err := loadSpec(specPath)
	if err != nil {
		return "", err
	}
	if len(spec.Packages) != 1 {
		return "", fmt.Errorf("%s declares %d packages; this build makes one", specPath, len(spec.Packages))
	}
	pkg := spec.Packages[0]
	icon := ""
	if spec.Icon != "" {
		icon = "icon" + strings.ToLower(path.Ext(spec.Icon))
		data, err := os.ReadFile(filepath.Join(filepath.Dir(specPath), filepath.FromSlash(spec.Icon)))
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(stage, icon), data, 0o644); err != nil {
			return "", err
		}
	}
	manifest := spec.manifest(pkg, icon)
	text, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), append(text, '\n'), 0o644); err != nil {
		return "", err
	}
	entry, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(pkg.Entry)))
	if err != nil {
		return "", fmt.Errorf("entry: %w", err)
	}
	if info, err := os.Stat(filepath.Join(stage, filepath.FromSlash(pkg.Entry))); err != nil || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("entry %s is not executable", pkg.Entry)
	}
	if err := checkELF(entry, pkg.Target); err != nil {
		return "", fmt.Errorf("entry %s: %w", pkg.Entry, err)
	}

	files, err := stageFiles(stage)
	if err != nil {
		return "", err
	}
	var sums bytes.Buffer
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(f.name)))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), f.name)
	}
	if err := os.WriteFile(filepath.Join(stage, "CHECKSUMS"), sums.Bytes(), 0o644); err != nil {
		return "", err
	}
	files = append(files, stageFile{name: "CHECKSUMS", mode: 0o644})

	out := filepath.Join(outDir, packageName(manifest))
	if err := writeZip(out, stage, files); err != nil {
		return "", err
	}
	return out, nil
}

type stageFile struct {
	name string
	mode fs.FileMode
}

// stageFiles lists the regular files under stage in byte order, with normalized modes.
func stageFiles(stage string) ([]stageFile, error) {
	var files []stageFile
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", name)
		}
		if err := checkName(name); err != nil {
			return err
		}
		if name == "CHECKSUMS" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		files = append(files, stageFile{name: name, mode: mode})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, err
}

// writeZip writes a deflated zip with no data descriptors, ZIP64 or extra fields.
func writeZip(out, stage string, files []stageFile) error {
	tmp := out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	zw := zip.NewWriter(f)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(stage, filepath.FromSlash(file.name)))
		if err != nil {
			return err
		}
		var compressed bytes.Buffer
		fw, err := flate.NewWriter(&compressed, flate.BestCompression)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
		if err := fw.Close(); err != nil {
			return err
		}
		header := &zip.FileHeader{
			Name:               file.name,
			Method:             zip.Deflate,
			ModifiedDate:       dosDate,
			ModifiedTime:       dosTime,
			CRC32:              crc32.ChecksumIEEE(data),
			CompressedSize64:   uint64(compressed.Len()),
			UncompressedSize64: uint64(len(data)),
		}
		header.SetMode(file.mode)
		w, err := zw.CreateRaw(header)
		if err != nil {
			return err
		}
		if _, err := w.Write(compressed.Bytes()); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

// checkELF checks that data is a static Linux executable for target's architecture.
func checkELF(data []byte, target string) error {
	arch, _, _ := strings.Cut(target, "-")
	machine, ok := elfMachines[arch]
	if !ok || !strings.Contains(target, "-linux-") {
		return fmt.Errorf("target %s is not a supported Linux target", target)
	}
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("not an ELF file: %w", err)
	}
	defer f.Close()
	if f.Machine != machine || f.Class != elf.ELFCLASS64 {
		return fmt.Errorf("is %s, not %s", f.Machine, machine)
	}
	if f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN {
		return fmt.Errorf("is not an executable (%s)", f.Type)
	}
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			return fmt.Errorf("is dynamically linked; build it static")
		}
	}
	return nil
}
