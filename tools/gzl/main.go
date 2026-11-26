// Command gzl builds and checks this repository's .gzl package.
//
//	gzl field gizlet.toml KEY
//	gzl licenses -out DIR -goroot DIR -modcache DIR [-module PATH=DIR]... BINARY...
//	gzl pack -toml gizlet.toml -stage DIR -out DIR
//	gzl verify -toml gizlet.toml -upstream upstream.env FILE.gzl
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

type moduleDirs map[string]string

func (m moduleDirs) String() string { return fmt.Sprint(map[string]string(m)) }

func (m moduleDirs) Set(value string) error {
	path, dir, ok := strings.Cut(value, "=")
	if !ok || path == "" || dir == "" {
		return fmt.Errorf("want PATH=DIR, got %q", value)
	}
	m[path] = dir
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "field":
		if len(os.Args) != 4 {
			usage()
		}
		err = field(os.Args[2], os.Args[3])
	case "licenses":
		fs := flag.NewFlagSet("licenses", flag.ExitOnError)
		out := fs.String("out", "", "the package's licenses folder")
		goroot := fs.String("goroot", "", "GOROOT of the toolchain that built the binaries")
		modcache := fs.String("modcache", "", "GOMODCACHE holding the binaries' modules")
		modules := moduleDirs{}
		fs.Var(modules, "module", "PATH=DIR: the source folder of a main module")
		fs.Parse(os.Args[2:])
		if *out == "" || *goroot == "" || *modcache == "" || fs.NArg() == 0 || filepath.Base(*out) != "licenses" {
			usage()
		}
		binaries := map[string]*debug.BuildInfo{}
		for _, file := range fs.Args() {
			bi, e := readBuildInfo(file)
			if e != nil {
				fail(e)
			}
			binaries[filepath.Base(file)] = bi
		}
		var list []*Component
		list, err = collectLicenses(filepath.Dir(*out), binaries, licenseSources{goroot: *goroot, modcache: *modcache, modules: modules})
		if err == nil {
			fmt.Printf("licenses: %d components\n", len(list))
		}
	case "pack":
		fs := flag.NewFlagSet("pack", flag.ExitOnError)
		spec := fs.String("toml", "gizlet.toml", "gizlet.toml")
		stage := fs.String("stage", "", "the package's files")
		out := fs.String("out", "dist", "output folder")
		fs.Parse(os.Args[2:])
		if *stage == "" || fs.NArg() != 0 {
			usage()
		}
		var file string
		file, err = pack(*spec, *stage, *out)
		if err == nil {
			fmt.Println(file)
		}
	case "verify":
		fs := flag.NewFlagSet("verify", flag.ExitOnError)
		spec := fs.String("toml", "gizlet.toml", "gizlet.toml")
		upstreamFile := fs.String("upstream", "upstream.env", "upstream.env")
		fs.Parse(os.Args[2:])
		if fs.NArg() == 0 {
			usage()
		}
		var upstream Upstream
		upstream, err = loadUpstream(*upstreamFile)
		for _, file := range fs.Args() {
			if err != nil {
				break
			}
			if err = verify(*spec, upstream, file); err == nil {
				fmt.Printf("%s: ok\n", file)
			}
		}
	default:
		usage()
	}
	if err != nil {
		fail(err)
	}
}

func field(specPath, key string) error {
	spec, err := loadSpec(specPath)
	if err != nil {
		return err
	}
	values := map[string]string{"id": spec.ID, "name": spec.Name, "version": spec.Version, "kind": spec.Kind}
	value, ok := values[key]
	if !ok {
		return fmt.Errorf("unknown field %q", key)
	}
	fmt.Println(value)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  gzl field gizlet.toml KEY
  gzl licenses -out DIR/licenses -goroot DIR -modcache DIR [-module PATH=DIR]... BINARY...
  gzl pack -toml gizlet.toml -stage DIR -out DIR
  gzl verify -toml gizlet.toml -upstream upstream.env FILE.gzl...`)
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gzl:", err)
	os.Exit(1)
}
