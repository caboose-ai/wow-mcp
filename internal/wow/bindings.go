package wow

import (
	"os"
	"strings"
)

// Binding is one line of bindings-cache.wtf.
type Binding struct {
	Key     string `json:"key"`
	Command string `json:"command"`
}

// Bindings is a parsed bindings-cache.wtf.
type Bindings struct {
	Path       string    `json:"file"`
	BindingSet string    `json:"binding_set,omitempty"` // "bindingset N" header value when present
	Bindings   []Binding `json:"bindings"`
}

// ParseBindings reads a bindings-cache.wtf: "bindingset N" then "bind KEY COMMAND" lines.
func ParseBindings(path string) (*Bindings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := &Bindings{Path: path}
	for _, ln := range SplitLines(b) {
		f := strings.Fields(ln)
		if len(f) == 0 {
			continue
		}
		switch strings.ToLower(f[0]) {
		case "bindingset":
			if len(f) > 1 {
				out.BindingSet = f[1]
			}
		case "bind":
			if len(f) >= 3 {
				out.Bindings = append(out.Bindings, Binding{Key: f[1], Command: strings.Join(f[2:], " ")})
			}
		}
	}
	return out, nil
}

// Macro is one entry of macros-cache.txt.
type Macro struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
	Body string `json:"body"`
}

// ParseMacros reads a macros-cache.txt: blocks of
//
//	MACRO <id> "<name>" <icon>
//	<body lines>
//	END
func ParseMacros(path string) ([]Macro, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Macro
	var cur *Macro
	var body []string
	for _, ln := range SplitLines(b) {
		switch {
		case strings.HasPrefix(ln, "MACRO "):
			if cur != nil {
				cur.Body = strings.Join(body, "\n")
				out = append(out, *cur)
			}
			cur = &Macro{}
			body = body[:0]
			rest := strings.TrimSpace(ln[len("MACRO "):])
			if i := strings.Index(rest, " "); i > 0 {
				cur.ID = rest[:i]
				rest = strings.TrimSpace(rest[i+1:])
			} else {
				cur.ID = rest
				rest = ""
			}
			if strings.HasPrefix(rest, `"`) {
				if j := strings.LastIndex(rest, `"`); j > 0 {
					cur.Name = rest[1:j]
					cur.Icon = strings.TrimSpace(rest[j+1:])
				}
			} else if f := strings.Fields(rest); len(f) > 0 {
				cur.Name = f[0]
				cur.Icon = strings.Join(f[1:], " ")
			}
		case ln == "END":
			if cur != nil {
				cur.Body = strings.Join(body, "\n")
				out = append(out, *cur)
				cur = nil
				body = body[:0]
			}
		default:
			if cur != nil {
				body = append(body, ln)
			}
		}
	}
	if cur != nil {
		cur.Body = strings.Join(body, "\n")
		out = append(out, *cur)
	}
	return out, nil
}
