package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Spec is gizlet.toml, as specified in gizlet/gizlet's docs/gizlet-toml.md.
type Spec struct {
	ID           string            `toml:"id"`
	Name         string            `toml:"name"`
	Version      string            `toml:"version"`
	Kind         string            `toml:"kind"`
	Category     string            `toml:"category"`
	Description  string            `toml:"description"`
	Tags         []string          `toml:"tags"`
	Color        string            `toml:"color"`
	License      string            `toml:"license"`
	Runtime      *int              `toml:"runtime"`
	Icon         string            `toml:"icon"`
	Cover        string            `toml:"cover"`
	Pattern      string            `toml:"pattern"`
	Screenshots  []string          `toml:"screenshots"`
	Names        map[string]string `toml:"names"`
	Descriptions map[string]string `toml:"descriptions"`
	Links        map[string]string `toml:"links"`
	About        map[string]string `toml:"about"`
	Controls     []map[string]any  `toml:"controls"`
	Packages     []Package         `toml:"packages"`
}

type Package struct {
	Target string `toml:"target"`
	Tier   string `toml:"tier"`
	Entry  string `toml:"entry"`
}

// Manifest is a package's manifest.json, schema 2. Field order follows the format's examples.
type Manifest struct {
	Schema       int               `json:"schema"`
	Kind         string            `json:"kind"`
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Names        map[string]string `json:"names,omitempty"`
	Version      string            `json:"version"`
	Description  string            `json:"description,omitempty"`
	Descriptions map[string]string `json:"descriptions,omitempty"`
	Color        string            `json:"color,omitempty"`
	Runtime      int               `json:"runtime"`
	Target       string            `json:"target"`
	Tier         string            `json:"tier"`
	Entry        string            `json:"entry"`
	Icon         string            `json:"icon,omitempty"`
}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9_-]+(\.[a-z0-9_-]+)+$`)
	versionPattern = regexp.MustCompile(`^[0-9][0-9A-Za-z.+-]{0,63}$`)
	partPattern    = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
)

func loadSpec(path string) (*Spec, error) {
	var spec Spec
	meta, err := toml.DecodeFile(path, &spec)
	if err != nil {
		return nil, err
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("%s: unknown keys %s", path, strings.Join(keys, ", "))
	}
	if spec.Kind == "" {
		spec.Kind = "app"
	}
	if spec.Runtime == nil {
		one := 1
		spec.Runtime = &one
	}
	switch {
	case !idPattern.MatchString(spec.ID) || len(spec.ID) > 128:
		return nil, fmt.Errorf("%s: id %q is not a lowercase reverse-DNS ID", path, spec.ID)
	case spec.Name == "" || len([]rune(spec.Name)) > 80:
		return nil, fmt.Errorf("%s: name must have 1 to 80 characters", path)
	case !versionPattern.MatchString(spec.Version):
		return nil, fmt.Errorf("%s: invalid version %q", path, spec.Version)
	case spec.Kind != "app" && spec.Kind != "service":
		return nil, fmt.Errorf("%s: kind must be app or service, not %q", path, spec.Kind)
	case spec.Kind == "service" && spec.Category != "":
		return nil, fmt.Errorf("%s: a service has no category", path)
	case len(spec.Packages) == 0:
		return nil, fmt.Errorf("%s: no [[packages]]", path)
	}
	for _, pkg := range spec.Packages {
		if pkg.Tier != "lite" && pkg.Tier != "full" {
			return nil, fmt.Errorf("%s: tier must be lite or full, not %q", path, pkg.Tier)
		}
		if err := checkName(pkg.Entry); err != nil {
			return nil, fmt.Errorf("%s: entry: %w", path, err)
		}
	}
	return &spec, nil
}

// checkName refuses anything but a canonical relative path of portable parts.
func checkName(name string) error {
	if name == "" {
		return fmt.Errorf("empty path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || !partPattern.MatchString(part) {
			return fmt.Errorf("%q is not a canonical relative path", name)
		}
	}
	return nil
}

func (s *Spec) manifest(pkg Package, icon string) Manifest {
	return Manifest{
		Schema:       2,
		Kind:         s.Kind,
		ID:           s.ID,
		Name:         s.Name,
		Names:        s.Names,
		Version:      s.Version,
		Description:  s.Description,
		Descriptions: s.Descriptions,
		Color:        s.Color,
		Runtime:      *s.Runtime,
		Target:       pkg.Target,
		Tier:         pkg.Tier,
		Entry:        pkg.Entry,
		Icon:         icon,
	}
}

func packageName(m Manifest) string {
	return fmt.Sprintf("%s-%s-%s-%s.gzl", m.ID, m.Version, m.Target, m.Tier)
}
