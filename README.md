# wow-mcp

An MCP server for the files a World of Warcraft install leaves on disk: the
addons under `Interface/AddOns`, the `WTF` configuration tree (`Config.wtf`
CVars, per-character `config-cache.wtf` and `AddOns.txt`) and the Lua
`SavedVariables` files the client writes on logout.

It is the local-install complement to an addon *development* server such as
[hated-wow-mcp](https://github.com/RdyGaming/hated-wow-mcp), which provides
the Lua API reference, Blizzard UI source, linting and scaffolding. This
server does none of that; it reads and edits what is installed and configured
on your machine. Run both side by side.

Single static Go binary, stdio transport, no network access.

## What it can do

| Tool | Writes | Purpose |
|---|---|---|
| `wow_installs` | | Find the install, list flavors (`retail`, `classic`, `classic_era`, `forever`, `ptr`...), game version and derived TOC interface number, whether a client is running |
| `wow_characters` | | Accounts, realms and characters under `WTF/Account` |
| `wow_addons` | | Installed addons with TOC metadata; with a character, the enabled state from its `AddOns.txt` and stale entries |
| `wow_addon` | | One addon in depth: every `.toc`, disk footprint, where its SavedVariables files are, enabled state per character |
| `wow_addon_set_enabled` | yes | Enable or disable addons for one or all characters |
| `wow_cvars` | | Read `Config.wtf` or a character's `config-cache.wtf` |
| `wow_cvar_set` | yes | Set or unset one CVar |
| `wow_saved_variables` | | Read a SavedVariables file as JSON, with path and depth controls |
| `wow_saved_variable_set` | yes | Set or delete one value in a SavedVariables file by path |
| `wow_keybindings` | | `bindings-cache.wtf`, account-wide or per character |
| `wow_macros` | | `macros-cache.txt`, account-wide or per character |
| `wow_addon_file` | | Read or list files inside an installed addon |
| `wow_addon_grep` | | Regex search across installed addon sources |

## Install

Build on the machine where WoW is installed (the server needs the files, not
the game running):

```bash
go install github.com/caboose-ai/wow-mcp/cmd/wow-mcp@latest
```

or from a checkout: `make build` (current OS) or `make cross` for Windows and
macOS binaries in `dist/`.

Register with Claude Code (user scope, so every project sees it):

```bash
claude mcp add -s user wow-local -- wow-mcp
```

Other MCP clients take the same shape: command `wow-mcp`, no arguments, optional
environment below. Check the setup without a client:

```bash
wow-mcp inspect
```

## Configuration

Flags, or the environment variable in brackets:

| Flag | Env | Meaning |
|---|---|---|
| `-root PATH` | `WOW_ROOT` | The folder that contains `_retail_`, `_classic_`, `_forever_`... Auto-detected on Windows, macOS and Wine/Lutris/Bottles/Proton prefixes when unset. A flavor folder itself is accepted too |
| `-flavor NAME` | `WOW_DEFAULT_FLAVOR` | Flavor used when a call does not name one (default `retail`, or the only one installed) |
| `-read-only` | `WOW_MCP_READ_ONLY=1` | Disable every tool that writes |
| `-allow-running` | `WOW_MCP_ALLOW_RUNNING=1` | Allow writes while a client is running (see below) |
| `-max-output-bytes N` | | Cap on SavedVariables JSON per call (default 100000) |

Example for a Forever-first setup:

```bash
claude mcp add -s user -e WOW_DEFAULT_FLAVOR=forever wow-local -- wow-mcp
```

## Safety rules

- **Writes refuse while WoW runs.** The client holds `WTF` in memory and
  rewrites `Config.wtf`, `AddOns.txt` and SavedVariables on logout, which would
  silently discard an edit. Each write tool checks the process list and fails
  with an explanation; pass `force: true` on the call, or start the server with
  `-allow-running`, to override.
- **Every write keeps a backup** next to the file, named
  `<file>.<UTC timestamp>.wow-mcp.bak`, and replaces the file atomically. To
  undo, copy the backup over the file while the game is closed.
- **Names are confined.** Addon, account, realm and character arguments may
  not contain path separators or `..`; `wow_addon_file` paths cannot leave the
  addon folder. Writes only address installed addons (or `Blizzard_*`).
- `-read-only` turns the server into a pure inspector.

## Paths into SavedVariables

`wow_saved_variables` and `wow_saved_variable_set` take a `path` whose first
segment is the variable name:

- dotted: `MyAddonDB.profiles.Default.scale`
- as a JSON array when a key contains a dot: `["MyAddonDB","profileKeys","Char - Realm"]`

Numeric-looking segments match Lua number keys. Values for
`wow_saved_variable_set` are JSON (`1.5`, `true`, `"text"`, `null`, arrays,
objects); object keys that look like integers become Lua number keys, arrays
become tables keyed `1..n`. Reading converts tables keyed `1..n` to JSON
arrays and non-finite numbers to the strings `"inf"`, `"-inf"`, `"nan"`.

The rewritten file uses the client's own layout (tabs, bracketed keys, trailing
commas). The `-- [n]` comments the client adds after array entries are not
reproduced; the client does not need them.

## Flavors and TOC files

Flavor folders map to short names by trimming underscores: `_retail_` is
`retail`, `_classic_era_` is `classic_era`, `_forever_` is `forever`. Aliases
`mainline`, `era` and `vanilla` are accepted. `.build.info` at the root
supplies the game version per product; the TOC interface number is derived as
`major*10000 + minor*100 + patch` and used to flag addons whose `## Interface`
does not match (`out_of_date`).

For an addon with several `.toc` files the one the client would load is
picked: `_Mainline` for retail, `_Vanilla` for Classic Era, `_Camelot` for
Forever (Camelot is its codename), `_Mists`/`_Classic`/`_Cata`/... for the
progression client, then the plain `Name.toc`.

## Limitations

- Blizzard's own addons (`Blizzard_*`) live inside the game data, so they are
  not listed as installed; they can still be enabled or disabled.
- There is no Lua API reference, linter or scaffolder here; use an addon
  development server for that.
- Running-client detection matches `Wow.exe`, `WowClassic.exe`, `WowT.exe`,
  `WowB.exe` and the macOS app binary. If it cannot run `ps`/`tasklist` it
  assumes the client is closed and relies on the backup.
- Edits to `Config.wtf` made while the game is closed take effect on the next
  launch. SavedVariables edits are read on the next login of that
  account/character.

## Development

```bash
go test ./...
```

Tests run against a fixture install under `internal/wow/testdata/root` (two
flavors, one account, three characters, three addons) and an in-memory MCP
client; write tests copy the fixture to a temp dir first.

## License

MIT, see [LICENSE](LICENSE).
