package wow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Flavor is one installed client under the WoW root, for example _retail_.
type Flavor struct {
	Dir          string `json:"dir"`                 // folder name, e.g. "_retail_"
	Name         string `json:"name"`                // short name, e.g. "retail", "classic_era", "forever"
	Path         string `json:"path"`                // absolute path of the flavor folder
	Product      string `json:"product,omitempty"`   // Battle.net product code from .build.info, e.g. "wow"
	Version      string `json:"version,omitempty"`   // game version from .build.info, e.g. "11.2.5.64104"
	Interface    int    `json:"interface,omitempty"` // TOC interface number derived from Version, 0 when unknown
	HasInterface bool   `json:"has_interface"`
	HasWTF       bool   `json:"has_wtf"`
}

// Install is a WoW root folder and the client flavors found under it.
type Install struct {
	Root    string   `json:"root"`
	Source  string   `json:"source"` // "explicit" or "auto"
	Flavors []Flavor `json:"flavors"`
}

// productDirs maps Battle.net product codes from .build.info to flavor folders.
var productDirs = map[string]string{
	"wow":                 "_retail_",
	"wowt":                "_ptr_",
	"wowxptr":             "_xptr_",
	"wow_beta":            "_beta_",
	"wow_classic":         "_classic_",
	"wow_classic_ptr":     "_classic_ptr_",
	"wow_classic_beta":    "_classic_beta_",
	"wow_classic_era":     "_classic_era_",
	"wow_classic_era_ptr": "_classic_era_ptr_",
	"wowf":                "_forever_",
}

// AddOnsDir is Interface/AddOns under the flavor.
func (f *Flavor) AddOnsDir() string { return filepath.Join(f.Path, "Interface", "AddOns") }

// WTFDir is the configuration tree under the flavor.
func (f *Flavor) WTFDir() string { return filepath.Join(f.Path, "WTF") }

// ConfigWTF is the global CVar file.
func (f *Flavor) ConfigWTF() string { return filepath.Join(f.WTFDir(), "Config.wtf") }

// AccountsDir holds one folder per Battle.net game account.
func (f *Flavor) AccountsDir() string { return filepath.Join(f.WTFDir(), "Account") }

// flavorName turns "_classic_era_" into "classic_era".
func flavorName(dir string) string { return strings.Trim(dir, "_") }

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// IsFlavorDir reports whether p looks like a client folder (has Interface or WTF).
func IsFlavorDir(p string) bool {
	return isDir(filepath.Join(p, "Interface")) || isDir(filepath.Join(p, "WTF"))
}

// IsRoot reports whether p looks like a WoW root: it has a .build.info file
// or at least one _flavor_ folder that is a client folder.
func IsRoot(p string) bool {
	if isFile(filepath.Join(p, ".build.info")) {
		return true
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && strings.HasPrefix(n, "_") && strings.HasSuffix(n, "_") && IsFlavorDir(filepath.Join(p, n)) {
			return true
		}
	}
	return false
}

// Open loads the install at root. root may also point directly at a flavor
// folder such as ".../_retail_", in which case the install has that one flavor.
func Open(root string) (*Install, error) {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	root = filepath.Clean(root)
	if !isDir(root) {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	inst := &Install{Root: root, Source: "explicit"}
	switch {
	case IsRoot(root):
		if err := inst.scan(); err != nil {
			return nil, err
		}
	case IsFlavorDir(root):
		fl := Flavor{Dir: filepath.Base(root), Name: flavorName(filepath.Base(root)), Path: root}
		fl.HasInterface = isDir(filepath.Join(root, "Interface"))
		fl.HasWTF = isDir(filepath.Join(root, "WTF"))
		inst.Root = filepath.Dir(root)
		inst.Flavors = []Flavor{fl}
		inst.applyBuildInfo()
	default:
		return nil, fmt.Errorf("%s has neither _flavor_ folders nor Interface/WTF; point WOW_ROOT at the folder that contains _retail_, _classic_, _forever_ etc.", root)
	}
	if len(inst.Flavors) == 0 {
		return nil, fmt.Errorf("%s contains no client folders", root)
	}
	return inst, nil
}

func (inst *Install) scan() error {
	entries, err := os.ReadDir(inst.Root)
	if err != nil {
		return err
	}
	inst.Flavors = inst.Flavors[:0]
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || !strings.HasPrefix(n, "_") || !strings.HasSuffix(n, "_") {
			continue
		}
		p := filepath.Join(inst.Root, n)
		if !IsFlavorDir(p) {
			continue
		}
		inst.Flavors = append(inst.Flavors, Flavor{
			Dir:          n,
			Name:         flavorName(n),
			Path:         p,
			HasInterface: isDir(filepath.Join(p, "Interface")),
			HasWTF:       isDir(filepath.Join(p, "WTF")),
		})
	}
	sort.Slice(inst.Flavors, func(i, j int) bool { return inst.Flavors[i].Dir < inst.Flavors[j].Dir })
	inst.applyBuildInfo()
	return nil
}

// applyBuildInfo fills Product, Version and Interface from <root>/.build.info.
func (inst *Install) applyBuildInfo() {
	rows, err := ParseBuildInfo(filepath.Join(inst.Root, ".build.info"))
	if err != nil {
		return
	}
	for i := range inst.Flavors {
		fl := &inst.Flavors[i]
		for _, r := range rows {
			if productDirs[r.Product] == fl.Dir {
				fl.Product = r.Product
				fl.Version = r.Version
				fl.Interface = InterfaceFromVersion(r.Version)
				break
			}
		}
	}
}

// BuildInfoRow is one product line of .build.info.
type BuildInfoRow struct {
	Product string
	Version string
	Branch  string
}

// ParseBuildInfo reads Blizzard's pipe-delimited .build.info. The header row
// names each column as "Name!TYPE:n"; only Product, Version and Branch are kept.
func ParseBuildInfo(path string) ([]BuildInfoRow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := SplitLines(b)
	if len(lines) < 2 {
		return nil, errors.New(".build.info has no rows")
	}
	cols := map[string]int{}
	for i, h := range strings.Split(lines[0], "|") {
		name := h
		if j := strings.Index(h, "!"); j >= 0 {
			name = h[:j]
		}
		cols[strings.ToLower(strings.TrimSpace(name))] = i
	}
	pi, ok1 := cols["product"]
	vi, ok2 := cols["version"]
	bi := cols["branch"]
	if !ok1 || !ok2 {
		return nil, errors.New(".build.info has no Product/Version columns")
	}
	var rows []BuildInfoRow
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		f := strings.Split(ln, "|")
		if pi >= len(f) || vi >= len(f) {
			continue
		}
		r := BuildInfoRow{Product: strings.TrimSpace(f[pi]), Version: strings.TrimSpace(f[vi])}
		if bi < len(f) {
			r.Branch = strings.TrimSpace(f[bi])
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// InterfaceFromVersion converts "11.2.5.64104" into the TOC interface number
// 110205 (major*10000 + minor*100 + patch). It returns 0 when the version
// does not have three numeric parts.
func InterfaceFromVersion(v string) int {
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return 0
	}
	var n [3]int
	for i := 0; i < 3; i++ {
		x, err := strconv.Atoi(parts[i])
		if err != nil || x < 0 {
			return 0
		}
		n[i] = x
	}
	return n[0]*10000 + n[1]*100 + n[2]
}

// Flavor returns the flavor named by name (folder or short name, case
// insensitive). An empty name picks defaultName, then "retail", then the only
// flavor present, and otherwise fails with the list of choices.
func (inst *Install) Flavor(name, defaultName string) (*Flavor, error) {
	if name == "" {
		name = defaultName
	}
	if name == "" {
		if len(inst.Flavors) == 1 {
			return &inst.Flavors[0], nil
		}
		name = "retail"
	}
	want := strings.ToLower(strings.Trim(name, "_"))
	aliases := map[string]string{"mainline": "retail", "live": "retail", "era": "classic_era", "vanilla": "classic_era"}
	if a, ok := aliases[want]; ok {
		want = a
	}
	for i := range inst.Flavors {
		if strings.ToLower(inst.Flavors[i].Name) == want {
			return &inst.Flavors[i], nil
		}
	}
	names := make([]string, len(inst.Flavors))
	for i, f := range inst.Flavors {
		names[i] = f.Name
	}
	return nil, fmt.Errorf("no flavor %q under %s; installed: %s", name, inst.Root, strings.Join(names, ", "))
}

// Candidates lists the folders Detect checks, in order, for this OS. Only
// folders that exist are returned.
func Candidates() []string {
	home, _ := os.UserHomeDir()
	var raw []string
	const wow = "World of Warcraft"
	switch runtime.GOOS {
	case "windows":
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if v := os.Getenv(env); v != "" {
				raw = append(raw, filepath.Join(v, wow))
			}
		}
		for d := 'C'; d <= 'Z'; d++ {
			drive := string(d) + `:\`
			if !isDir(drive) {
				continue
			}
			raw = append(raw,
				filepath.Join(drive, "Program Files (x86)", wow),
				filepath.Join(drive, "Program Files", wow),
				filepath.Join(drive, wow),
				filepath.Join(drive, "Games", wow),
				filepath.Join(drive, "Battle.net", wow),
			)
		}
	case "darwin":
		raw = append(raw, filepath.Join("/Applications", wow), filepath.Join(home, "Applications", wow))
	default:
		// Wine, Lutris, Bottles and Proton prefixes.
		var prefixes []string
		prefixes = append(prefixes, filepath.Join(home, ".wine"))
		for _, pat := range []string{
			filepath.Join(home, "Games", "*"),
			filepath.Join(home, ".var", "app", "com.usebottles.bottles", "data", "bottles", "bottles", "*"),
			filepath.Join(home, ".local", "share", "bottles", "bottles", "*"),
			filepath.Join(home, ".steam", "steam", "steamapps", "compatdata", "*", "pfx"),
			filepath.Join(home, ".local", "share", "Steam", "steamapps", "compatdata", "*", "pfx"),
		} {
			m, _ := filepath.Glob(pat)
			prefixes = append(prefixes, m...)
		}
		for _, pfx := range prefixes {
			c := filepath.Join(pfx, "drive_c")
			raw = append(raw,
				filepath.Join(c, "Program Files (x86)", wow),
				filepath.Join(c, "Program Files", wow),
				filepath.Join(c, wow),
				filepath.Join(c, "Games", wow),
			)
		}
		raw = append(raw, filepath.Join(home, "Games", wow))
		// Inside WSL the Windows drives are mounted under /mnt/<letter>.
		if IsWSL() {
			drives, _ := filepath.Glob("/mnt/[a-z]")
			for _, d := range drives {
				raw = append(raw,
					filepath.Join(d, "Program Files (x86)", wow),
					filepath.Join(d, "Program Files", wow),
					filepath.Join(d, wow),
					filepath.Join(d, "Games", wow),
					filepath.Join(d, "Battle.net", wow),
				)
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range raw {
		if seen[p] || !isDir(p) {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// Detect opens the install at explicit when given (the -root flag or
// WOW_ROOT), otherwise the first candidate folder that is a WoW root.
func Detect(explicit string) (*Install, error) {
	if explicit != "" {
		return Open(explicit)
	}
	cands := Candidates()
	for _, c := range cands {
		if IsRoot(c) {
			inst, err := Open(c)
			if err == nil {
				inst.Source = "auto"
				return inst, nil
			}
		}
	}
	if len(cands) == 0 {
		return nil, errors.New("no World of Warcraft install found in the usual places; set WOW_ROOT (or -root) to the folder that contains _retail_, _classic_, _forever_ etc.")
	}
	return nil, fmt.Errorf("no WoW root among existing candidates %s; set WOW_ROOT (or -root)", strings.Join(cands, ", "))
}
