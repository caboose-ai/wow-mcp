package server

import "github.com/modelcontextprotocol/go-sdk/mcp"

func (s *Server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_installs",
		Description: "Detect the local World of Warcraft install and list its client flavors (retail, classic, classic_era, forever, ptr...) with game version, derived TOC interface number, addon and character counts, and whether a client is running.",
		Annotations: readOnly(),
	}, s.installs)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_characters",
		Description: "List the WTF/Account tree of a flavor: accounts, realms and characters, with whether each character has an AddOns.txt and how many SavedVariables files it owns.",
		Annotations: readOnly(),
	}, s.characters)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_addons",
		Description: "List installed addons from Interface/AddOns with TOC metadata (title, version, interface versions, dependencies, SavedVariables names) and, when a character is given, the enabled state from that character's AddOns.txt plus entries in AddOns.txt that are no longer installed.",
		Annotations: readOnly(),
	}, s.addons)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_addon",
		Description: "Details of one installed addon: every .toc file with all fields and load list, disk footprint, where its SavedVariables files exist (account and per character), and its enabled state on every character.",
		Annotations: readOnly(),
	}, s.addon)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_addon_set_enabled",
		Description: "Enable or disable addons for one character or for all characters by editing AddOns.txt. Refuses while the WoW client runs (it would overwrite the file on logout); keeps a backup next to the file.",
		Annotations: writes(),
	}, s.setEnabled)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_cvars",
		Description: "Read console variables: the global WTF/Config.wtf, or a character's config-cache.wtf when a character is given. Optional substring filter on the name.",
		Annotations: readOnly(),
	}, s.cvars)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_cvar_set",
		Description: "Set or unset one CVar in Config.wtf (or a character's config-cache.wtf). Values are strings exactly as the client stores them. Refuses while the client runs; keeps a backup.",
		Annotations: writes(),
	}, s.cvarSet)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_saved_variables",
		Description: "Read an addon's SavedVariables Lua file (account-wide, or per character) as JSON. Use path to dive into a subtree and depth to limit nesting; large tables are summarised automatically. Lua tables with keys 1..n become arrays.",
		Annotations: readOnly(),
	}, s.savedVariables)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_saved_variable_set",
		Description: "Set or delete one value inside an addon's SavedVariables file, addressed by a dotted path whose first segment is the variable name. Rewrites the file in the client's own layout. Refuses while the client runs; keeps a backup.",
		Annotations: writes(),
	}, s.savedVariableSet)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_keybindings",
		Description: "Read key bindings from bindings-cache.wtf, account-wide or for one character.",
		Annotations: readOnly(),
	}, s.keybindings)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_macros",
		Description: "Read macros from macros-cache.txt, account-wide (general macros) or for one character.",
		Annotations: readOnly(),
	}, s.macros)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_addon_file",
		Description: "Read a text file inside an installed addon's folder (Lua, TOC, XML...), or list a folder. Paths are confined to the addon folder; large files are paged with offset.",
		Annotations: readOnly(),
	}, s.addonFile)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wow_addon_grep",
		Description: "Search installed addon sources with a regular expression, across all addons or inside one. Returns addon, file, line number and the matching line.",
		Annotations: readOnly(),
	}, s.addonGrep)
}
