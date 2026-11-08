package wow

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Some clients (WoW Forever) name realm folders by numeric ID, which means
// nothing to a person. Addons, however, key their per-character data by
// "<first name> - <realm name>" as the game's API reports it, so the
// account-wide SavedVariables reveal the human names. RealmNames collects
// them: for every profile-key line whose first name belongs to a character
// of this account, the realm part is credited to that character's realm
// folder, unless it simply equals one of the realm folders (then it is not
// a label but the folder's own name, as on retail).

// profileKeyLine matches `["Furda - CAMWOW US2 Realm 5 PvE"] =` style keys.
var profileKeyLine = regexp.MustCompile(`^\s*\["([^"\\]+?) - ([^"\\]+?)"\]\s*=`)

const (
	realmScanMaxFileBytes = 16 << 20
	realmScanMaxLine      = 4 << 20
	realmScanMaxNames     = 4
)

type realmNamesEntry struct {
	stamp string
	names map[string][]string
}

var realmNamesCache sync.Map // account dir -> realmNamesEntry

// FirstName returns the part of a character folder name before the first
// hyphen: Forever characters are stored as "First-Surname" and the game's
// API reports the first name alone. Names without a hyphen are returned as is.
func FirstName(name string) string {
	if i := strings.Index(name, "-"); i > 0 {
		return name[:i]
	}
	return name
}

// RealmNames maps each realm folder of the account to the human realm names
// found in its SavedVariables, most frequent first. Folders for which no
// name was found are absent. The result is cached per account until any
// SavedVariables file changes.
func (a *Account) RealmNames() map[string][]string {
	stamp := a.savedVariablesStamp()
	if v, ok := realmNamesCache.Load(a.Dir); ok {
		if e := v.(realmNamesEntry); e.stamp == stamp {
			return e.names
		}
	}
	names := a.scanRealmNames()
	realmNamesCache.Store(a.Dir, realmNamesEntry{stamp: stamp, names: names})
	return names
}

func (a *Account) savedVariablesStamp() string {
	entries, err := os.ReadDir(a.SavedVariablesDir())
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			fmt.Fprintf(&sb, "%s|%d|%d;", e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	return sb.String()
}

func (a *Account) scanRealmNames() map[string][]string {
	// first name (lower) -> realm folders holding a character with that first name
	byFirst := map[string][]string{}
	folders := map[string]bool{}
	for _, r := range a.Realms {
		folders[strings.ToLower(r.Name)] = true
		for _, c := range r.Characters {
			k := strings.ToLower(FirstName(c.Name))
			byFirst[k] = append(byFirst[k], r.Name)
		}
	}
	if len(byFirst) == 0 {
		return nil
	}
	counts := map[string]map[string]int{} // realm folder -> realm name -> files mentioning it
	entries, err := os.ReadDir(a.SavedVariablesDir())
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".lua") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > realmScanMaxFileBytes {
			continue
		}
		seen := map[string]bool{} // folder|name, once per file
		f, err := os.Open(filepath.Join(a.SavedVariablesDir(), e.Name()))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), realmScanMaxLine)
		for sc.Scan() {
			m := profileKeyLine.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			first, realm := m[1], strings.TrimSpace(m[2])
			if realm == "" || folders[strings.ToLower(realm)] {
				continue
			}
			for _, folder := range byFirst[strings.ToLower(first)] {
				key := folder + "|" + realm
				if seen[key] {
					continue
				}
				seen[key] = true
				if counts[folder] == nil {
					counts[folder] = map[string]int{}
				}
				counts[folder][realm]++
			}
		}
		_ = f.Close()
	}
	if len(counts) == 0 {
		return nil
	}
	out := make(map[string][]string, len(counts))
	for folder, byName := range counts {
		names := make([]string, 0, len(byName))
		for n := range byName {
			names = append(names, n)
		}
		sort.Slice(names, func(i, j int) bool {
			if byName[names[i]] != byName[names[j]] {
				return byName[names[i]] > byName[names[j]]
			}
			return names[i] < names[j]
		})
		if len(names) > realmScanMaxNames {
			names = names[:realmScanMaxNames]
		}
		out[folder] = names
	}
	return out
}

// RealmLabel returns the best human name for a realm folder, or "" when the
// folder name is all there is.
func (a *Account) RealmLabel(folder string) string {
	if names := a.RealmNames()[folder]; len(names) > 0 {
		return names[0]
	}
	return ""
}

// realmFolderFor turns a realm given by folder name or by any human name
// into the folder name. ok is false when nothing matches.
func (a *Account) realmFolderFor(realm string) (string, bool) {
	for _, r := range a.Realms {
		if strings.EqualFold(r.Name, realm) {
			return r.Name, true
		}
	}
	for folder, names := range a.RealmNames() {
		for _, n := range names {
			if strings.EqualFold(n, realm) {
				return folder, true
			}
		}
	}
	return "", false
}
