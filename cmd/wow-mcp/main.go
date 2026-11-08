// Command wow-mcp is an MCP server over stdio for a local World of Warcraft
// install: addons, WTF configuration and SavedVariables.
//
//	wow-mcp [flags] [serve|inspect|version]
//
// Flags may also come from the environment: WOW_ROOT, WOW_DEFAULT_FLAVOR,
// WOW_MCP_READ_ONLY=1, WOW_MCP_ALLOW_RUNNING=1.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	"github.com/caboose-ai/wow-mcp/internal/server"
	"github.com/caboose-ai/wow-mcp/internal/wow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func envBool(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func main() {
	fs := flag.NewFlagSet("wow-mcp", flag.ExitOnError)
	root := fs.String("root", os.Getenv("WOW_ROOT"), "WoW install root (the folder that contains _retail_, _classic_, _forever_...); default $WOW_ROOT or auto-detect")
	flavor := fs.String("flavor", os.Getenv("WOW_DEFAULT_FLAVOR"), "flavor used when a call does not name one: retail, classic, classic_era, forever... ; default $WOW_DEFAULT_FLAVOR, else retail")
	readOnly := fs.Bool("read-only", envBool("WOW_MCP_READ_ONLY"), "disable every tool that writes")
	allowRunning := fs.Bool("allow-running", envBool("WOW_MCP_ALLOW_RUNNING"), "allow writes while a WoW client is running (the client overwrites WTF on logout)")
	maxOut := fs.Int("max-output-bytes", 100000, "cap on the SavedVariables JSON returned per call")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: wow-mcp [flags] [serve|inspect|version]\n\n  serve    run the MCP server on stdio (default)\n  inspect  print the detected install, characters and addons as JSON\n  version  print the version\n\nflags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	cmd := "serve"
	if fs.NArg() > 0 {
		cmd = fs.Arg(0)
	}
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	switch cmd {
	case "version":
		fmt.Println("wow-mcp", version)
	case "inspect":
		if err := inspect(*root, *flavor); err != nil {
			log.Fatalf("wow-mcp: %v", err)
		}
	case "serve":
		opts := server.Options{Root: *root, DefaultFlavor: *flavor, ReadOnly: *readOnly, AllowRunning: *allowRunning, MaxOutputBytes: *maxOut, Version: version}
		srv := server.New(opts)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if inst, err := wow.Detect(*root); err == nil {
			log.Printf("wow-mcp %s: serving %s (%d flavors) on stdio", version, inst.Root, len(inst.Flavors))
		} else {
			log.Printf("wow-mcp %s: no install detected yet (%v); tools will report the error", version, err)
		}
		if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("wow-mcp: %v", err)
		}
	default:
		fs.Usage()
		os.Exit(2)
	}
}

// inspect prints what the tools would see, for checking a setup by hand.
func inspect(root, flavor string) error {
	inst, err := wow.Detect(root)
	if err != nil {
		return err
	}
	type flavorReport struct {
		wow.Flavor
		Accounts []wow.Account `json:"accounts"`
		Addons   []string      `json:"addons"`
	}
	report := struct {
		Install *wow.Install   `json:"install"`
		Default string         `json:"default_flavor,omitempty"`
		Flavors []flavorReport `json:"flavors"`
	}{Install: inst, Default: flavor}
	for i := range inst.Flavors {
		fl := &inst.Flavors[i]
		fr := flavorReport{Flavor: *fl}
		fr.Accounts, _ = fl.Accounts()
		if fr.Accounts == nil {
			fr.Accounts = []wow.Account{}
		}
		fr.Addons = []string{}
		addons, _ := wow.ListAddons(fl)
		for _, a := range addons {
			fr.Addons = append(fr.Addons, a.Name)
		}
		report.Flavors = append(report.Flavors, fr)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
