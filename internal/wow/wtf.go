package wow

import (
	"os"
	"regexp"
	"strings"
)

// CVar is one console variable as stored in Config.wtf or config-cache.wtf.
type CVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// WTFConfig is a parsed Config.wtf (global CVars) or a character's
// config-cache.wtf. Lines are `SET name "value"`; anything else is kept verbatim.
type WTFConfig struct {
	Path    string
	Existed bool
	lines   []wtfLine
	eol     string
}

type wtfLine struct {
	raw   string
	name  string
	value string
	isSet bool
}

var setLine = regexp.MustCompile(`^\s*SET\s+(\S+)\s+"(.*)"\s*$`)

// LoadWTFConfig reads path; a missing file yields an empty, writable config.
func LoadWTFConfig(path string) (*WTFConfig, error) {
	c := &WTFConfig{Path: path, eol: "\n"}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	c.Existed = true
	c.eol = DetectEOL(b)
	for _, ln := range SplitLines(b) {
		if m := setLine.FindStringSubmatch(ln); m != nil {
			c.lines = append(c.lines, wtfLine{name: m[1], value: m[2], isSet: true})
		} else {
			c.lines = append(c.lines, wtfLine{raw: ln})
		}
	}
	return c, nil
}

// All returns every CVar in file order.
func (c *WTFConfig) All() []CVar {
	var out []CVar
	for _, l := range c.lines {
		if l.isSet {
			out = append(out, CVar{Name: l.name, Value: l.value})
		}
	}
	return out
}

// Get looks a CVar up by name, case insensitive.
func (c *WTFConfig) Get(name string) (value string, ok bool) {
	for _, l := range c.lines {
		if l.isSet && strings.EqualFold(l.name, name) {
			return l.value, true
		}
	}
	return "", false
}

// Set stores value under name, replacing an existing line in place or
// appending a new one. It returns the previous value and whether it existed.
func (c *WTFConfig) Set(name, value string) (previous string, existed bool) {
	for i, l := range c.lines {
		if l.isSet && strings.EqualFold(l.name, name) {
			c.lines[i].value = value
			return l.value, true
		}
	}
	c.lines = append(c.lines, wtfLine{name: name, value: value, isSet: true})
	return "", false
}

// Unset removes name and reports whether it was present.
func (c *WTFConfig) Unset(name string) (previous string, existed bool) {
	for i, l := range c.lines {
		if l.isSet && strings.EqualFold(l.name, name) {
			c.lines = append(c.lines[:i], c.lines[i+1:]...)
			return l.value, true
		}
	}
	return "", false
}

// Bytes renders the file with its original line endings. Quotes inside a
// value are written as-is, which is what the client does too.
func (c *WTFConfig) Bytes() []byte {
	var sb strings.Builder
	for _, l := range c.lines {
		if l.isSet {
			sb.WriteString("SET ")
			sb.WriteString(l.name)
			sb.WriteString(` "`)
			sb.WriteString(l.value)
			sb.WriteString(`"`)
		} else {
			sb.WriteString(l.raw)
		}
		sb.WriteString(c.eol)
	}
	return []byte(sb.String())
}

// Save writes the file through WriteFileSafely and returns the backup path.
func (c *WTFConfig) Save() (string, error) {
	backup, err := WriteFileSafely(c.Path, c.Bytes())
	if err == nil {
		c.Existed = true
	}
	return backup, err
}
