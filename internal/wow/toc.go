package wow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// TOC is one parsed .toc file.
type TOC struct {
	File   string            `json:"file"`             // absolute path
	Suffix string            `json:"suffix,omitempty"` // flavor suffix, e.g. "Mainline", "Vanilla"; "" for the plain toc
	Fields map[string]string `json:"fields"`           // every "## Key: Value" line, first occurrence wins
	Order  []string          `json:"-"`                // field keys in file order
	Files  []string          `json:"files"`            // the load list

	Interface                  []int    `json:"interface,omitempty"`
	Title                      string   `json:"title,omitempty"` // color codes stripped
	Notes                      string   `json:"notes,omitempty"`
	Version                    string   `json:"version,omitempty"`
	Author                     string   `json:"author,omitempty"`
	Dependencies               []string `json:"dependencies,omitempty"`
	OptionalDeps               []string `json:"optional_deps,omitempty"`
	SavedVariables             []string `json:"saved_variables,omitempty"`
	SavedVariablesPerCharacter []string `json:"saved_variables_per_character,omitempty"`
	LoadOnDemand               bool     `json:"load_on_demand,omitempty"`
	DefaultState               string   `json:"default_state,omitempty"` // "enabled" (default) or "disabled"
}

// Get returns a field by key, case insensitive.
func (t *TOC) Get(key string) string {
	for k, v := range t.Fields {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

var colorCode = regexp.MustCompile(`\|c[0-9a-fA-F]{8}|\|r|\|T[^|]*\|t|\|A[^|]*\|a`)

// StripColorCodes removes WoW UI escape sequences such as |cff00ff00 and |r.
func StripColorCodes(s string) string { return colorCode.ReplaceAllString(s, "") }

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseTOC reads and parses one .toc file. suffix is the flavor suffix taken
// from the file name by the caller.
func ParseTOC(path, suffix string) (*TOC, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := &TOC{File: path, Suffix: suffix, Fields: map[string]string{}}
	for _, ln := range SplitLines(b) {
		ln = strings.TrimPrefix(ln, "\ufeff")
		ln = strings.TrimSpace(ln)
		switch {
		case ln == "":
		case strings.HasPrefix(ln, "##"):
			kv := strings.TrimSpace(ln[2:])
			i := strings.Index(kv, ":")
			if i < 0 {
				continue
			}
			k := strings.TrimSpace(kv[:i])
			v := strings.TrimSpace(kv[i+1:])
			if _, dup := t.Fields[k]; !dup {
				t.Fields[k] = v
				t.Order = append(t.Order, k)
			}
		case strings.HasPrefix(ln, "#"):
			// comment
		default:
			t.Files = append(t.Files, ln)
		}
	}
	for _, p := range splitList(t.Get("Interface")) {
		if n, err := strconv.Atoi(p); err == nil {
			t.Interface = append(t.Interface, n)
		}
	}
	t.Title = StripColorCodes(t.Get("Title"))
	t.Notes = StripColorCodes(t.Get("Notes"))
	t.Version = t.Get("Version")
	t.Author = t.Get("Author")
	t.Dependencies = append(splitList(t.Get("Dependencies")), splitList(t.Get("RequiredDeps"))...)
	t.Dependencies = append(t.Dependencies, splitList(t.Get("Dependency"))...)
	t.OptionalDeps = splitList(t.Get("OptionalDeps"))
	t.SavedVariables = splitList(t.Get("SavedVariables"))
	t.SavedVariablesPerCharacter = splitList(t.Get("SavedVariablesPerCharacter"))
	t.LoadOnDemand = t.Get("LoadOnDemand") == "1"
	if strings.EqualFold(t.Get("DefaultState"), "disabled") {
		t.DefaultState = "disabled"
	} else {
		t.DefaultState = "enabled"
	}
	return t, nil
}

// Addon is one folder under Interface/AddOns with its .toc files.
type Addon struct {
	Name        string   `json:"name"` // folder name, which is the name the client uses
	Dir         string   `json:"dir"`
	TOCs        []*TOC   `json:"tocs"`                   // tocs named after the folder; the plain one first
	IgnoredTOCs []string `json:"ignored_tocs,omitempty"` // .toc files not named after the folder (the client ignores them)
}

// tocSuffix returns the flavor suffix of toc file base (without .toc) for the
// addon folder name, and whether the client would consider the file at all.
func tocSuffix(folder, base string) (suffix string, ok bool) {
	if strings.EqualFold(folder, base) {
		return "", true
	}
	if len(base) > len(folder)+1 && strings.EqualFold(base[:len(folder)], folder) {
		sep := base[len(folder)]
		if sep == '_' || sep == '-' {
			return base[len(folder)+1:], true
		}
	}
	return "", false
}

// LoadAddon parses the .toc files of the addon folder at dir.
func LoadAddon(dir string) (*Addon, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	a := &Addon{Name: filepath.Base(dir), Dir: dir}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".toc") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		suffix, ok := tocSuffix(a.Name, base)
		if !ok {
			a.IgnoredTOCs = append(a.IgnoredTOCs, e.Name())
			continue
		}
		t, err := ParseTOC(filepath.Join(dir, e.Name()), suffix)
		if err != nil {
			return nil, err
		}
		a.TOCs = append(a.TOCs, t)
	}
	sort.SliceStable(a.TOCs, func(i, j int) bool {
		if (a.TOCs[i].Suffix == "") != (a.TOCs[j].Suffix == "") {
			return a.TOCs[i].Suffix == ""
		}
		return a.TOCs[i].Suffix < a.TOCs[j].Suffix
	})
	return a, nil
}

// flavorSuffixes lists, per flavor short name, the toc suffixes the client
// loads, most specific first. "Classic" is Blizzard's generic suffix for the
// current progression client. WoW Forever (codename Camelot) uses "_Camelot",
// seen on a real install in 2026-11; it is not known to read the Vanilla toc.
var flavorSuffixes = map[string][]string{
	"retail":          {"Mainline"},
	"ptr":             {"Mainline"},
	"xptr":            {"Mainline"},
	"beta":            {"Mainline"},
	"classic":         {"Mists", "Classic", "Cata", "Wrath", "WOTLKC", "TBC", "BCC"},
	"classic_ptr":     {"Mists", "Classic", "Cata", "Wrath", "WOTLKC", "TBC", "BCC"},
	"classic_beta":    {"Mists", "Classic", "Cata", "Wrath", "WOTLKC", "TBC", "BCC"},
	"classic_era":     {"Vanilla", "Classic"},
	"classic_era_ptr": {"Vanilla", "Classic"},
	"forever":         {"Camelot"},
}

// TOCFor picks the .toc the client of flavorName would load: the matching
// suffixed file first, then the plain one, then whatever exists.
func (a *Addon) TOCFor(flavorName string) *TOC {
	if len(a.TOCs) == 0 {
		return nil
	}
	for _, want := range flavorSuffixes[strings.ToLower(flavorName)] {
		for _, t := range a.TOCs {
			if strings.EqualFold(t.Suffix, want) {
				return t
			}
		}
	}
	for _, t := range a.TOCs {
		if t.Suffix == "" {
			return t
		}
	}
	return a.TOCs[0]
}

// ListAddons loads every addon folder under the flavor's Interface/AddOns.
// Folders without any .toc are skipped.
func ListAddons(fl *Flavor) ([]*Addon, error) {
	dir := fl.AddOnsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Addon
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		a, err := LoadAddon(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("addon %s: %w", e.Name(), err)
		}
		if len(a.TOCs) == 0 && len(a.IgnoredTOCs) == 0 {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// FindAddon returns the installed addon whose folder matches name (case
// insensitive).
func FindAddon(fl *Flavor, name string) (*Addon, error) {
	if err := ValidateName("addon", name); err != nil {
		return nil, err
	}
	p := filepath.Join(fl.AddOnsDir(), name)
	if isDir(p) {
		return LoadAddon(p)
	}
	entries, err := os.ReadDir(fl.AddOnsDir())
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), name) {
			return LoadAddon(filepath.Join(fl.AddOnsDir(), e.Name()))
		}
	}
	return nil, fmt.Errorf("no addon %q under %s", name, fl.AddOnsDir())
}
