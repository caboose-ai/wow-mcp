package wow

import (
	"os"
	"strings"
)

// AddonsTxt is a character's WTF/.../AddOns.txt: one "Name: enabled|disabled"
// line per addon the client knew about at logout. A missing file means every
// addon is in its TOC DefaultState.
type AddonsTxt struct {
	Path    string
	Existed bool
	lines   []addonsLine
	eol     string
}

type addonsLine struct {
	raw     string // kept verbatim for lines that are not "Name: state"
	name    string
	enabled bool
}

// LoadAddonsTxt reads path; a missing file yields an empty, writable AddonsTxt.
func LoadAddonsTxt(path string) (*AddonsTxt, error) {
	a := &AddonsTxt{Path: path, eol: "\n"}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return a, nil
		}
		return nil, err
	}
	a.Existed = true
	a.eol = DetectEOL(b)
	for _, ln := range SplitLines(b) {
		i := strings.LastIndex(ln, ":")
		if i <= 0 {
			a.lines = append(a.lines, addonsLine{raw: ln})
			continue
		}
		name := strings.TrimSpace(ln[:i])
		state := strings.ToLower(strings.TrimSpace(ln[i+1:]))
		if name == "" || (state != "enabled" && state != "disabled") {
			a.lines = append(a.lines, addonsLine{raw: ln})
			continue
		}
		a.lines = append(a.lines, addonsLine{name: name, enabled: state == "enabled"})
	}
	return a, nil
}

// Get reports the recorded state of name and whether it is listed at all.
func (a *AddonsTxt) Get(name string) (enabled, listed bool) {
	for _, l := range a.lines {
		if l.name != "" && strings.EqualFold(l.name, name) {
			return l.enabled, true
		}
	}
	return false, false
}

// Names returns the listed addon names in file order.
func (a *AddonsTxt) Names() []string {
	var out []string
	for _, l := range a.lines {
		if l.name != "" {
			out = append(out, l.name)
		}
	}
	return out
}

// Set records the state of name, appending when it is not listed yet. It
// returns whether the file content changed.
func (a *AddonsTxt) Set(name string, enabled bool) bool {
	for i, l := range a.lines {
		if l.name != "" && strings.EqualFold(l.name, name) {
			if l.enabled == enabled {
				return false
			}
			a.lines[i].enabled = enabled
			return true
		}
	}
	a.lines = append(a.lines, addonsLine{name: name, enabled: enabled})
	return true
}

// Bytes renders the file with its original line endings.
func (a *AddonsTxt) Bytes() []byte {
	var sb strings.Builder
	for _, l := range a.lines {
		if l.name == "" {
			sb.WriteString(l.raw)
		} else {
			sb.WriteString(l.name)
			if l.enabled {
				sb.WriteString(": enabled")
			} else {
				sb.WriteString(": disabled")
			}
		}
		sb.WriteString(a.eol)
	}
	return []byte(sb.String())
}

// Save writes the file through WriteFileSafely and returns the backup path.
func (a *AddonsTxt) Save() (string, error) {
	backup, err := WriteFileSafely(a.Path, a.Bytes())
	if err == nil {
		a.Existed = true
	}
	return backup, err
}
