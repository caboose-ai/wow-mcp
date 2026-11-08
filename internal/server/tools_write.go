package server

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/caboose-ai/wow-mcp/internal/wow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ------------------------------------------------------------ wow_addon_set_enabled

type setEnabledIn struct {
	Flavor        string   `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Addons        []string `json:"addons" jsonschema:"Addon folder names to change. Must be installed, or start with Blizzard_"`
	Enabled       bool     `json:"enabled" jsonschema:"true to enable, false to disable"`
	Account       string   `json:"account,omitempty" jsonschema:"Account folder; optional when unambiguous. With all_characters it limits the change to that account"`
	Realm         string   `json:"realm,omitempty" jsonschema:"Realm folder; optional when the character name is unique"`
	Character     string   `json:"character,omitempty" jsonschema:"Character whose AddOns.txt to edit. Required unless all_characters is true"`
	AllCharacters bool     `json:"all_characters,omitempty" jsonschema:"Apply to every character on the flavor (or on the given account)"`
	Force         bool     `json:"force,omitempty" jsonschema:"Write even if a WoW client appears to be running (the client will overwrite the file on logout)"`
}

type changeOut struct {
	File   string `json:"file"`
	Addon  string `json:"addon"`
	Before string `json:"before"` // "enabled", "disabled" or "unlisted"
	After  string `json:"after"`
}

type setEnabledOut struct {
	Changes    []changeOut `json:"changes"`
	Unchanged  []string    `json:"unchanged,omitempty"`
	Backups    []string    `json:"backups,omitempty"`
	Characters int         `json:"characters"`
}

func stateWord(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func (s *Server) setEnabled(_ context.Context, _ *mcp.CallToolRequest, in setEnabledIn) (*mcp.CallToolResult, setEnabledOut, error) {
	if len(in.Addons) == 0 {
		return nil, setEnabledOut{}, fmt.Errorf("addons is empty")
	}
	if err := s.guardWrite(in.Force); err != nil {
		return nil, setEnabledOut{}, err
	}
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, setEnabledOut{}, err
	}
	names := make([]string, 0, len(in.Addons))
	for _, n := range in.Addons {
		canon, err := requireAddonKnown(fl, n)
		if err != nil {
			return nil, setEnabledOut{}, err
		}
		names = append(names, canon)
	}
	var targets []wow.Character
	switch {
	case in.AllCharacters:
		all, err := fl.AllCharacters()
		if err != nil {
			return nil, setEnabledOut{}, err
		}
		for _, c := range all {
			if in.Account == "" || strings.EqualFold(c.Account, in.Account) {
				targets = append(targets, c)
			}
		}
		if len(targets) == 0 {
			return nil, setEnabledOut{}, fmt.Errorf("no characters found under %s", fl.AccountsDir())
		}
	case in.Character != "":
		ch, err := fl.ResolveCharacter(in.Account, in.Realm, in.Character)
		if err != nil {
			return nil, setEnabledOut{}, err
		}
		targets = []wow.Character{*ch}
	default:
		return nil, setEnabledOut{}, fmt.Errorf("pass character (with account/realm when ambiguous) or all_characters=true")
	}
	out := setEnabledOut{Changes: []changeOut{}, Characters: len(targets)}
	for _, c := range targets {
		txt, err := wow.LoadAddonsTxt(c.AddonsTxtPath())
		if err != nil {
			return nil, out, err
		}
		dirty := false
		for _, n := range names {
			wasEnabled, listed := txt.Get(n)
			before := "unlisted"
			if listed {
				before = stateWord(wasEnabled)
			}
			if txt.Set(n, in.Enabled) {
				dirty = true
				out.Changes = append(out.Changes, changeOut{File: c.AddonsTxtPath(), Addon: n, Before: before, After: stateWord(in.Enabled)})
			} else {
				out.Unchanged = append(out.Unchanged, c.Realm+"/"+c.Name+": "+n)
			}
		}
		if dirty {
			backup, err := txt.Save()
			if err != nil {
				return nil, out, fmt.Errorf("write %s: %w", c.AddonsTxtPath(), err)
			}
			if backup != "" {
				out.Backups = append(out.Backups, backup)
			}
		}
	}
	return nil, out, nil
}

// ------------------------------------------------------------ wow_cvar_set

type cvarSetIn struct {
	Flavor    string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Name      string `json:"name" jsonschema:"CVar name, e.g. gxMaximize"`
	Value     string `json:"value,omitempty" jsonschema:"New value, always a string as the client stores it, e.g. \"1\" or \"0.5\""`
	Unset     bool   `json:"unset,omitempty" jsonschema:"Remove the CVar line instead of setting it (the client then uses its default)"`
	Account   string `json:"account,omitempty" jsonschema:"Account folder; only with character"`
	Realm     string `json:"realm,omitempty" jsonschema:"Realm folder; only with character"`
	Character string `json:"character,omitempty" jsonschema:"Edit the character's config-cache.wtf instead of the global Config.wtf"`
	Force     bool   `json:"force,omitempty" jsonschema:"Write even if a WoW client appears to be running"`
}

type cvarSetOut struct {
	File    string  `json:"file"`
	Scope   string  `json:"scope"`
	Name    string  `json:"name"`
	Before  *string `json:"before"` // null when the CVar was not present
	After   *string `json:"after"`  // null after unset
	Changed bool    `json:"changed"`
	Backup  string  `json:"backup,omitempty"`
}

func (s *Server) cvarSet(_ context.Context, _ *mcp.CallToolRequest, in cvarSetIn) (*mcp.CallToolResult, cvarSetOut, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || strings.ContainsAny(name, " \t\"\r\n") {
		return nil, cvarSetOut{}, fmt.Errorf("invalid CVar name %q", in.Name)
	}
	if strings.ContainsAny(in.Value, "\"\r\n") {
		return nil, cvarSetOut{}, fmt.Errorf("CVar values cannot contain quotes or newlines")
	}
	if err := s.guardWrite(in.Force); err != nil {
		return nil, cvarSetOut{}, err
	}
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, cvarSetOut{}, err
	}
	file, label, err := s.cvarFile(fl, in.Account, in.Realm, in.Character)
	if err != nil {
		return nil, cvarSetOut{}, err
	}
	cfg, err := wow.LoadWTFConfig(file)
	if err != nil {
		return nil, cvarSetOut{}, err
	}
	out := cvarSetOut{File: file, Scope: label, Name: name}
	if in.Unset {
		prev, existed := cfg.Unset(name)
		if !existed {
			return nil, out, nil
		}
		out.Before, out.Changed = &prev, true
	} else {
		prev, existed := cfg.Set(name, in.Value)
		if existed {
			out.Before = &prev
			if prev == in.Value {
				out.After = &prev
				return nil, out, nil
			}
		}
		v := in.Value
		out.After, out.Changed = &v, true
	}
	backup, err := cfg.Save()
	if err != nil {
		return nil, out, fmt.Errorf("write %s: %w", file, err)
	}
	out.Backup = backup
	return nil, out, nil
}

// ------------------------------------------------------------ wow_saved_variable_set

type svSetIn struct {
	Flavor    string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Addon     string `json:"addon" jsonschema:"Addon folder name; the file is SavedVariables/<addon>.lua"`
	Account   string `json:"account,omitempty" jsonschema:"Account folder; optional when only one account exists"`
	Realm     string `json:"realm,omitempty" jsonschema:"Realm folder; optional when the character name is unique"`
	Character string `json:"character,omitempty" jsonschema:"Character for the per-character file; leave empty for the account-wide file"`
	Path      string `json:"path" jsonschema:"Dotted path, first segment is the variable name, e.g. MyAddonDB.profiles.Default.scale. Use a JSON array of segments when a key contains a dot. Missing tables are created"`
	ValueJSON string `json:"value_json,omitempty" jsonschema:"New value as JSON: 1.5, true, \"text\", null, [..] or {..}. Object keys that look like integers become Lua number keys"`
	Delete    bool   `json:"delete,omitempty" jsonschema:"Remove the entry at path instead of setting it"`
	Force     bool   `json:"force,omitempty" jsonschema:"Write even if a WoW client appears to be running"`
}

type svSetOut struct {
	File    string `json:"file"`
	Scope   string `json:"scope"`
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Before  any    `json:"before"`
	After   any    `json:"after"`
	Created bool   `json:"created_file,omitempty"`
	Backup  string `json:"backup,omitempty"`
}

func (s *Server) savedVariableSet(_ context.Context, _ *mcp.CallToolRequest, in svSetIn) (*mcp.CallToolResult, svSetOut, error) {
	path, err := wow.ParsePath(in.Path)
	if err != nil {
		return nil, svSetOut{}, err
	}
	if !in.Delete && strings.TrimSpace(in.ValueJSON) == "" {
		return nil, svSetOut{}, fmt.Errorf("value_json is required unless delete=true")
	}
	if err := s.guardWrite(in.Force); err != nil {
		return nil, svSetOut{}, err
	}
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, svSetOut{}, err
	}
	addon, err := requireAddonKnown(fl, in.Addon)
	if err != nil {
		return nil, svSetOut{}, err
	}
	sc, err := resolveScope(fl, in.Account, in.Realm, in.Character)
	if err != nil {
		return nil, svSetOut{}, err
	}
	file := sc.savedVariablesFile(addon)
	var sv *wow.SavedVariables
	created := false
	if _, statErr := os.Stat(file); statErr == nil {
		sv, err = wow.LoadSavedVariables(file)
		if err != nil {
			return nil, svSetOut{}, err
		}
	} else if os.IsNotExist(statErr) {
		if in.Delete {
			return nil, svSetOut{}, fmt.Errorf("no SavedVariables file %s", file)
		}
		sv = &wow.SavedVariables{Path: file}
		created = true
	} else {
		return nil, svSetOut{}, statErr
	}
	out := svSetOut{File: file, Scope: sc.Label, Path: in.Path, Created: created}
	if in.Delete {
		prev, existed, err := sv.DeletePath(path)
		if err != nil {
			return nil, svSetOut{}, err
		}
		if !existed {
			return nil, out, nil
		}
		out.Existed, out.Before = true, wow.LuaToJSON(prev, -1)
	} else {
		v, err := wow.JSONToLua([]byte(in.ValueJSON))
		if err != nil {
			return nil, svSetOut{}, err
		}
		prev, existed, err := sv.SetPath(path, v)
		if err != nil {
			return nil, svSetOut{}, err
		}
		out.Existed = existed
		if existed {
			out.Before = wow.LuaToJSON(prev, -1)
		}
		out.After = wow.LuaToJSON(v, -1)
	}
	backup, err := sv.Save()
	if err != nil {
		return nil, out, fmt.Errorf("write %s: %w", file, err)
	}
	out.Backup = backup
	return nil, out, nil
}
