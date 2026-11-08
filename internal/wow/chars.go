package wow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Character is one WTF/Account/<account>/<realm>/<name> folder.
type Character struct {
	Account        string    `json:"account"`
	Realm          string    `json:"realm"`
	Name           string    `json:"name"`
	Dir            string    `json:"dir"`
	HasAddonsTxt   bool      `json:"has_addons_txt"`
	SavedVariables int       `json:"saved_variables"` // number of per-character SavedVariables files
	Modified       time.Time `json:"modified"`        // newest change under the folder's top level
}

// Realm groups the characters of one account on one realm.
type Realm struct {
	Name       string      `json:"name"`
	Characters []Character `json:"characters"`
}

// Account is one WTF/Account/<account> folder.
type Account struct {
	Name           string  `json:"name"`
	Dir            string  `json:"dir"`
	SavedVariables int     `json:"saved_variables"` // account-wide SavedVariables files
	Realms         []Realm `json:"realms"`
}

// AddonsTxtPath is the character's addon enable list.
func (c *Character) AddonsTxtPath() string { return filepath.Join(c.Dir, "AddOns.txt") }

// ConfigCachePath is the character's CVar overrides.
func (c *Character) ConfigCachePath() string { return filepath.Join(c.Dir, "config-cache.wtf") }

// SavedVariablesDir is the character's SavedVariables folder.
func (c *Character) SavedVariablesDir() string { return filepath.Join(c.Dir, "SavedVariables") }

// SavedVariablesDir is the account-wide SavedVariables folder.
func (a *Account) SavedVariablesDir() string { return filepath.Join(a.Dir, "SavedVariables") }

func countLua(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".lua") {
			n++
		}
	}
	return n
}

func newestMod(dir string) time.Time {
	var t time.Time
	entries, err := os.ReadDir(dir)
	if err != nil {
		return t
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().After(t) {
			t = info.ModTime()
		}
	}
	return t
}

// Accounts enumerates WTF/Account. Inside an account, the folder named
// SavedVariables holds the account-wide variables and every other folder is
// a realm. The client also keeps a SavedVariables folder directly under
// WTF/Account (shared, not an account), which is skipped.
func (f *Flavor) Accounts() ([]Account, error) {
	entries, err := os.ReadDir(f.AccountsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Account
	for _, ae := range entries {
		if !ae.IsDir() || strings.EqualFold(ae.Name(), "SavedVariables") {
			continue
		}
		acc := Account{Name: ae.Name(), Dir: filepath.Join(f.AccountsDir(), ae.Name()), Realms: []Realm{}}
		acc.SavedVariables = countLua(acc.SavedVariablesDir())
		realms, err := os.ReadDir(acc.Dir)
		if err != nil {
			return nil, err
		}
		for _, re := range realms {
			if !re.IsDir() || re.Name() == "SavedVariables" {
				continue
			}
			realm := Realm{Name: re.Name(), Characters: []Character{}}
			chars, err := os.ReadDir(filepath.Join(acc.Dir, re.Name()))
			if err != nil {
				return nil, err
			}
			for _, ce := range chars {
				if !ce.IsDir() {
					continue
				}
				c := Character{Account: acc.Name, Realm: realm.Name, Name: ce.Name(), Dir: filepath.Join(acc.Dir, re.Name(), ce.Name())}
				c.HasAddonsTxt = isFile(c.AddonsTxtPath())
				c.SavedVariables = countLua(c.SavedVariablesDir())
				c.Modified = newestMod(c.Dir)
				realm.Characters = append(realm.Characters, c)
			}
			if len(realm.Characters) > 0 {
				acc.Realms = append(acc.Realms, realm)
			}
		}
		out = append(out, acc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AllCharacters flattens Accounts.
func (f *Flavor) AllCharacters() ([]Character, error) {
	accs, err := f.Accounts()
	if err != nil {
		return nil, err
	}
	var out []Character
	for _, a := range accs {
		for _, r := range a.Realms {
			out = append(out, r.Characters...)
		}
	}
	return out, nil
}

// ResolveAccount finds an account by name (case insensitive). An empty name
// is accepted when exactly one account exists.
func (f *Flavor) ResolveAccount(name string) (*Account, error) {
	accs, err := f.Accounts()
	if err != nil {
		return nil, err
	}
	if len(accs) == 0 {
		return nil, fmt.Errorf("no accounts under %s (the client has never logged in on this flavor)", f.AccountsDir())
	}
	if name == "" {
		if len(accs) == 1 {
			return &accs[0], nil
		}
		return nil, fmt.Errorf("several accounts exist, pass one of: %s", strings.Join(accountNames(accs), ", "))
	}
	if err := ValidateName("account", name); err != nil {
		return nil, err
	}
	for i := range accs {
		if strings.EqualFold(accs[i].Name, name) {
			return &accs[i], nil
		}
	}
	return nil, fmt.Errorf("no account %q; have: %s", name, strings.Join(accountNames(accs), ", "))
}

func accountNames(accs []Account) []string {
	out := make([]string, len(accs))
	for i, a := range accs {
		out[i] = a.Name
	}
	return out
}

// ResolveCharacter finds a character. account and realm may be empty when the
// character name is unique across the flavor; ambiguity is an error that
// lists the matches.
func (f *Flavor) ResolveCharacter(account, realm, name string) (*Character, error) {
	if err := ValidateName("character", name); err != nil {
		return nil, err
	}
	if account != "" {
		if err := ValidateName("account", account); err != nil {
			return nil, err
		}
	}
	if realm != "" {
		if err := ValidateName("realm", realm); err != nil {
			return nil, err
		}
	}
	all, err := f.AllCharacters()
	if err != nil {
		return nil, err
	}
	var matches []Character
	for _, c := range all {
		if !strings.EqualFold(c.Name, name) {
			continue
		}
		if account != "" && !strings.EqualFold(c.Account, account) {
			continue
		}
		if realm != "" && !strings.EqualFold(c.Realm, realm) {
			continue
		}
		matches = append(matches, c)
	}
	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		return nil, fmt.Errorf("no character %q (account %q, realm %q) under %s", name, account, realm, f.AccountsDir())
	}
	var desc []string
	for _, m := range matches {
		desc = append(desc, m.Account+"/"+m.Realm+"/"+m.Name)
	}
	return nil, fmt.Errorf("character %q is ambiguous, pass account and realm: %s", name, strings.Join(desc, ", "))
}
