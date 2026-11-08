package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caboose-ai/wow-mcp/internal/wow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ------------------------------------------------------------ wow_installs

type installsIn struct{}

type flavorOut struct {
	Dir          string `json:"dir"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Product      string `json:"product,omitempty"`
	Version      string `json:"version,omitempty"`
	Interface    int    `json:"interface,omitempty"`
	HasInterface bool   `json:"has_interface"`
	HasWTF       bool   `json:"has_wtf"`
	Addons       int    `json:"addons"`
	Accounts     int    `json:"accounts"`
	Characters   int    `json:"characters"`
}

type installsOut struct {
	Root          string      `json:"root"`
	Source        string      `json:"source"`
	DefaultFlavor string      `json:"default_flavor,omitempty"`
	Flavors       []flavorOut `json:"flavors"`
	ClientRunning *bool       `json:"client_running,omitempty"`
	ClientDetail  string      `json:"client_detail,omitempty"`
	ReadOnly      bool        `json:"read_only"`
	BackupSuffix  string      `json:"backup_suffix"`
}

func (s *Server) installs(_ context.Context, _ *mcp.CallToolRequest, _ installsIn) (*mcp.CallToolResult, installsOut, error) {
	inst, err := s.install()
	if err != nil {
		return nil, installsOut{}, err
	}
	out := installsOut{Root: inst.Root, Source: inst.Source, DefaultFlavor: s.opts.DefaultFlavor, ReadOnly: s.opts.ReadOnly, BackupSuffix: wow.BackupSuffix}
	for i := range inst.Flavors {
		fl := &inst.Flavors[i]
		fo := flavorOut{Dir: fl.Dir, Name: fl.Name, Path: fl.Path, Product: fl.Product, Version: fl.Version, Interface: fl.Interface, HasInterface: fl.HasInterface, HasWTF: fl.HasWTF}
		if addons, err := wow.ListAddons(fl); err == nil {
			fo.Addons = len(addons)
		}
		if accs, err := fl.Accounts(); err == nil {
			fo.Accounts = len(accs)
			for _, a := range accs {
				for _, r := range a.Realms {
					fo.Characters += len(r.Characters)
				}
			}
		}
		out.Flavors = append(out.Flavors, fo)
	}
	if running, detail, err := wow.WoWRunning(); err == nil {
		out.ClientRunning = boolp(running)
		out.ClientDetail = detail
	}
	return nil, out, nil
}

// ------------------------------------------------------------ wow_characters

type charactersIn struct {
	Flavor string `json:"flavor,omitempty" jsonschema:"Client flavor: retail, classic, classic_era, forever, ptr... Defaults to the server default"`
}

type characterOut struct {
	Account        string `json:"account"`
	Realm          string `json:"realm"`
	Name           string `json:"name"`
	Dir            string `json:"dir"`
	HasAddonsTxt   bool   `json:"has_addons_txt"`
	SavedVariables int    `json:"saved_variables"`
	Modified       string `json:"modified,omitempty"`
}

type realmOut struct {
	Name       string         `json:"name"`
	Characters []characterOut `json:"characters"`
}

type accountOut struct {
	Name           string     `json:"name"`
	Dir            string     `json:"dir"`
	SavedVariables int        `json:"saved_variables"`
	Realms         []realmOut `json:"realms"`
}

type charactersOut struct {
	Flavor   string       `json:"flavor"`
	Accounts []accountOut `json:"accounts"`
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Server) characters(_ context.Context, _ *mcp.CallToolRequest, in charactersIn) (*mcp.CallToolResult, charactersOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, charactersOut{}, err
	}
	accs, err := fl.Accounts()
	if err != nil {
		return nil, charactersOut{}, err
	}
	out := charactersOut{Flavor: fl.Name, Accounts: []accountOut{}}
	for _, a := range accs {
		ao := accountOut{Name: a.Name, Dir: a.Dir, SavedVariables: a.SavedVariables, Realms: []realmOut{}}
		for _, r := range a.Realms {
			ro := realmOut{Name: r.Name}
			for _, c := range r.Characters {
				ro.Characters = append(ro.Characters, characterOut{Account: c.Account, Realm: c.Realm, Name: c.Name, Dir: c.Dir, HasAddonsTxt: c.HasAddonsTxt, SavedVariables: c.SavedVariables, Modified: fmtTime(c.Modified)})
			}
			ao.Realms = append(ao.Realms, ro)
		}
		out.Accounts = append(out.Accounts, ao)
	}
	return nil, out, nil
}

// ------------------------------------------------------------ wow_addons

type addonsIn struct {
	Flavor    string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Account   string `json:"account,omitempty" jsonschema:"Account folder under WTF/Account; optional when unambiguous"`
	Realm     string `json:"realm,omitempty" jsonschema:"Realm folder; optional when the character name is unique"`
	Character string `json:"character,omitempty" jsonschema:"Character whose AddOns.txt decides the enabled state. Without it no enabled state is reported"`
	Filter    string `json:"filter,omitempty" jsonschema:"Case-insensitive substring matched against the folder name and title"`
}

type addonOut struct {
	Name                       string   `json:"name"`
	Title                      string   `json:"title,omitempty"`
	Notes                      string   `json:"notes,omitempty"`
	Version                    string   `json:"version,omitempty"`
	Author                     string   `json:"author,omitempty"`
	Interface                  []int    `json:"interface,omitempty"`
	OutOfDate                  *bool    `json:"out_of_date,omitempty"`
	LoadOnDemand               bool     `json:"load_on_demand,omitempty"`
	DefaultState               string   `json:"default_state,omitempty"`
	Dependencies               []string `json:"dependencies,omitempty"`
	OptionalDeps               []string `json:"optional_deps,omitempty"`
	SavedVariables             []string `json:"saved_variables,omitempty"`
	SavedVariablesPerCharacter []string `json:"saved_variables_per_character,omitempty"`
	TOC                        string   `json:"toc,omitempty"`
	TOCFiles                   []string `json:"toc_files,omitempty"`
	IgnoredTOCs                []string `json:"ignored_tocs,omitempty"`
	Enabled                    *bool    `json:"enabled,omitempty"`
	Listed                     *bool    `json:"listed,omitempty"`
}

type addonsOut struct {
	Flavor        string     `json:"flavor"`
	Interface     int        `json:"interface,omitempty"`
	AddOnsDir     string     `json:"addons_dir"`
	Character     string     `json:"character,omitempty"`
	AddonsTxt     string     `json:"addons_txt,omitempty"`
	Count         int        `json:"count"`
	Addons        []addonOut `json:"addons"`
	AddonsTxtOnly []string   `json:"addons_txt_only,omitempty"` // listed in AddOns.txt but no longer installed (Blizzard_* excluded)
}

func containsInt(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}

func summarize(a *wow.Addon, fl *wow.Flavor) addonOut {
	o := addonOut{Name: a.Name, IgnoredTOCs: a.IgnoredTOCs}
	for _, t := range a.TOCs {
		o.TOCFiles = append(o.TOCFiles, filepath.Base(t.File))
	}
	t := a.TOCFor(fl.Name)
	if t == nil {
		return o
	}
	o.Title, o.Notes, o.Version, o.Author = t.Title, t.Notes, t.Version, t.Author
	o.Interface = t.Interface
	o.LoadOnDemand = t.LoadOnDemand
	o.DefaultState = t.DefaultState
	o.Dependencies, o.OptionalDeps = t.Dependencies, t.OptionalDeps
	o.SavedVariables, o.SavedVariablesPerCharacter = t.SavedVariables, t.SavedVariablesPerCharacter
	o.TOC = filepath.Base(t.File)
	if fl.Interface > 0 && len(t.Interface) > 0 {
		o.OutOfDate = boolp(!containsInt(t.Interface, fl.Interface))
	}
	return o
}

func (s *Server) addons(_ context.Context, _ *mcp.CallToolRequest, in addonsIn) (*mcp.CallToolResult, addonsOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, addonsOut{}, err
	}
	list, err := wow.ListAddons(fl)
	if err != nil {
		return nil, addonsOut{}, err
	}
	out := addonsOut{Flavor: fl.Name, Interface: fl.Interface, AddOnsDir: fl.AddOnsDir(), Addons: []addonOut{}}
	var txt *wow.AddonsTxt
	if in.Character != "" {
		ch, err := fl.ResolveCharacter(in.Account, in.Realm, in.Character)
		if err != nil {
			return nil, addonsOut{}, err
		}
		out.Character = ch.Account + "/" + ch.Realm + "/" + ch.Name
		out.AddonsTxt = ch.AddonsTxtPath()
		txt, err = wow.LoadAddonsTxt(ch.AddonsTxtPath())
		if err != nil {
			return nil, addonsOut{}, err
		}
	}
	filter := strings.ToLower(in.Filter)
	installed := map[string]bool{}
	for _, a := range list {
		installed[strings.ToLower(a.Name)] = true
		o := summarize(a, fl)
		if filter != "" && !strings.Contains(strings.ToLower(o.Name), filter) && !strings.Contains(strings.ToLower(o.Title), filter) {
			continue
		}
		if txt != nil {
			enabled, listed := txt.Get(a.Name)
			if !listed {
				enabled = o.DefaultState != "disabled"
			}
			o.Enabled, o.Listed = boolp(enabled), boolp(listed)
		}
		out.Addons = append(out.Addons, o)
	}
	out.Count = len(out.Addons)
	if txt != nil {
		for _, n := range txt.Names() {
			// Blizzard_* addons ship inside the game data, so they are never on disk.
			if strings.HasPrefix(strings.ToLower(n), "blizzard_") {
				continue
			}
			if !installed[strings.ToLower(n)] && (filter == "" || strings.Contains(strings.ToLower(n), filter)) {
				out.AddonsTxtOnly = append(out.AddonsTxtOnly, n)
			}
		}
	}
	return nil, out, nil
}

// ------------------------------------------------------------ wow_addon

type addonIn struct {
	Flavor string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Name   string `json:"name" jsonschema:"Addon folder name under Interface/AddOns (case insensitive)"`
}

type tocOut struct {
	File      string            `json:"file"`
	Suffix    string            `json:"suffix,omitempty"`
	Interface []int             `json:"interface,omitempty"`
	Fields    map[string]string `json:"fields"`
	Files     []string          `json:"files"`
}

type svFileOut struct {
	Scope     string `json:"scope"` // "account" or "character"
	Account   string `json:"account"`
	Realm     string `json:"realm,omitempty"`
	Character string `json:"character,omitempty"`
	File      string `json:"file"`
	Size      int64  `json:"size"`
	Modified  string `json:"modified,omitempty"`
}

type charStateOut struct {
	Account   string `json:"account"`
	Realm     string `json:"realm"`
	Character string `json:"character"`
	Enabled   bool   `json:"enabled"`
	Listed    bool   `json:"listed"`
}

type addonDetailOut struct {
	addonOut
	Dir                string         `json:"dir"`
	TOCs               []tocOut       `json:"tocs"`
	FileCount          int            `json:"file_count"`
	TotalBytes         int64          `json:"total_bytes"`
	Files              []string       `json:"files"`
	FilesTruncated     bool           `json:"files_truncated,omitempty"`
	SavedVariableFiles []svFileOut    `json:"saved_variable_files"`
	EnabledOn          []charStateOut `json:"enabled_on"`
}

const maxListedFiles = 300

func (s *Server) addon(_ context.Context, _ *mcp.CallToolRequest, in addonIn) (*mcp.CallToolResult, addonDetailOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, addonDetailOut{}, err
	}
	a, err := wow.FindAddon(fl, in.Name)
	if err != nil {
		return nil, addonDetailOut{}, err
	}
	out := addonDetailOut{addonOut: summarize(a, fl), Dir: a.Dir, TOCs: []tocOut{}, Files: []string{}, SavedVariableFiles: []svFileOut{}, EnabledOn: []charStateOut{}}
	for _, t := range a.TOCs {
		out.TOCs = append(out.TOCs, tocOut{File: filepath.Base(t.File), Suffix: t.Suffix, Interface: t.Interface, Fields: t.Fields, Files: t.Files})
	}
	_ = filepath.WalkDir(a.Dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out.FileCount++
		out.TotalBytes += info.Size()
		if rel, err := filepath.Rel(a.Dir, p); err == nil {
			if len(out.Files) < maxListedFiles {
				out.Files = append(out.Files, filepath.ToSlash(rel))
			} else {
				out.FilesTruncated = true
			}
		}
		return nil
	})
	accs, err := fl.Accounts()
	if err != nil {
		return nil, addonDetailOut{}, err
	}
	addSV := func(scope, account, realm, character, file string) {
		st, err := os.Stat(file)
		if err != nil {
			return
		}
		out.SavedVariableFiles = append(out.SavedVariableFiles, svFileOut{Scope: scope, Account: account, Realm: realm, Character: character, File: file, Size: st.Size(), Modified: fmtTime(st.ModTime())})
	}
	for _, acc := range accs {
		addSV("account", acc.Name, "", "", filepath.Join(acc.SavedVariablesDir(), a.Name+".lua"))
		for _, r := range acc.Realms {
			for _, c := range r.Characters {
				addSV("character", c.Account, c.Realm, c.Name, filepath.Join(c.SavedVariablesDir(), a.Name+".lua"))
				txt, err := wow.LoadAddonsTxt(c.AddonsTxtPath())
				if err != nil {
					continue
				}
				enabled, listed := txt.Get(a.Name)
				if !listed {
					enabled = out.DefaultState != "disabled"
				}
				out.EnabledOn = append(out.EnabledOn, charStateOut{Account: c.Account, Realm: c.Realm, Character: c.Name, Enabled: enabled, Listed: listed})
			}
		}
	}
	return nil, out, nil
}

// ------------------------------------------------------------ wow_cvars

type cvarsIn struct {
	Flavor    string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Account   string `json:"account,omitempty" jsonschema:"Account folder; only used with character"`
	Realm     string `json:"realm,omitempty" jsonschema:"Realm folder; only used with character"`
	Character string `json:"character,omitempty" jsonschema:"When set, read the character's config-cache.wtf instead of the global Config.wtf"`
	Filter    string `json:"filter,omitempty" jsonschema:"Case-insensitive substring matched against the CVar name"`
}

type cvarsOut struct {
	Flavor string     `json:"flavor"`
	Scope  string     `json:"scope"` // "global" or "ACCOUNT/Realm/Char"
	File   string     `json:"file"`
	Exists bool       `json:"exists"`
	Count  int        `json:"count"`
	CVars  []wow.CVar `json:"cvars"`
}

func (s *Server) cvarFile(fl *wow.Flavor, account, realm, character string) (file, label string, err error) {
	if character == "" {
		if realm != "" || account != "" {
			return "", "", fmt.Errorf("account/realm only make sense together with character; without a character the global Config.wtf is read")
		}
		return fl.ConfigWTF(), "global", nil
	}
	ch, err := fl.ResolveCharacter(account, realm, character)
	if err != nil {
		return "", "", err
	}
	return ch.ConfigCachePath(), ch.Account + "/" + ch.Realm + "/" + ch.Name, nil
}

func (s *Server) cvars(_ context.Context, _ *mcp.CallToolRequest, in cvarsIn) (*mcp.CallToolResult, cvarsOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, cvarsOut{}, err
	}
	file, label, err := s.cvarFile(fl, in.Account, in.Realm, in.Character)
	if err != nil {
		return nil, cvarsOut{}, err
	}
	cfg, err := wow.LoadWTFConfig(file)
	if err != nil {
		return nil, cvarsOut{}, err
	}
	out := cvarsOut{Flavor: fl.Name, Scope: label, File: file, Exists: cfg.Existed, CVars: []wow.CVar{}}
	filter := strings.ToLower(in.Filter)
	for _, c := range cfg.All() {
		if filter == "" || strings.Contains(strings.ToLower(c.Name), filter) {
			out.CVars = append(out.CVars, c)
		}
	}
	out.Count = len(out.CVars)
	return nil, out, nil
}

// ------------------------------------------------------------ wow_saved_variables

type savedVariablesIn struct {
	Flavor    string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Addon     string `json:"addon" jsonschema:"Addon folder name; the file is SavedVariables/<addon>.lua"`
	Account   string `json:"account,omitempty" jsonschema:"Account folder; optional when only one account exists"`
	Realm     string `json:"realm,omitempty" jsonschema:"Realm folder; optional when the character name is unique"`
	Character string `json:"character,omitempty" jsonschema:"Character for per-character SavedVariables; leave empty for the account-wide file"`
	Path      string `json:"path,omitempty" jsonschema:"Dotted path into the data, first segment is the variable name, e.g. MyAddonDB.profiles.Default. Use a JSON array of segments when a key contains a dot"`
	Depth     int    `json:"depth,omitempty" jsonschema:"Table nesting to return below the path (default 4). Deeper tables are summarised. Reduced automatically when the output would exceed max_bytes"`
	MaxBytes  int    `json:"max_bytes,omitempty" jsonschema:"Cap on the JSON size returned (default: server setting, 100000)"`
}

type savedVariablesOut struct {
	Flavor    string   `json:"flavor"`
	Scope     string   `json:"scope"`
	File      string   `json:"file"`
	Size      int64    `json:"size"`
	Modified  string   `json:"modified,omitempty"`
	Variables []string `json:"variables"`
	Path      string   `json:"path,omitempty"`
	Depth     int      `json:"depth"`
	Data      any      `json:"data"`
	Note      string   `json:"note,omitempty"`
}

func (s *Server) savedVariables(_ context.Context, _ *mcp.CallToolRequest, in savedVariablesIn) (*mcp.CallToolResult, savedVariablesOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, savedVariablesOut{}, err
	}
	if err := wow.ValidateName("addon", in.Addon); err != nil {
		return nil, savedVariablesOut{}, err
	}
	sc, err := resolveScope(fl, in.Account, in.Realm, in.Character)
	if err != nil {
		return nil, savedVariablesOut{}, err
	}
	file := sc.savedVariablesFile(in.Addon)
	st, err := os.Stat(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, savedVariablesOut{}, fmt.Errorf("no SavedVariables file %s (the addon has not saved anything for this scope yet)", file)
		}
		return nil, savedVariablesOut{}, err
	}
	sv, err := wow.LoadSavedVariables(file)
	if err != nil {
		return nil, savedVariablesOut{}, err
	}
	out := savedVariablesOut{Flavor: fl.Name, Scope: sc.Label, File: file, Size: st.Size(), Modified: fmtTime(st.ModTime()), Variables: sv.Names(), Path: in.Path}
	if out.Variables == nil {
		out.Variables = []string{}
	}

	var value any
	if in.Path != "" {
		path, err := wow.ParsePath(in.Path)
		if err != nil {
			return nil, savedVariablesOut{}, err
		}
		v, ok := sv.Lookup(path)
		if !ok {
			return nil, savedVariablesOut{}, fmt.Errorf("nothing at path %q in %s (variables: %s)", in.Path, filepath.Base(file), strings.Join(sv.Names(), ", "))
		}
		value = v
	} else {
		t := &wow.LuaTable{}
		for _, v := range sv.Vars {
			t.Entries = append(t.Entries, wow.LuaEntry{Key: v.Name, Value: v.Value})
		}
		value = t
	}

	depth := in.Depth
	if depth <= 0 {
		depth = 4
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 {
		maxBytes = s.opts.MaxOutputBytes
	}
	for d := depth; d >= 1; d-- {
		data := wow.LuaToJSON(value, d)
		if n, err := jsonSize(data); err == nil && n <= maxBytes {
			out.Data, out.Depth = data, d
			if d < depth {
				out.Note = fmt.Sprintf("depth reduced from %d to %d to stay under %d bytes; pass a path to go deeper", depth, d, maxBytes)
			}
			return nil, out, nil
		}
	}
	// Even one level is too large: return a key sample.
	if t, ok := value.(*wow.LuaTable); ok {
		keys := make([]string, 0, 200)
		for _, e := range t.Entries {
			if len(keys) == 200 {
				break
			}
			keys = append(keys, wow.KeyString(e.Key))
		}
		sort.Strings(keys)
		out.Data = map[string]any{"entries": len(t.Entries), "sample_keys": keys}
		out.Depth = 0
		out.Note = fmt.Sprintf("table has %d entries and does not fit in %d bytes even at depth 1; pass a path to one of the keys", len(t.Entries), maxBytes)
		return nil, out, nil
	}
	return nil, savedVariablesOut{}, fmt.Errorf("value at %q is larger than %d bytes; raise max_bytes", in.Path, maxBytes)
}

// ------------------------------------------------------------ wow_keybindings

type bindingsIn struct {
	Flavor    string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Account   string `json:"account,omitempty" jsonschema:"Account folder; optional when only one account exists"`
	Realm     string `json:"realm,omitempty" jsonschema:"Realm folder; optional when the character name is unique"`
	Character string `json:"character,omitempty" jsonschema:"Character for character-specific bindings; leave empty for the account-wide set"`
}

type bindingsOut struct {
	Flavor     string        `json:"flavor"`
	Scope      string        `json:"scope"`
	File       string        `json:"file"`
	BindingSet string        `json:"binding_set,omitempty"`
	Count      int           `json:"count"`
	Bindings   []wow.Binding `json:"bindings"`
}

func (s *Server) keybindings(_ context.Context, _ *mcp.CallToolRequest, in bindingsIn) (*mcp.CallToolResult, bindingsOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, bindingsOut{}, err
	}
	sc, err := resolveScope(fl, in.Account, in.Realm, in.Character)
	if err != nil {
		return nil, bindingsOut{}, err
	}
	file := filepath.Join(sc.Dir, "bindings-cache.wtf")
	b, err := wow.ParseBindings(file)
	if err != nil {
		return nil, bindingsOut{}, err
	}
	if b.Bindings == nil {
		b.Bindings = []wow.Binding{}
	}
	return nil, bindingsOut{Flavor: fl.Name, Scope: sc.Label, File: file, BindingSet: b.BindingSet, Count: len(b.Bindings), Bindings: b.Bindings}, nil
}

// ------------------------------------------------------------ wow_macros

type macrosOut struct {
	Flavor string      `json:"flavor"`
	Scope  string      `json:"scope"`
	File   string      `json:"file"`
	Count  int         `json:"count"`
	Macros []wow.Macro `json:"macros"`
}

func (s *Server) macros(_ context.Context, _ *mcp.CallToolRequest, in bindingsIn) (*mcp.CallToolResult, macrosOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, macrosOut{}, err
	}
	sc, err := resolveScope(fl, in.Account, in.Realm, in.Character)
	if err != nil {
		return nil, macrosOut{}, err
	}
	file := filepath.Join(sc.Dir, "macros-cache.txt")
	m, err := wow.ParseMacros(file)
	if err != nil {
		return nil, macrosOut{}, err
	}
	if m == nil {
		m = []wow.Macro{}
	}
	return nil, macrosOut{Flavor: fl.Name, Scope: sc.Label, File: file, Count: len(m), Macros: m}, nil
}
