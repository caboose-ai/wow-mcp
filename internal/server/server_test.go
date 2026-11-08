package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixtureCopy(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "wow", "testdata", "root"))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	err = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func start(t *testing.T, opts Options) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := New(opts)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

type obj = map[string]any

// call invokes a tool and decodes the structured result. A tool error is
// returned as errText with a nil object.
func call(t *testing.T, cs *mcp.ClientSession, name string, args obj) (obj, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: transport error: %v", name, err)
	}
	if res.IsError {
		var sb strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				sb.WriteString(tc.Text)
			}
		}
		return nil, sb.String()
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out obj
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: result is not an object: %s", name, b)
	}
	return out, ""
}

// mustCall is call for tools that are expected to succeed.
func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args obj) obj {
	t.Helper()
	o, errText := call(t, cs, name, args)
	if errText != "" {
		t.Fatalf("%s: unexpected tool error: %s", name, errText)
	}
	return o
}

func list(o obj, k string) []any {
	v, _ := o[k].([]any)
	return v
}

func num(o obj, k string) float64 {
	v, _ := o[k].(float64)
	return v
}

func str(o obj, k string) string {
	v, _ := o[k].(string)
	return v
}

func findAddon(t *testing.T, o obj, name string) obj {
	t.Helper()
	for _, a := range list(o, "addons") {
		if ao := a.(obj); ao["name"] == name {
			return ao
		}
	}
	t.Fatalf("addon %s not in %v", name, o["addons"])
	return nil
}

func TestToolList(t *testing.T) {
	cs := start(t, Options{Root: fixtureCopy(t)})
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
		if tl.Annotations == nil {
			t.Errorf("%s has no annotations", tl.Name)
		}
	}
	for _, want := range []string{"wow_installs", "wow_characters", "wow_addons", "wow_addon", "wow_addon_set_enabled", "wow_cvars", "wow_cvar_set", "wow_saved_variables", "wow_saved_variable_set", "wow_keybindings", "wow_macros", "wow_addon_file", "wow_addon_grep"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if len(tools.Tools) != 13 {
		t.Errorf("tool count = %d", len(tools.Tools))
	}
}

func TestReadTools(t *testing.T) {
	root := fixtureCopy(t)
	cs := start(t, Options{Root: root})

	inst := mustCall(t, cs, "wow_installs", obj{})
	if str(inst, "root") != root || len(list(inst, "flavors")) != 2 {
		t.Fatalf("installs = %v", inst)
	}
	var retail obj
	for _, f := range list(inst, "flavors") {
		if fo := f.(obj); fo["name"] == "retail" {
			retail = fo
		}
	}
	if num(retail, "interface") != 110205 || num(retail, "addons") != 3 || num(retail, "characters") != 3 || num(retail, "accounts") != 1 {
		t.Errorf("retail = %v", retail)
	}

	chars := mustCall(t, cs, "wow_characters", obj{})
	accs := list(chars, "accounts")
	if len(accs) != 1 || str(accs[0].(obj), "name") != "TESTACC" || len(list(accs[0].(obj), "realms")) != 2 {
		t.Errorf("characters = %v", chars)
	}

	addons := mustCall(t, cs, "wow_addons", obj{"character": "Testchar", "realm": "Testrealm"})
	if num(addons, "count") != 3 || str(addons, "character") != "TESTACC/Testrealm/Testchar" {
		t.Errorf("addons = %v", addons)
	}
	ta := findAddon(t, addons, "TestAddon")
	if ta["enabled"] != true || ta["listed"] != true || ta["out_of_date"] != false || ta["toc"] != "TestAddon_Mainline.toc" || ta["title"] != "Test Addon (Mainline)" {
		t.Errorf("TestAddon = %v", ta)
	}
	oa := findAddon(t, addons, "OldAddon")
	if oa["enabled"] != false || oa["out_of_date"] != true || oa["load_on_demand"] != true || oa["default_state"] != "disabled" {
		t.Errorf("OldAddon = %v", oa)
	}
	if got := list(addons, "addons_txt_only"); len(got) != 1 || got[0] != "Gone" {
		t.Errorf("addons_txt_only = %v", got)
	}
	filtered := mustCall(t, cs, "wow_addons", obj{"filter": "old"})
	if num(filtered, "count") != 1 || findAddon(t, filtered, "OldAddon")["enabled"] != nil {
		t.Errorf("filtered = %v", filtered)
	}
	if _, errText := call(t, cs, "wow_addons", obj{"character": "Testchar"}); !strings.Contains(errText, "ambiguous") {
		t.Errorf("ambiguous error = %q", errText)
	}

	detail := mustCall(t, cs, "wow_addon", obj{"name": "testaddon"})
	if str(detail, "name") != "TestAddon" || len(list(detail, "tocs")) != 3 || num(detail, "file_count") != 6 || len(list(detail, "saved_variable_files")) != 2 || len(list(detail, "enabled_on")) != 3 {
		t.Errorf("detail = %v", detail)
	}
	if _, errText := call(t, cs, "wow_addon", obj{"name": "Nope"}); errText == "" {
		t.Error("missing addon did not error")
	}

	cv := mustCall(t, cs, "wow_cvars", obj{})
	if num(cv, "count") != 5 || str(cv, "scope") != "global" {
		t.Errorf("cvars = %v", cv)
	}
	cv = mustCall(t, cs, "wow_cvars", obj{"filter": "GX"})
	if num(cv, "count") != 1 {
		t.Errorf("filtered cvars = %v", cv)
	}
	cv = mustCall(t, cs, "wow_cvars", obj{"character": "Testchar", "realm": "Testrealm"})
	if num(cv, "count") != 2 || !strings.HasSuffix(str(cv, "file"), "config-cache.wtf") {
		t.Errorf("char cvars = %v", cv)
	}

	sv := mustCall(t, cs, "wow_saved_variables", obj{"addon": "TestAddon"})
	if vars := list(sv, "variables"); len(vars) != 2 || sv["data"].(obj)["TestAddonGlobal"] != 42.0 {
		t.Errorf("sv = %v", sv)
	}
	sv = mustCall(t, cs, "wow_saved_variables", obj{"addon": "TestAddon", "path": "TestAddonDB.profiles.Default.list"})
	if got, _ := sv["data"].([]any); len(got) != 3 || got[2] != "c" {
		t.Errorf("sv list = %v", sv)
	}
	sv = mustCall(t, cs, "wow_saved_variables", obj{"addon": "TestAddon", "path": "TestAddonDB.profiles", "depth": 1})
	if d := sv["data"].(obj); !strings.HasPrefix(d["Default"].(string), "<table:") {
		t.Errorf("depth 1 = %v", sv)
	}
	sv = mustCall(t, cs, "wow_saved_variables", obj{"addon": "TestAddon", "max_bytes": 60})
	if str(sv, "note") == "" {
		t.Errorf("small max_bytes gave no note: %v", sv)
	}
	sv = mustCall(t, cs, "wow_saved_variables", obj{"addon": "TestAddon", "character": "Testchar", "realm": "Testrealm"})
	if vars := list(sv, "variables"); len(vars) != 1 || vars[0] != "TestAddonCharDB" {
		t.Errorf("char sv = %v", sv)
	}
	if _, errText := call(t, cs, "wow_saved_variables", obj{"addon": "TestAddon", "path": "TestAddonDB.nope"}); !strings.Contains(errText, "nothing at path") {
		t.Errorf("bad path error = %q", errText)
	}
	if _, errText := call(t, cs, "wow_saved_variables", obj{"addon": "OldAddon"}); !strings.Contains(errText, "no SavedVariables file") {
		t.Errorf("missing sv error = %q", errText)
	}

	kb := mustCall(t, cs, "wow_keybindings", obj{})
	if num(kb, "count") != 3 || str(kb, "binding_set") != "0" {
		t.Errorf("bindings = %v", kb)
	}
	kb = mustCall(t, cs, "wow_keybindings", obj{"character": "Testchar", "realm": "Testrealm"})
	if num(kb, "count") != 1 || str(kb, "binding_set") != "1" {
		t.Errorf("char bindings = %v", kb)
	}
	mc := mustCall(t, cs, "wow_macros", obj{})
	if num(mc, "count") != 2 {
		t.Errorf("macros = %v", mc)
	}
	mc = mustCall(t, cs, "wow_macros", obj{"character": "Testchar", "realm": "Testrealm"})
	if num(mc, "count") != 1 || list(mc, "macros")[0].(obj)["body"] != "/say hi" {
		t.Errorf("char macros = %v", mc)
	}

	af := mustCall(t, cs, "wow_addon_file", obj{"addon": "TestAddon"})
	if af["is_dir"] != true || len(list(af, "entries")) != 6 {
		t.Errorf("dir listing = %v", af)
	}
	af = mustCall(t, cs, "wow_addon_file", obj{"addon": "TestAddon", "path": "core.lua"})
	if !strings.Contains(str(af, "content"), "hello from TestAddon") || af["truncated"] != nil {
		t.Errorf("core.lua = %v", af)
	}
	af = mustCall(t, cs, "wow_addon_file", obj{"addon": "TestAddon", "path": "locales/enUS.lua", "max_bytes": 10})
	if len(str(af, "content")) != 10 || af["truncated"] != true || num(af, "next_offset") != 10 {
		t.Errorf("paged = %v", af)
	}
	if _, errText := call(t, cs, "wow_addon_file", obj{"addon": "TestAddon", "path": "../OldAddon/old.lua"}); errText == "" {
		t.Error("path escape accepted")
	}

	gr := mustCall(t, cs, "wow_addon_grep", obj{"pattern": "RegisterEvent"})
	if num(gr, "count") != 1 || list(gr, "matches")[0].(obj)["file"] != "TestAddon/core.lua" || num(list(gr, "matches")[0].(obj), "line") != 3 {
		t.Errorf("grep = %v", gr)
	}
	gr = mustCall(t, cs, "wow_addon_grep", obj{"pattern": "HELLO", "ignore_case": true})
	if num(gr, "count") != 2 { // core.lua print and enUS.lua L["Hello"]
		t.Errorf("grep ci = %v", gr)
	}
	gr = mustCall(t, cs, "wow_addon_grep", obj{"pattern": "Frame", "extensions": []string{"xml"}, "addon": "TestAddon"})
	if num(gr, "count") != 1 || num(gr, "files_searched") != 1 {
		t.Errorf("grep xml = %v", gr)
	}
	gr = mustCall(t, cs, "wow_addon_grep", obj{"pattern": ".", "max_results": 2})
	if num(gr, "count") != 2 || gr["truncated"] != true {
		t.Errorf("grep truncated = %v", gr)
	}
	if _, errText := call(t, cs, "wow_addon_grep", obj{"pattern": "("}); !strings.Contains(errText, "bad pattern") {
		t.Errorf("bad pattern error = %q", errText)
	}
}

func TestWriteTools(t *testing.T) {
	root := fixtureCopy(t)
	cs := start(t, Options{Root: root, AllowRunning: true})
	char := obj{"character": "Testchar", "realm": "Testrealm"}

	// AddOns.txt
	r := mustCall(t, cs, "wow_addon_set_enabled", obj{"addons": []string{"OldAddon"}, "enabled": true, "character": "Testchar", "realm": "Testrealm"})
	ch := list(r, "changes")
	if len(ch) != 1 || ch[0].(obj)["before"] != "disabled" || ch[0].(obj)["after"] != "enabled" || len(list(r, "backups")) != 1 {
		t.Fatalf("set_enabled = %v", r)
	}
	addons := mustCall(t, cs, "wow_addons", char)
	if findAddon(t, addons, "OldAddon")["enabled"] != true {
		t.Error("OldAddon not enabled after write")
	}
	raw, _ := os.ReadFile(filepath.Join(root, "_retail_", "WTF", "Account", "TESTACC", "Testrealm", "Testchar", "AddOns.txt"))
	if !strings.Contains(string(raw), "OldAddon: enabled\r\n") || !strings.HasSuffix(string(raw), "Gone: enabled\r\n") {
		t.Errorf("AddOns.txt = %q", raw)
	}
	r = mustCall(t, cs, "wow_addon_set_enabled", obj{"addons": []string{"TestAddon", "Blizzard_Foo"}, "enabled": false, "all_characters": true})
	if num(r, "characters") != 3 || len(list(r, "changes")) != 5 || len(list(r, "unchanged")) != 1 {
		t.Errorf("all_characters = %v", r)
	}
	if _, errText := call(t, cs, "wow_addon_set_enabled", obj{"addons": []string{"Nope"}, "enabled": true, "all_characters": true}); !strings.Contains(errText, "no addon") {
		t.Errorf("unknown addon error = %q", errText)
	}
	if _, errText := call(t, cs, "wow_addon_set_enabled", obj{"addons": []string{"TestAddon"}, "enabled": true}); !strings.Contains(errText, "all_characters") {
		t.Errorf("no target error = %q", errText)
	}

	// CVars
	r = mustCall(t, cs, "wow_cvar_set", obj{"name": "gxMaximize", "value": "0"})
	if r["before"] != "1" || r["after"] != "0" || r["changed"] != true || str(r, "backup") == "" {
		t.Errorf("cvar_set = %v", r)
	}
	r = mustCall(t, cs, "wow_cvar_set", obj{"name": "gxMaximize", "value": "0"})
	if r["changed"] != false || str(r, "backup") != "" {
		t.Errorf("idempotent cvar_set = %v", r)
	}
	r = mustCall(t, cs, "wow_cvar_set", obj{"name": "portal", "unset": true})
	if r["before"] != "US" || r["after"] != nil || r["changed"] != true {
		t.Errorf("unset = %v", r)
	}
	r = mustCall(t, cs, "wow_cvar_set", obj{"name": "brandNew", "value": "7", "character": "Testchar", "realm": "Testrealm"})
	if r["before"] != nil || r["after"] != "7" || !strings.HasSuffix(str(r, "file"), "config-cache.wtf") {
		t.Errorf("char cvar_set = %v", r)
	}
	cv := mustCall(t, cs, "wow_cvars", obj{})
	if num(cv, "count") != 4 {
		t.Errorf("cvars after edits = %v", cv)
	}
	if _, errText := call(t, cs, "wow_cvar_set", obj{"name": "bad name", "value": "1"}); errText == "" {
		t.Error("bad cvar name accepted")
	}

	// SavedVariables
	r = mustCall(t, cs, "wow_saved_variable_set", obj{"addon": "TestAddon", "path": "TestAddonDB.profiles.Default.scale", "value_json": "2"})
	if r["before"] != 1.25 || r["after"] != 2.0 || r["existed"] != true || str(r, "backup") == "" {
		t.Errorf("sv set = %v", r)
	}
	sv := mustCall(t, cs, "wow_saved_variables", obj{"addon": "TestAddon", "path": "TestAddonDB.profiles.Default.scale"})
	if sv["data"] != 2.0 {
		t.Errorf("sv read back = %v", sv)
	}
	r = mustCall(t, cs, "wow_saved_variable_set", obj{"addon": "TestAddon", "path": "TestAddonDB.profiles.Default.nested", "delete": true})
	if r["existed"] != true || r["before"] == nil {
		t.Errorf("sv delete = %v", r)
	}
	if _, errText := call(t, cs, "wow_saved_variables", obj{"addon": "TestAddon", "path": "TestAddonDB.profiles.Default.nested"}); errText == "" {
		t.Error("deleted path still readable")
	}
	r = mustCall(t, cs, "wow_saved_variable_set", obj{"addon": "OldAddon", "path": "OldAddonDB.opts", "value_json": `{"a":1,"list":[1,2]}`})
	if r["created_file"] != true || str(r, "backup") != "" {
		t.Errorf("sv create = %v", r)
	}
	created, err := os.ReadFile(filepath.Join(root, "_retail_", "WTF", "Account", "TESTACC", "SavedVariables", "OldAddon.lua"))
	if err != nil || string(created) != "OldAddonDB = {\n\t[\"opts\"] = {\n\t\t[\"a\"] = 1,\n\t\t[\"list\"] = {\n\t\t\t[1] = 1,\n\t\t\t[2] = 2,\n\t\t},\n\t},\n}\n" {
		t.Errorf("created file = %q %v", created, err)
	}
	if _, errText := call(t, cs, "wow_saved_variable_set", obj{"addon": "TestAddon", "path": "TestAddonDB.x", "value_json": "{bad"}); !strings.Contains(errText, "not valid JSON") {
		t.Errorf("bad json error = %q", errText)
	}
	if _, errText := call(t, cs, "wow_saved_variable_set", obj{"addon": "TestAddon", "path": "TestAddonDB.x"}); !strings.Contains(errText, "value_json is required") {
		t.Errorf("missing value error = %q", errText)
	}
	if _, errText := call(t, cs, "wow_saved_variable_set", obj{"addon": "Unknown", "path": "X.y", "value_json": "1"}); !strings.Contains(errText, "no addon") {
		t.Errorf("unknown addon sv error = %q", errText)
	}
}

func TestReadOnly(t *testing.T) {
	cs := start(t, Options{Root: fixtureCopy(t), ReadOnly: true, AllowRunning: true})
	for name, args := range map[string]obj{
		"wow_cvar_set":           {"name": "gxMaximize", "value": "0"},
		"wow_addon_set_enabled":  {"addons": []string{"TestAddon"}, "enabled": false, "all_characters": true},
		"wow_saved_variable_set": {"addon": "TestAddon", "path": "TestAddonDB.v", "value_json": "1"},
	} {
		if _, errText := call(t, cs, name, args); !strings.Contains(errText, "read-only") {
			t.Errorf("%s in read-only mode: %q", name, errText)
		}
	}
	inst := mustCall(t, cs, "wow_installs", obj{})
	if inst["read_only"] != true {
		t.Errorf("installs read_only = %v", inst["read_only"])
	}
}

func TestNoInstall(t *testing.T) {
	cs := start(t, Options{Root: t.TempDir()})
	if _, errText := call(t, cs, "wow_installs", obj{}); !strings.Contains(errText, "WOW_ROOT") {
		t.Errorf("no install error = %q", errText)
	}
}
