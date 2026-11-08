package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caboose-ai/wow-mcp/internal/wow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func jsonSize(v any) (int, error) {
	b, err := json.Marshal(v)
	return len(b), err
}

// ------------------------------------------------------------ wow_addon_file

type addonFileIn struct {
	Flavor   string `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Addon    string `json:"addon" jsonschema:"Addon folder name"`
	Path     string `json:"path,omitempty" jsonschema:"File path relative to the addon folder, e.g. core.lua or locales/enUS.lua. Empty or a folder lists entries"`
	Offset   int    `json:"offset,omitempty" jsonschema:"Byte offset to start reading from"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"Bytes to return (default 65536)"`
}

type dirEntryOut struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type addonFileOut struct {
	Addon      string        `json:"addon"`
	File       string        `json:"file"`
	IsDir      bool          `json:"is_dir"`
	Entries    []dirEntryOut `json:"entries,omitempty"`
	Size       int64         `json:"size,omitempty"`
	Offset     int           `json:"offset,omitempty"`
	Content    string        `json:"content,omitempty"`
	Truncated  bool          `json:"truncated,omitempty"`
	NextOffset int           `json:"next_offset,omitempty"`
}

// insideAddon joins rel onto the addon dir and refuses anything that escapes it.
func insideAddon(a *wow.Addon, rel string) (string, error) {
	if rel == "" || rel == "." {
		return a.Dir, nil
	}
	if err := wow.ValidateRelPath("path", rel); err != nil {
		return "", err
	}
	p := filepath.Clean(filepath.Join(a.Dir, filepath.FromSlash(rel)))
	if p != a.Dir && !strings.HasPrefix(p, a.Dir+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the addon folder", rel)
	}
	return p, nil
}

func (s *Server) addonFile(_ context.Context, _ *mcp.CallToolRequest, in addonFileIn) (*mcp.CallToolResult, addonFileOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, addonFileOut{}, err
	}
	a, err := wow.FindAddon(fl, in.Addon)
	if err != nil {
		return nil, addonFileOut{}, err
	}
	p, err := insideAddon(a, in.Path)
	if err != nil {
		return nil, addonFileOut{}, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, addonFileOut{}, err
	}
	out := addonFileOut{Addon: a.Name, File: p}
	if st.IsDir() {
		out.IsDir = true
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, addonFileOut{}, err
		}
		out.Entries = []dirEntryOut{}
		for _, e := range entries {
			de := dirEntryOut{Name: e.Name(), IsDir: e.IsDir()}
			if info, err := e.Info(); err == nil && !e.IsDir() {
				de.Size = info.Size()
			}
			out.Entries = append(out.Entries, de)
		}
		return nil, out, nil
	}
	maxBytes := in.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 65536
	}
	if in.Offset < 0 || int64(in.Offset) > st.Size() {
		return nil, addonFileOut{}, fmt.Errorf("offset %d is outside the file (size %d)", in.Offset, st.Size())
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, addonFileOut{}, err
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, err := f.ReadAt(buf, int64(in.Offset))
	if err != nil && n == 0 && int64(in.Offset) < st.Size() {
		return nil, addonFileOut{}, err
	}
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 {
		return nil, addonFileOut{}, fmt.Errorf("%s looks binary (contains NUL bytes); only text files are returned", in.Path)
	}
	out.Size, out.Offset, out.Content = st.Size(), in.Offset, string(buf)
	if int64(in.Offset+n) < st.Size() {
		out.Truncated = true
		out.NextOffset = in.Offset + n
	}
	return nil, out, nil
}

// ------------------------------------------------------------ wow_addon_grep

type addonGrepIn struct {
	Flavor     string   `json:"flavor,omitempty" jsonschema:"Client flavor; defaults to the server default"`
	Pattern    string   `json:"pattern" jsonschema:"Go/RE2 regular expression matched against each line"`
	Addon      string   `json:"addon,omitempty" jsonschema:"Restrict the search to one addon folder"`
	Extensions []string `json:"extensions,omitempty" jsonschema:"File extensions to search (default lua, toc, xml)"`
	IgnoreCase bool     `json:"ignore_case,omitempty" jsonschema:"Case-insensitive match"`
	MaxResults int      `json:"max_results,omitempty" jsonschema:"Stop after this many matches (default 100)"`
}

type grepMatchOut struct {
	Addon string `json:"addon"`
	File  string `json:"file"` // relative to Interface/AddOns
	Line  int    `json:"line"`
	Text  string `json:"text"`
}

type addonGrepOut struct {
	Flavor        string         `json:"flavor"`
	FilesSearched int            `json:"files_searched"`
	Count         int            `json:"count"`
	Truncated     bool           `json:"truncated,omitempty"`
	Matches       []grepMatchOut `json:"matches"`
}

const grepMaxFileBytes = 8 << 20

func (s *Server) addonGrep(_ context.Context, _ *mcp.CallToolRequest, in addonGrepIn) (*mcp.CallToolResult, addonGrepOut, error) {
	_, fl, err := s.flavor(in.Flavor)
	if err != nil {
		return nil, addonGrepOut{}, err
	}
	pat := in.Pattern
	if in.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, addonGrepOut{}, fmt.Errorf("bad pattern: %w", err)
	}
	exts := map[string]bool{}
	if len(in.Extensions) == 0 {
		in.Extensions = []string{"lua", "toc", "xml"}
	}
	for _, e := range in.Extensions {
		exts["."+strings.ToLower(strings.TrimPrefix(e, "."))] = true
	}
	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}
	var addons []*wow.Addon
	if in.Addon != "" {
		a, err := wow.FindAddon(fl, in.Addon)
		if err != nil {
			return nil, addonGrepOut{}, err
		}
		addons = []*wow.Addon{a}
	} else {
		addons, err = wow.ListAddons(fl)
		if err != nil {
			return nil, addonGrepOut{}, err
		}
	}
	out := addonGrepOut{Flavor: fl.Name, Matches: []grepMatchOut{}}
	root := fl.AddOnsDir()
	for _, a := range addons {
		if out.Truncated {
			break
		}
		_ = filepath.WalkDir(a.Dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || out.Truncated {
				return nil
			}
			if d.IsDir() || !exts[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() > grepMaxFileBytes {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			out.FilesSearched++
			rel, _ := filepath.Rel(root, p)
			for i, ln := range wow.SplitLines(b) {
				if re.MatchString(ln) {
					if len(ln) > 400 {
						ln = ln[:400] + "..."
					}
					out.Matches = append(out.Matches, grepMatchOut{Addon: a.Name, File: filepath.ToSlash(rel), Line: i + 1, Text: ln})
					if len(out.Matches) >= maxResults {
						out.Truncated = true
						return filepath.SkipAll
					}
				}
			}
			return nil
		})
	}
	out.Count = len(out.Matches)
	return nil, out, nil
}
