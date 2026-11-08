// Package server exposes the wow package over the Model Context Protocol.
package server

import (
	"errors"
	"fmt"
	"sync"

	"github.com/caboose-ai/wow-mcp/internal/wow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configure a Server.
type Options struct {
	Root           string // explicit WoW root; "" means auto-detect
	DefaultFlavor  string // flavor used when a call does not name one
	ReadOnly       bool   // refuse every write
	AllowRunning   bool   // write even while a WoW client is running
	MaxOutputBytes int    // cap on SavedVariables JSON per call (default 100000)
	Version        string
}

// Server holds the options and the detected install root.
type Server struct {
	opts   Options
	mu     sync.Mutex
	root   string
	source string
}

const instructions = `wow-mcp reads and edits a local World of Warcraft install: the addons under
Interface/AddOns, the WTF configuration tree (Config.wtf CVars, per-character
config-cache.wtf and AddOns.txt) and the Lua SavedVariables files.

Start with wow_installs to see which client flavors exist (retail, classic,
classic_era, forever, ...), then wow_characters for the account/realm/character
folders. Most tools take an optional "flavor"; account and realm may be left
empty when they are unambiguous.

Writes (wow_addon_set_enabled, wow_cvar_set, wow_saved_variable_set) refuse to
run while a WoW client is running, because the client rewrites these files on
logout and would discard the edit. Every write keeps a timestamped backup next
to the file (suffix ` + wow.BackupSuffix + `) and replaces the file atomically.
This server does not provide the WoW Lua API reference; pair it with an addon
development server such as hated-wow-mcp for that.`

// New builds the MCP server with every tool registered.
func New(opts Options) *mcp.Server {
	s := &Server{opts: opts}
	if s.opts.MaxOutputBytes <= 0 {
		s.opts.MaxOutputBytes = 100_000
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "wow-mcp", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	s.register(srv)
	return srv
}

// install detects (or re-opens the cached) WoW root.
func (s *Server) install() (*wow.Install, error) {
	s.mu.Lock()
	root, source := s.root, s.source
	s.mu.Unlock()
	explicit := s.opts.Root
	if root != "" {
		explicit = root
	}
	inst, err := wow.Detect(explicit)
	if err != nil {
		return nil, err
	}
	if source != "" {
		inst.Source = source
	}
	s.mu.Lock()
	s.root, s.source = inst.Root, inst.Source
	s.mu.Unlock()
	return inst, nil
}

// flavor resolves the flavor named in a call, falling back to the default.
func (s *Server) flavor(name string) (*wow.Install, *wow.Flavor, error) {
	inst, err := s.install()
	if err != nil {
		return nil, nil, err
	}
	fl, err := inst.Flavor(name, s.opts.DefaultFlavor)
	if err != nil {
		return nil, nil, err
	}
	return inst, fl, nil
}

// guardWrite enforces read-only mode and the running-client check.
func (s *Server) guardWrite(force bool) error {
	if s.opts.ReadOnly {
		return errors.New("this server runs read-only (-read-only / WOW_MCP_READ_ONLY); writes are disabled")
	}
	if s.opts.AllowRunning || force {
		return nil
	}
	running, detail, err := wow.WoWRunning()
	if err != nil {
		// Could not tell. Proceed; the backup still protects the file.
		return nil
	}
	if running {
		return fmt.Errorf("a WoW client is running (%s). The client rewrites WTF on logout and would discard this edit; close the game first, or pass force=true to write anyway", detail)
	}
	return nil
}

func boolp(b bool) *bool { return &b }

func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolp(false)}
}

func writes() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolp(true), IdempotentHint: true, OpenWorldHint: boolp(false)}
}
