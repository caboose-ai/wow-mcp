package wow

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "root"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// copyTree copies the fixture into a temp dir so write tests leave it untouched.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func retail(t *testing.T, root string) *Flavor {
	t.Helper()
	inst, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	fl, err := inst.Flavor("retail", "")
	if err != nil {
		t.Fatal(err)
	}
	return fl
}

func TestOpenInstall(t *testing.T) {
	root := fixtureRoot(t)
	inst, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Flavors) != 2 {
		t.Fatalf("flavors = %+v", inst.Flavors)
	}
	fl, err := inst.Flavor("", "")
	if err != nil || fl.Name != "retail" {
		t.Fatalf("default flavor = %v, %v", fl, err)
	}
	if fl.Product != "wow" || fl.Version != "11.2.5.64104" || fl.Interface != 110205 || !fl.HasInterface || !fl.HasWTF {
		t.Errorf("retail = %+v", *fl)
	}
	era, err := inst.Flavor("era", "")
	if err != nil || era.Name != "classic_era" || era.Interface != 11507 {
		t.Errorf("era = %+v, %v", era, err)
	}
	if _, err := inst.Flavor("forever", ""); err == nil || !strings.Contains(err.Error(), "classic_era, retail") {
		t.Errorf("missing flavor error = %v", err)
	}
	if fl, err := inst.Flavor("", "classic_era"); err != nil || fl.Name != "classic_era" {
		t.Errorf("default name = %v, %v", fl, err)
	}
	// A flavor folder is accepted as root too.
	inst2, err := Open(filepath.Join(root, "_retail_"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inst2.Flavors) != 1 || inst2.Flavors[0].Version != "11.2.5.64104" || inst2.Root != root {
		t.Errorf("flavor-as-root = %+v", inst2)
	}
	if _, err := Open(t.TempDir()); err == nil {
		t.Error("empty dir accepted as root")
	}
	if _, err := Detect(t.TempDir()); err == nil {
		t.Error("Detect accepted empty dir")
	}
	_ = Candidates() // must not panic on any OS
}

func TestInterfaceFromVersion(t *testing.T) {
	cases := map[string]int{"11.2.5.64104": 110205, "1.15.7.60000": 11507, "4.4.2.1": 40402, "bad": 0, "1.2": 0, "1.60.1.123": 16001}
	for in, want := range cases {
		if got := InterfaceFromVersion(in); got != want {
			t.Errorf("InterfaceFromVersion(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseBuildInfo(t *testing.T) {
	rows, err := ParseBuildInfo(filepath.Join(fixtureRoot(t), ".build.info"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Product != "wow" || rows[0].Version != "11.2.5.64104" || rows[1].Product != "wow_classic_era" || rows[0].Branch != "us" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestAddonsAndTOCs(t *testing.T) {
	fl := retail(t, fixtureRoot(t))
	a, err := FindAddon(fl, "testaddon")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "TestAddon" || len(a.TOCs) != 3 || a.TOCs[0].Suffix != "" {
		t.Fatalf("addon = %+v", a)
	}
	plain := a.TOCs[0]
	if plain.Title != "Test Addon" || plain.Notes != "A fixture addon." || plain.Author != "Fixture" || plain.Version != "1.2.3" {
		t.Errorf("plain toc = %+v", plain)
	}
	if !reflect.DeepEqual(plain.Interface, []int{110200}) || !reflect.DeepEqual(plain.SavedVariables, []string{"TestAddonDB", "TestAddonGlobal"}) ||
		!reflect.DeepEqual(plain.SavedVariablesPerCharacter, []string{"TestAddonCharDB"}) || !reflect.DeepEqual(plain.OptionalDeps, []string{"Ace3", "LibStub"}) {
		t.Errorf("plain lists = %+v", plain)
	}
	if plain.Fields["X-Website"] != "https://example.invalid" || plain.Get("x-website") == "" {
		t.Errorf("fields = %v", plain.Fields)
	}
	if !reflect.DeepEqual(plain.Files, []string{"core.lua", `locales\enUS.lua`, "ui.xml"}) {
		t.Errorf("files = %v", plain.Files)
	}
	if plain.DefaultState != "enabled" || plain.LoadOnDemand {
		t.Errorf("defaults = %+v", plain)
	}
	for flavor, want := range map[string]string{"retail": "Mainline", "classic_era": "Vanilla", "forever": "Vanilla", "classic": "", "unknown": ""} {
		if got := a.TOCFor(flavor); got.Suffix != want {
			t.Errorf("TOCFor(%s) = %q, want %q", flavor, got.Suffix, want)
		}
	}
	old, err := FindAddon(fl, "OldAddon")
	if err != nil {
		t.Fatal(err)
	}
	ot := old.TOCFor("retail")
	if ot.DefaultState != "disabled" || !ot.LoadOnDemand || !reflect.DeepEqual(ot.Dependencies, []string{"TestAddon"}) || !reflect.DeepEqual(ot.Interface, []int{100207}) {
		t.Errorf("old toc = %+v", ot)
	}
	list, err := ListAddons(fl)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range list {
		names = append(names, x.Name)
	}
	if !reflect.DeepEqual(names, []string{"OldAddon", "Orphan", "TestAddon"}) {
		t.Errorf("names = %v", names)
	}
	orphan := list[1]
	if len(orphan.TOCs) != 0 || !reflect.DeepEqual(orphan.IgnoredTOCs, []string{"wrong.toc"}) || orphan.TOCFor("retail") != nil {
		t.Errorf("orphan = %+v", orphan)
	}
	if _, err := FindAddon(fl, "../etc"); err == nil {
		t.Error("path-like addon name accepted")
	}
	if _, err := FindAddon(fl, "Missing"); err == nil {
		t.Error("missing addon found")
	}
}

func TestStripColorCodes(t *testing.T) {
	if got := StripColorCodes("|cff00ff00Test|r Addon"); got != "Test Addon" {
		t.Errorf("got %q", got)
	}
	if got := StripColorCodes(`|TInterface\Icons\X:0|t Name`); got != " Name" {
		t.Errorf("got %q", got)
	}
}

func TestAddonsTxt(t *testing.T) {
	root := copyTree(t, fixtureRoot(t))
	path := filepath.Join(root, "_retail_", "WTF", "Account", "TESTACC", "Testrealm", "Testchar", "AddOns.txt")
	original, _ := os.ReadFile(path)
	a, err := LoadAddonsTxt(path)
	if err != nil || !a.Existed {
		t.Fatal(err)
	}
	if en, ok := a.Get("TestAddon"); !en || !ok {
		t.Error("TestAddon should be enabled+listed")
	}
	if en, ok := a.Get("oldaddon"); en || !ok {
		t.Error("OldAddon should be disabled+listed")
	}
	if _, ok := a.Get("Nope"); ok {
		t.Error("Nope should be unlisted")
	}
	if !reflect.DeepEqual(a.Names(), []string{"TestAddon", "OldAddon", "Blizzard_Calendar", "Gone"}) {
		t.Errorf("names = %v", a.Names())
	}
	if !a.Set("OldAddon", true) || a.Set("OldAddon", true) || !a.Set("New", false) {
		t.Error("Set change reporting is wrong")
	}
	out := string(a.Bytes())
	if !strings.Contains(out, "OldAddon: enabled\r\n") || !strings.HasSuffix(out, "New: disabled\r\n") || strings.Contains(out, "enabled\n\n") {
		t.Errorf("bytes = %q", out)
	}
	backup, err := a.Save()
	if err != nil || backup == "" {
		t.Fatalf("save: %v %q", err, backup)
	}
	if b, _ := os.ReadFile(backup); string(b) != string(original) {
		t.Error("backup does not hold the original content")
	}
	again, _ := LoadAddonsTxt(path)
	if en, ok := again.Get("New"); en || !ok {
		t.Error("New not persisted")
	}
	missing := filepath.Join(root, "nope", "AddOns.txt")
	m, err := LoadAddonsTxt(missing)
	if err != nil || m.Existed {
		t.Fatal("missing file should load empty")
	}
	m.Set("X", true)
	if backup, err := m.Save(); err != nil || backup != "" {
		t.Errorf("first save: %v %q", err, backup)
	}
	if b, _ := os.ReadFile(missing); string(b) != "X: enabled\n" {
		t.Errorf("new file = %q", b)
	}
}

func TestWTFConfig(t *testing.T) {
	root := copyTree(t, fixtureRoot(t))
	c, err := LoadWTFConfig(filepath.Join(root, "_retail_", "WTF", "Config.wtf"))
	if err != nil || len(c.All()) != 5 {
		t.Fatalf("load: %v %d", err, len(c.All()))
	}
	if v, ok := c.Get("GXMAXIMIZE"); v != "1" || !ok {
		t.Error("case-insensitive Get failed")
	}
	if prev, existed := c.Set("gxMaximize", "0"); prev != "1" || !existed {
		t.Error("Set existing")
	}
	if _, existed := c.Set("newVar", "x"); existed {
		t.Error("Set new")
	}
	if prev, existed := c.Unset("portal"); prev != "US" || !existed {
		t.Error("Unset")
	}
	if _, existed := c.Unset("portal"); existed {
		t.Error("Unset twice")
	}
	want := "SET hwDetect \"0\"\nSET gxMaximize \"0\"\nSET graphicsQuality \"7\"\nSET Sound_MasterVolume \"0.5\"\nSET newVar \"x\"\n"
	if got := string(c.Bytes()); got != want {
		t.Errorf("bytes = %q", got)
	}
	cc, err := LoadWTFConfig(filepath.Join(root, "_retail_", "WTF", "Account", "TESTACC", "Testrealm", "Testchar", "config-cache.wtf"))
	if err != nil {
		t.Fatal(err)
	}
	cc.Set("autoLootDefault", "0")
	if got := string(cc.Bytes()); got != "SET nameplateShowAll \"1\"\r\nSET autoLootDefault \"0\"\r\n" {
		t.Errorf("crlf bytes = %q", got)
	}
	if backup, err := cc.Save(); err != nil || backup == "" {
		t.Errorf("save: %v %q", err, backup)
	}
}

func TestAccountsAndResolve(t *testing.T) {
	fl := retail(t, fixtureRoot(t))
	accs, err := fl.Accounts()
	if err != nil || len(accs) != 1 { // WTF/Account/SavedVariables is not an account
		t.Fatalf("accounts = %+v %v", accs, err)
	}
	acc := accs[0]
	if acc.Name != "TESTACC" || acc.SavedVariables != 1 || len(acc.Realms) != 2 {
		t.Fatalf("account = %+v", acc)
	}
	if acc.Realms[0].Name != "Testrealm" || len(acc.Realms[0].Characters) != 1 || !acc.Realms[0].Characters[0].HasAddonsTxt || acc.Realms[0].Characters[0].SavedVariables != 1 {
		t.Errorf("realm0 = %+v", acc.Realms[0])
	}
	if acc.Realms[1].Name != "Testrealm2" || len(acc.Realms[1].Characters) != 2 || acc.Realms[1].Characters[0].Name != "Altchar" || acc.Realms[1].Characters[0].HasAddonsTxt {
		t.Errorf("realm1 = %+v", acc.Realms[1])
	}
	if _, err := fl.ResolveCharacter("", "", "Testchar"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("ambiguous = %v", err)
	}
	if c, err := fl.ResolveCharacter("", "Testrealm", "testchar"); err != nil || c.Realm != "Testrealm" || c.Name != "Testchar" {
		t.Errorf("by realm = %+v %v", c, err)
	}
	if c, err := fl.ResolveCharacter("", "", "Altchar"); err != nil || c.Realm != "Testrealm2" {
		t.Errorf("unique = %+v %v", c, err)
	}
	if _, err := fl.ResolveCharacter("", "", "Nobody"); err == nil {
		t.Error("Nobody resolved")
	}
	if a, err := fl.ResolveAccount(""); err != nil || a.Name != "TESTACC" {
		t.Errorf("single account = %v %v", a, err)
	}
	if _, err := fl.ResolveAccount("testacc"); err != nil {
		t.Error(err)
	}
	if _, err := fl.ResolveAccount("x"); err == nil {
		t.Error("bad account resolved")
	}
	all, _ := fl.AllCharacters()
	if len(all) != 3 {
		t.Errorf("all = %d", len(all))
	}
}

func TestBindingsAndMacros(t *testing.T) {
	acc := filepath.Join(fixtureRoot(t), "_retail_", "WTF", "Account", "TESTACC")
	b, err := ParseBindings(filepath.Join(acc, "bindings-cache.wtf"))
	if err != nil || b.BindingSet != "0" || len(b.Bindings) != 3 || b.Bindings[0] != (Binding{Key: "F1", Command: "ACTIONBUTTON1"}) {
		t.Errorf("bindings = %+v %v", b, err)
	}
	m, err := ParseMacros(filepath.Join(acc, "macros-cache.txt"))
	if err != nil || len(m) != 2 {
		t.Fatalf("macros = %+v %v", m, err)
	}
	if m[0].ID != "1" || m[0].Name != "Mount" || m[0].Icon != "INV_MISC_QUESTIONMARK" || m[0].Body != "#showtooltip\n/cast Swift Zulian Tiger" {
		t.Errorf("macro0 = %+v", m[0])
	}
	if m[1].Name != "Heal" || !strings.Contains(m[1].Body, "[@mouseover") {
		t.Errorf("macro1 = %+v", m[1])
	}
}

func accountSV(t *testing.T, root string) string {
	return filepath.Join(root, "_retail_", "WTF", "Account", "TESTACC", "SavedVariables", "TestAddon.lua")
}

func TestParseLuaFixture(t *testing.T) {
	sv, err := LoadSavedVariables(accountSV(t, fixtureRoot(t)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sv.Names(), []string{"TestAddonDB", "TestAddonGlobal"}) {
		t.Fatalf("names = %v", sv.Names())
	}
	if g, _ := sv.Get("TestAddonGlobal"); g != 42.0 {
		t.Errorf("global = %v", g)
	}
	look := func(path ...string) any {
		v, ok := sv.Lookup(path)
		if !ok {
			t.Fatalf("missing %v", path)
		}
		return v
	}
	def := []string{"TestAddonDB", "profiles", "Default"}
	if v := look(append(def, "scale")...); v != 1.25 {
		t.Errorf("scale = %v", v)
	}
	if v := look(append(def, "name")...); v != "Quote \" and backslash \\ and pipe |cff00ff00x|r and tab\tend" {
		t.Errorf("name = %q", v)
	}
	list := look(append(def, "list")...).(*LuaTable)
	if !list.IsArray() || len(list.Entries) != 3 || list.Entries[1].Value != "b" {
		t.Errorf("list = %+v", list)
	}
	if v := look(append(def, "count")...); v != -3.0 {
		t.Errorf("count = %v", v)
	}
	if v := look(append(def, "tiny")...); v != 1e-05 {
		t.Errorf("tiny = %v", v)
	}
	if v := look(append(def, "big")...); v != 1234567890123.0 {
		t.Errorf("big = %v", v)
	}
	if v := look(append(def, "inf")...); !math.IsInf(v.(float64), 1) {
		t.Errorf("inf = %v", v)
	}
	if v := look(append(def, "neginf")...); !math.IsInf(v.(float64), -1) {
		t.Errorf("neginf = %v", v)
	}
	nested := look(append(def, "nested")...).(*LuaTable)
	if len(nested.Entries) != 4 || nested.Entries[2].Value != nil || nested.Entries[3].Key != true || nested.Entries[3].Value != "boolkey" || nested.IsArray() {
		t.Errorf("nested = %+v", nested)
	}
	if v := look(append(def, "10")...); v != "sparse" {
		t.Errorf("sparse = %v", v)
	}
	path, err := ParsePath(`["TestAddonDB","profileKeys","Testchar - Testrealm"]`)
	if err != nil {
		t.Fatal(err)
	}
	if v := look(path...); v != "Default" {
		t.Errorf("profileKeys = %v", v)
	}
	if _, ok := sv.Lookup([]string{"TestAddonDB", "nope"}); ok {
		t.Error("lookup of missing key succeeded")
	}
	if _, ok := sv.Lookup([]string{"TestAddonGlobal", "x"}); ok {
		t.Error("lookup through a number succeeded")
	}
}

func TestLuaRoundTrip(t *testing.T) {
	src, _ := os.ReadFile(accountSV(t, fixtureRoot(t)))
	sv1, err := ParseLua(src)
	if err != nil {
		t.Fatal(err)
	}
	out := sv1.Bytes()
	for _, want := range []string{
		"TestAddonDB = {\n",
		"\t\t\t[\"inf\"] = 1/0,\n",
		"\t\t\t[\"neginf\"] = -1/0,\n",
		"\t\t\t[\"name\"] = \"Quote \\\" and backslash \\\\ and pipe |cff00ff00x|r and tab\\tend\",\n",
		"\t\t\t\t[true] = \"boolkey\",\n",
		"\t\t\t[\"tiny\"] = 1e-05,\n",
		"\t\t\t[\"big\"] = 1234567890123,\n",
		"\t\t\t\t[1] = \"a\",\n",
		"\t\t\t[10] = \"sparse\",\n",
		"TestAddonGlobal = 42\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
	sv2, err := ParseLua(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	for i := range sv1.Vars {
		a, b := LuaToJSON(sv1.Vars[i].Value, -1), LuaToJSON(sv2.Vars[i].Value, -1)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s differs after round trip:\n%v\n%v", sv1.Vars[i].Name, a, b)
		}
	}
	if string(sv2.Bytes()) != string(out) {
		t.Error("second serialisation differs from the first")
	}
}

func TestLuaParserEdgeCases(t *testing.T) {
	src := "A = { 1, 2; 3 } -- trailing\nB = \"x\\65\\x41\\u00e9\" C = {[\"k\"]=nil} D = 0x10 E = [[long\nstring]] F = -0.5 --[[ block\ncomment ]] G = 0/0\n"
	sv, err := ParseLua([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := sv.Get("A")
	if !a.(*LuaTable).IsArray() || len(a.(*LuaTable).Entries) != 3 {
		t.Errorf("A = %+v", a)
	}
	if b, _ := sv.Get("B"); b != "xAAu00e9" {
		t.Errorf("B = %q", b)
	}
	if d, _ := sv.Get("D"); d != 16.0 {
		t.Errorf("D = %v", d)
	}
	if e, _ := sv.Get("E"); e != "long\nstring" {
		t.Errorf("E = %q", e)
	}
	if f, _ := sv.Get("F"); f != -0.5 {
		t.Errorf("F = %v", f)
	}
	if g, _ := sv.Get("G"); !math.IsNaN(g.(float64)) {
		t.Errorf("G = %v", g)
	}
	for _, bad := range []string{"A = ", "A = {", "A = \"open", "A = foo", "= 1", "A = {[nil] = 1}", "A = { [1] 2 }"} {
		if _, err := ParseLua([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if sv, err := ParseLua(nil); err != nil || len(sv.Vars) != 0 {
		t.Errorf("empty = %v %v", sv, err)
	}
}

func TestLuaToJSONShapes(t *testing.T) {
	arr := &LuaTable{Entries: []LuaEntry{{Key: 1.0, Value: "a"}, {Key: 2.0, Value: "b"}}}
	if got := LuaToJSON(arr, -1); !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("array = %#v", got)
	}
	sparse := &LuaTable{Entries: []LuaEntry{{Key: 1.0, Value: "a"}, {Key: 3.0, Value: "c"}}}
	if got := LuaToJSON(sparse, -1); !reflect.DeepEqual(got, map[string]any{"1": "a", "3": "c"}) {
		t.Errorf("sparse = %#v", got)
	}
	nested := &LuaTable{Entries: []LuaEntry{{Key: "in", Value: arr}, {Key: "n", Value: math.NaN()}, {Key: 2.5, Value: math.Inf(-1)}}}
	got := LuaToJSON(nested, 1).(map[string]any)
	if got["in"] != "<table: 2 entries>" || got["n"] != "nan" || got["2.5"] != "-inf" {
		t.Errorf("depth-limited = %#v", got)
	}
	if got := LuaToJSON(&LuaTable{}, -1); !reflect.DeepEqual(got, map[string]any{}) {
		t.Errorf("empty = %#v", got)
	}
}

func TestJSONToLua(t *testing.T) {
	v, err := JSONToLua([]byte(`{"b":1,"a":[true,null,"x"],"3":2.5,"-1":"neg"}`))
	if err != nil {
		t.Fatal(err)
	}
	sv := &SavedVariables{Vars: []LuaVar{{Name: "X", Value: v}}}
	want := "X = {\n\t[-1] = \"neg\",\n\t[3] = 2.5,\n\t[\"a\"] = {\n\t\t[1] = true,\n\t\t[2] = nil,\n\t\t[3] = \"x\",\n\t},\n\t[\"b\"] = 1,\n}\n"
	if got := string(sv.Bytes()); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	for _, bad := range []string{"", "{", "1 2", "nope"} {
		if _, err := JSONToLua([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if s, err := JSONToLua([]byte(`"str"`)); err != nil || s != "str" {
		t.Errorf("scalar = %v %v", s, err)
	}
}

func TestSetAndDeletePath(t *testing.T) {
	sv, err := ParseLua([]byte("A = {\n\t[\"x\"] = 1,\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if prev, existed, err := sv.SetPath([]string{"A", "x"}, 2.0); err != nil || !existed || prev != 1.0 {
		t.Errorf("set existing = %v %v %v", prev, existed, err)
	}
	if _, existed, err := sv.SetPath([]string{"A", "y", "z"}, "deep"); err != nil || existed {
		t.Errorf("set deep = %v %v", existed, err)
	}
	if v, ok := sv.Lookup([]string{"A", "y", "z"}); !ok || v != "deep" {
		t.Errorf("deep lookup = %v %v", v, ok)
	}
	if _, _, err := sv.SetPath([]string{"B"}, true); err != nil || !reflect.DeepEqual(sv.Names(), []string{"A", "B"}) {
		t.Errorf("new var: %v %v", sv.Names(), err)
	}
	if _, _, err := sv.SetPath([]string{"A", "x", "w"}, 1.0); err == nil || !strings.Contains(err.Error(), "not a table") {
		t.Errorf("through scalar = %v", err)
	}
	if _, _, err := sv.SetPath([]string{"B", "k"}, 1.0); err == nil {
		t.Error("through boolean var accepted")
	}
	if _, _, err := sv.SetPath([]string{"A", "5"}, "five"); err != nil || !strings.Contains(string(sv.Bytes()), "\t[5] = \"five\",\n") {
		t.Errorf("numeric key: %v\n%s", err, sv.Bytes())
	}
	if _, _, err := sv.SetPath([]string{"C", "k"}, 1.0); err != nil || !strings.Contains(string(sv.Bytes()), "C = {\n\t[\"k\"] = 1,\n}\n") {
		t.Errorf("new table var: %v\n%s", err, sv.Bytes())
	}
	if prev, existed, err := sv.DeletePath([]string{"A", "y"}); err != nil || !existed || prev == nil {
		t.Errorf("delete = %v %v %v", prev, existed, err)
	}
	if _, ok := sv.Lookup([]string{"A", "y"}); ok {
		t.Error("y still there")
	}
	if _, existed, err := sv.DeletePath([]string{"Nope", "x"}); err != nil || existed {
		t.Errorf("delete missing = %v %v", existed, err)
	}
	if _, existed, _ := sv.DeletePath([]string{"B"}); !existed || !reflect.DeepEqual(sv.Names(), []string{"A", "C"}) {
		t.Errorf("delete var: %v", sv.Names())
	}
	if _, _, err := sv.SetPath(nil, 1.0); err == nil {
		t.Error("empty path accepted")
	}
}

func TestParsePath(t *testing.T) {
	if p, err := ParsePath("a.b.c"); err != nil || !reflect.DeepEqual(p, []string{"a", "b", "c"}) {
		t.Errorf("dotted = %v %v", p, err)
	}
	if p, err := ParsePath(`["a","x.y",3]`); err != nil || !reflect.DeepEqual(p, []string{"a", "x.y", "3"}) {
		t.Errorf("json = %v %v", p, err)
	}
	for _, bad := range []string{"", "a..b", "[", "[]", `[{}]`, "   "} {
		if _, err := ParsePath(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestQuoteAndNumber(t *testing.T) {
	if got := QuoteLua("a\"b\\c\nd\x01é\r\t"); got != `"a\"b\\c\nd\001é\r\t"` {
		t.Errorf("quote = %s", got)
	}
	cases := map[float64]string{3: "3", 2.5: "2.5", 1e21: "1e+21", -7: "-7", 1e-05: "1e-05", math.Inf(1): "1/0", math.Inf(-1): "-1/0"}
	for in, want := range cases {
		if got := FormatLuaNumber(in); got != want {
			t.Errorf("FormatLuaNumber(%v) = %s, want %s", in, got, want)
		}
	}
	if FormatLuaNumber(math.NaN()) != "0/0" {
		t.Error("nan")
	}
}

func TestWriteFileSafely(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.txt")
	backup, err := WriteFileSafely(p, []byte("one"))
	if err != nil || backup != "" {
		t.Fatalf("first write: %v %q", err, backup)
	}
	backup, err = WriteFileSafely(p, []byte("two"))
	if err != nil || backup == "" || !strings.HasSuffix(backup, BackupSuffix) {
		t.Fatalf("second write: %v %q", err, backup)
	}
	if b, _ := os.ReadFile(backup); string(b) != "one" {
		t.Error("backup content")
	}
	if b, _ := os.ReadFile(p); string(b) != "two" {
		t.Error("new content")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 2 {
		t.Errorf("leftover files: %v", entries)
	}
	if bs, err := ListBackups(p); err != nil || len(bs) != 1 || bs[0] != backup {
		t.Errorf("ListBackups = %v %v", bs, err)
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"Foo", "Realm Name", "123#1", "Zul'jin"} {
		if err := ValidateName("x", ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "a/b", "..", `a\b`, "x\x00y", "a..b"} {
		if err := ValidateName("x", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateRelPath("p", "a/../b"); err == nil {
		t.Error("dotdot rel path accepted")
	}
	if err := ValidateRelPath("p", "/abs"); err == nil {
		t.Error("absolute rel path accepted")
	}
	if err := ValidateRelPath("p", "locales/enUS.lua"); err != nil {
		t.Error(err)
	}
}

func TestLuaIdentifierGuard(t *testing.T) {
	for _, ok := range []string{"MyAddonDB", "_x", "a1", "Blizzard_X"} {
		if err := ValidateLuaIdentifier(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "1abc", "a b", "a.b", "end", "nil", "X = 1 print(\"pwn\") --", "a-b", "é"} {
		if err := ValidateLuaIdentifier(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	sv := &SavedVariables{Path: filepath.Join(t.TempDir(), "X.lua")}
	if _, _, err := sv.SetPath([]string{"X = 1 print(\"pwn\") --", "k"}, 1.0); err == nil {
		t.Fatal("injection via variable name accepted by SetPath")
	}
	if len(sv.Vars) != 0 {
		t.Fatal("rejected SetPath still added a variable")
	}
	sv.Vars = append(sv.Vars, LuaVar{Name: "bad name", Value: 1.0})
	if _, err := sv.Save(); err == nil {
		t.Fatal("Save wrote a non-identifier variable name")
	}
	if _, err := os.Stat(sv.Path); err == nil {
		t.Fatal("file was written despite the rejected name")
	}
}
