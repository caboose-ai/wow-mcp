package wow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Lua values are represented as:
//
//	nil      -> nil
//	boolean  -> bool
//	number   -> float64 (integers included; ±Inf and NaN survive as 1/0, -1/0, 0/0)
//	string   -> string
//	table    -> *LuaTable, an ordered list of key/value entries
//
// Keys may be float64, string or bool.

// LuaTable keeps entries in file order so a rewrite changes as little as possible.
type LuaTable struct {
	Entries []LuaEntry
}

// LuaEntry is one key/value pair of a LuaTable.
type LuaEntry struct {
	Key   any
	Value any
}

// LuaVar is one top-level "Name = value" statement of a SavedVariables file.
type LuaVar struct {
	Name  string
	Value any
}

// SavedVariables is a parsed SavedVariables .lua file.
type SavedVariables struct {
	Path string
	Vars []LuaVar
	eol  string
}

// Names lists the top-level variables in file order.
func (sv *SavedVariables) Names() []string {
	out := make([]string, len(sv.Vars))
	for i, v := range sv.Vars {
		out[i] = v.Name
	}
	return out
}

// Get returns a top-level variable.
func (sv *SavedVariables) Get(name string) (any, bool) {
	for _, v := range sv.Vars {
		if v.Name == name {
			return v.Value, true
		}
	}
	return nil, false
}

// LoadSavedVariables reads and parses path.
func LoadSavedVariables(path string) (*SavedVariables, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sv, err := ParseLua(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	sv.Path = path
	return sv, nil
}

// Save writes the file back in the client's own layout through WriteFileSafely.
// Every top-level name is checked to be a plain Lua identifier first: the
// client executes this file as Lua, so a name is the one place where text
// would otherwise reach the file unescaped.
func (sv *SavedVariables) Save() (string, error) {
	for _, v := range sv.Vars {
		if err := ValidateLuaIdentifier(v.Name); err != nil {
			return "", err
		}
	}
	return WriteFileSafely(sv.Path, sv.Bytes())
}

var luaIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var luaReserved = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true, "end": true,
	"false": true, "for": true, "function": true, "goto": true, "if": true, "in": true,
	"local": true, "nil": true, "not": true, "or": true, "repeat": true, "return": true,
	"then": true, "true": true, "until": true, "while": true,
}

// ValidateLuaIdentifier accepts only names that can stand on the left of
// "=" in a SavedVariables file: letters, digits and underscores, not starting
// with a digit, and not a Lua reserved word.
func ValidateLuaIdentifier(name string) error {
	if !luaIdent.MatchString(name) {
		return fmt.Errorf("variable name %q is not a Lua identifier (letters, digits and _ only)", name)
	}
	if luaReserved[name] {
		return fmt.Errorf("variable name %q is a Lua reserved word", name)
	}
	return nil
}

// ---------------------------------------------------------------- lexer

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNumber
	tString
	tLBrace
	tRBrace
	tLBracket
	tRBracket
	tAssign
	tComma
	tSemi
	tMinus
	tSlash
)

type token struct {
	kind tokKind
	text string
	num  float64
	line int
}

type lexer struct {
	src  []byte
	pos  int
	line int
	peek *token
}

func (lx *lexer) errf(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", lx.line, fmt.Sprintf(format, args...))
}

func (lx *lexer) skipSpaceAndComments() error {
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch {
		case c == '\n':
			lx.line++
			lx.pos++
		case c == ' ' || c == '\t' || c == '\r':
			lx.pos++
		case c == '-' && lx.pos+1 < len(lx.src) && lx.src[lx.pos+1] == '-':
			lx.pos += 2
			if lx.pos < len(lx.src) && lx.src[lx.pos] == '[' {
				if level, ok := lx.longBracketOpen(); ok {
					if _, err := lx.readLongBracket(level); err != nil {
						return err
					}
					continue
				}
			}
			for lx.pos < len(lx.src) && lx.src[lx.pos] != '\n' {
				lx.pos++
			}
		default:
			return nil
		}
	}
	return nil
}

// longBracketOpen checks for "[" "="* "[" at pos and consumes it.
func (lx *lexer) longBracketOpen() (level int, ok bool) {
	p := lx.pos
	if p >= len(lx.src) || lx.src[p] != '[' {
		return 0, false
	}
	p++
	for p < len(lx.src) && lx.src[p] == '=' {
		level++
		p++
	}
	if p < len(lx.src) && lx.src[p] == '[' {
		lx.pos = p + 1
		return level, true
	}
	return 0, false
}

// readLongBracket reads up to and including the matching "]" "="* "]".
func (lx *lexer) readLongBracket(level int) (string, error) {
	closer := "]" + strings.Repeat("=", level) + "]"
	i := bytes.Index(lx.src[lx.pos:], []byte(closer))
	if i < 0 {
		return "", lx.errf("unterminated long bracket")
	}
	s := string(lx.src[lx.pos : lx.pos+i])
	lx.line += strings.Count(s, "\n")
	lx.pos += i + len(closer)
	// Lua drops a first newline directly after the opening bracket.
	s = strings.TrimPrefix(s, "\r\n")
	s = strings.TrimPrefix(s, "\n")
	return s, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (lx *lexer) next() (token, error) {
	if lx.peek != nil {
		t := *lx.peek
		lx.peek = nil
		return t, nil
	}
	if err := lx.skipSpaceAndComments(); err != nil {
		return token{}, err
	}
	if lx.pos >= len(lx.src) {
		return token{kind: tEOF, line: lx.line}, nil
	}
	c := lx.src[lx.pos]
	start := lx.pos
	switch {
	case isIdentStart(c):
		for lx.pos < len(lx.src) && (isIdentStart(lx.src[lx.pos]) || isDigit(lx.src[lx.pos])) {
			lx.pos++
		}
		return token{kind: tIdent, text: string(lx.src[start:lx.pos]), line: lx.line}, nil
	case isDigit(c) || (c == '.' && lx.pos+1 < len(lx.src) && isDigit(lx.src[lx.pos+1])):
		return lx.number()
	case c == '"' || c == '\'':
		return lx.quoted(c)
	case c == '[':
		if level, ok := lx.longBracketOpen(); ok {
			s, err := lx.readLongBracket(level)
			if err != nil {
				return token{}, err
			}
			return token{kind: tString, text: s, line: lx.line}, nil
		}
		lx.pos++
		return token{kind: tLBracket, line: lx.line}, nil
	}
	lx.pos++
	single := map[byte]tokKind{'{': tLBrace, '}': tRBrace, ']': tRBracket, '=': tAssign, ',': tComma, ';': tSemi, '-': tMinus, '/': tSlash}
	if k, ok := single[c]; ok {
		return token{kind: k, line: lx.line}, nil
	}
	return token{}, lx.errf("unexpected character %q", c)
}

func (lx *lexer) unread(t token) { lx.peek = &t }

func (lx *lexer) number() (token, error) {
	start := lx.pos
	src := lx.src
	if src[lx.pos] == '0' && lx.pos+1 < len(src) && (src[lx.pos+1] == 'x' || src[lx.pos+1] == 'X') {
		lx.pos += 2
		for lx.pos < len(src) && (isDigit(src[lx.pos]) || (src[lx.pos]|0x20 >= 'a' && src[lx.pos]|0x20 <= 'f')) {
			lx.pos++
		}
		n, err := strconv.ParseInt(string(src[start:lx.pos]), 0, 64)
		if err != nil {
			return token{}, lx.errf("bad hex number %q", src[start:lx.pos])
		}
		return token{kind: tNumber, num: float64(n), text: string(src[start:lx.pos]), line: lx.line}, nil
	}
	for lx.pos < len(src) {
		c := src[lx.pos]
		if isDigit(c) || c == '.' {
			lx.pos++
			continue
		}
		if (c == 'e' || c == 'E') && lx.pos+1 < len(src) {
			lx.pos++
			if src[lx.pos] == '+' || src[lx.pos] == '-' {
				lx.pos++
			}
			continue
		}
		break
	}
	text := string(src[start:lx.pos])
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return token{}, lx.errf("bad number %q", text)
	}
	return token{kind: tNumber, num: f, text: text, line: lx.line}, nil
}

func (lx *lexer) quoted(q byte) (token, error) {
	lx.pos++ // opening quote
	var sb strings.Builder
	for {
		if lx.pos >= len(lx.src) {
			return token{}, lx.errf("unterminated string")
		}
		c := lx.src[lx.pos]
		switch c {
		case q:
			lx.pos++
			return token{kind: tString, text: sb.String(), line: lx.line}, nil
		case '\n':
			return token{}, lx.errf("newline in string")
		case '\\':
			lx.pos++
			if lx.pos >= len(lx.src) {
				return token{}, lx.errf("unterminated escape")
			}
			e := lx.src[lx.pos]
			switch e {
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'a':
				sb.WriteByte('\a')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'v':
				sb.WriteByte('\v')
			case '\\', '"', '\'', '|':
				sb.WriteByte(e)
			case '\n':
				sb.WriteByte('\n')
				lx.line++
			case 'x':
				if lx.pos+2 >= len(lx.src) {
					return token{}, lx.errf("short \\x escape")
				}
				n, err := strconv.ParseUint(string(lx.src[lx.pos+1:lx.pos+3]), 16, 8)
				if err != nil {
					return token{}, lx.errf("bad \\x escape")
				}
				sb.WriteByte(byte(n))
				lx.pos += 2
			case 'z':
				for lx.pos+1 < len(lx.src) && (lx.src[lx.pos+1] == ' ' || lx.src[lx.pos+1] == '\t' || lx.src[lx.pos+1] == '\n' || lx.src[lx.pos+1] == '\r') {
					if lx.src[lx.pos+1] == '\n' {
						lx.line++
					}
					lx.pos++
				}
			default:
				if isDigit(e) {
					end := lx.pos
					for end < len(lx.src) && end < lx.pos+3 && isDigit(lx.src[end]) {
						end++
					}
					n, _ := strconv.Atoi(string(lx.src[lx.pos:end]))
					if n > 255 {
						return token{}, lx.errf("decimal escape out of range")
					}
					sb.WriteByte(byte(n))
					lx.pos = end - 1
				} else {
					// Unknown escape: keep the character, as the client's own
					// deserializer is lenient here.
					sb.WriteByte(e)
				}
			}
			lx.pos++
		default:
			sb.WriteByte(c)
			lx.pos++
		}
	}
}

// ---------------------------------------------------------------- parser

type parser struct{ lx *lexer }

// ParseLua parses the subset of Lua the client writes to SavedVariables:
// a sequence of "Name = value" statements where values are nil, booleans,
// numbers, strings and table constructors.
func ParseLua(src []byte) (*SavedVariables, error) {
	p := &parser{lx: &lexer{src: src, line: 1}}
	sv := &SavedVariables{eol: DetectEOL(src)}
	for {
		t, err := p.lx.next()
		if err != nil {
			return nil, err
		}
		switch t.kind {
		case tEOF:
			return sv, nil
		case tSemi:
			continue
		case tIdent:
		default:
			return nil, p.lx.errf("expected a variable name")
		}
		eq, err := p.lx.next()
		if err != nil {
			return nil, err
		}
		if eq.kind != tAssign {
			return nil, p.lx.errf("expected '=' after %s", t.text)
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		sv.Vars = append(sv.Vars, LuaVar{Name: t.text, Value: v})
	}
}

func (p *parser) value() (any, error) {
	t, err := p.lx.next()
	if err != nil {
		return nil, err
	}
	return p.valueFrom(t)
}

func (p *parser) valueFrom(t token) (any, error) {
	switch t.kind {
	case tIdent:
		switch t.text {
		case "nil":
			return nil, nil
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, p.lx.errf("unexpected identifier %q (only literal values are supported)", t.text)
	case tString:
		return t.text, nil
	case tNumber:
		return p.numberTail(t.num)
	case tMinus:
		n, err := p.lx.next()
		if err != nil {
			return nil, err
		}
		if n.kind != tNumber {
			return nil, p.lx.errf("expected a number after '-'")
		}
		v, err := p.numberTail(n.num)
		if err != nil {
			return nil, err
		}
		return -v.(float64), nil
	case tLBrace:
		return p.table()
	}
	return nil, p.lx.errf("unexpected token")
}

// numberTail handles the "1/0" and "0/0" spellings the client uses for
// infinity and NaN.
func (p *parser) numberTail(n float64) (any, error) {
	t, err := p.lx.next()
	if err != nil {
		return nil, err
	}
	if t.kind != tSlash {
		p.lx.unread(t)
		return n, nil
	}
	d, err := p.lx.next()
	if err != nil {
		return nil, err
	}
	if d.kind != tNumber {
		return nil, p.lx.errf("expected a number after '/'")
	}
	return n / d.num, nil
}

func (p *parser) table() (*LuaTable, error) {
	tbl := &LuaTable{}
	n := 0
	for {
		t, err := p.lx.next()
		if err != nil {
			return nil, err
		}
		switch t.kind {
		case tRBrace:
			return tbl, nil
		case tComma, tSemi:
			continue
		case tLBracket:
			k, err := p.value()
			if err != nil {
				return nil, err
			}
			if k == nil {
				return nil, p.lx.errf("nil table key")
			}
			if rb, err := p.lx.next(); err != nil {
				return nil, err
			} else if rb.kind != tRBracket {
				return nil, p.lx.errf("expected ']'")
			}
			if eq, err := p.lx.next(); err != nil {
				return nil, err
			} else if eq.kind != tAssign {
				return nil, p.lx.errf("expected '=' after table key")
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			tbl.Entries = append(tbl.Entries, LuaEntry{Key: k, Value: v})
		case tIdent:
			nx, err := p.lx.next()
			if err != nil {
				return nil, err
			}
			if nx.kind == tAssign {
				v, err := p.value()
				if err != nil {
					return nil, err
				}
				tbl.Entries = append(tbl.Entries, LuaEntry{Key: t.text, Value: v})
				continue
			}
			p.lx.unread(nx)
			v, err := p.valueFrom(t)
			if err != nil {
				return nil, err
			}
			n++
			tbl.Entries = append(tbl.Entries, LuaEntry{Key: float64(n), Value: v})
		default:
			v, err := p.valueFrom(t)
			if err != nil {
				return nil, err
			}
			n++
			tbl.Entries = append(tbl.Entries, LuaEntry{Key: float64(n), Value: v})
		}
	}
}

// ---------------------------------------------------------------- writer

// Bytes renders the file the way the client does: tab indentation, every
// key bracketed, one entry per line, trailing commas.
func (sv *SavedVariables) Bytes() []byte {
	eol := sv.eol
	if eol == "" {
		eol = "\n"
	}
	var b bytes.Buffer
	for _, v := range sv.Vars {
		b.WriteString(v.Name)
		b.WriteString(" = ")
		writeLua(&b, v.Value, 0, eol)
		b.WriteString(eol)
	}
	return b.Bytes()
}

func writeLua(b *bytes.Buffer, v any, indent int, eol string) {
	switch x := v.(type) {
	case nil:
		b.WriteString("nil")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case float64:
		b.WriteString(FormatLuaNumber(x))
	case string:
		b.WriteString(QuoteLua(x))
	case *LuaTable:
		b.WriteString("{")
		b.WriteString(eol)
		for _, e := range x.Entries {
			for i := 0; i <= indent; i++ {
				b.WriteByte('\t')
			}
			b.WriteByte('[')
			writeLua(b, e.Key, indent+1, eol)
			b.WriteString("] = ")
			writeLua(b, e.Value, indent+1, eol)
			b.WriteByte(',')
			b.WriteString(eol)
		}
		for i := 0; i < indent; i++ {
			b.WriteByte('\t')
		}
		b.WriteString("}")
	default:
		b.WriteString(QuoteLua(fmt.Sprint(x)))
	}
}

// FormatLuaNumber prints integers without a fraction and uses the client's
// 1/0, -1/0 and 0/0 spellings for the non-finite values.
func FormatLuaNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "0/0"
	case math.IsInf(f, 1):
		return "1/0"
	case math.IsInf(f, -1):
		return "-1/0"
	case f == math.Trunc(f) && math.Abs(f) < 1e15:
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// QuoteLua writes a double-quoted Lua string literal.
func QuoteLua(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\':
			sb.WriteString(`\\`)
		case c == '"':
			sb.WriteString(`\"`)
		case c == '\n':
			sb.WriteString(`\n`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\t':
			sb.WriteString(`\t`)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&sb, `\%03d`, c)
		case c >= 0x80:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				fmt.Fprintf(&sb, `\%03d`, c)
			} else {
				sb.WriteString(s[i : i+size])
			}
			i += size
			continue
		default:
			sb.WriteByte(c)
		}
		i++
	}
	sb.WriteByte('"')
	return sb.String()
}

// ---------------------------------------------------------------- JSON bridge

// KeyString renders a table key the way paths and JSON object keys spell it.
func KeyString(k any) string {
	switch x := k.(type) {
	case string:
		return x
	case float64:
		return FormatLuaNumber(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return fmt.Sprint(k)
}

// IsArray reports whether the table's keys are exactly 1..n.
func (t *LuaTable) IsArray() bool {
	if len(t.Entries) == 0 {
		return false
	}
	seen := make([]bool, len(t.Entries))
	for _, e := range t.Entries {
		f, ok := e.Key.(float64)
		if !ok || f != math.Trunc(f) || f < 1 || f > float64(len(t.Entries)) || seen[int(f)-1] {
			return false
		}
		seen[int(f)-1] = true
	}
	return true
}

// LuaToJSON converts a Lua value into JSON-marshalable Go values. Tables with
// keys 1..n become arrays, other tables become objects with string keys.
// depth limits nesting: tables below the limit are replaced by a summary
// string; depth < 0 means unlimited. Non-finite numbers become the strings
// "inf", "-inf" and "nan".
func LuaToJSON(v any, depth int) any {
	switch x := v.(type) {
	case *LuaTable:
		if depth == 0 {
			return fmt.Sprintf("<table: %d entries>", len(x.Entries))
		}
		if x.IsArray() {
			out := make([]any, len(x.Entries))
			for _, e := range x.Entries {
				out[int(e.Key.(float64))-1] = LuaToJSON(e.Value, depth-1)
			}
			return out
		}
		out := make(map[string]any, len(x.Entries))
		for _, e := range x.Entries {
			out[KeyString(e.Key)] = LuaToJSON(e.Value, depth-1)
		}
		return out
	case float64:
		switch {
		case math.IsNaN(x):
			return "nan"
		case math.IsInf(x, 1):
			return "inf"
		case math.IsInf(x, -1):
			return "-inf"
		}
		return x
	}
	return v
}

var intLike = regexp.MustCompile(`^-?\d+$`)

// JSONToLua converts decoded JSON into Lua values. Arrays get keys 1..n;
// object keys that look like integers become Lua numbers, because that is how
// LuaToJSON spells numeric keys. The strings "inf", "-inf" and "nan" stay
// strings; use 1/0 spellings only through a Lua file.
func JSONToLua(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("value is not valid JSON: %w", err)
	}
	if dec.More() {
		return nil, errors.New("value has trailing data after the JSON")
	}
	return fromJSON(v), nil
}

func fromJSON(v any) any {
	switch x := v.(type) {
	case nil, bool, string:
		return x
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return x.String()
		}
		return f
	case float64:
		return x
	case []any:
		t := &LuaTable{}
		for i, e := range x {
			t.Entries = append(t.Entries, LuaEntry{Key: float64(i + 1), Value: fromJSON(e)})
		}
		return t
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t := &LuaTable{}
		for _, k := range keys {
			var key any = k
			if intLike.MatchString(k) {
				if n, err := strconv.ParseFloat(k, 64); err == nil {
					key = n
				}
			}
			t.Entries = append(t.Entries, LuaEntry{Key: key, Value: fromJSON(x[k])})
		}
		return t
	}
	return fmt.Sprint(v)
}

// ---------------------------------------------------------------- paths

// ParsePath splits a path into segments. A path is either a JSON array of
// strings and numbers (for keys that contain dots) or a dot-separated string.
// The first segment is always the top-level variable name.
func ParsePath(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("path is empty")
	}
	if strings.HasPrefix(s, "[") {
		var raw []any
		if err := json.Unmarshal([]byte(s), &raw); err != nil {
			return nil, fmt.Errorf("path is not a JSON array: %w", err)
		}
		if len(raw) == 0 {
			return nil, errors.New("path is empty")
		}
		out := make([]string, len(raw))
		for i, e := range raw {
			switch x := e.(type) {
			case string:
				out[i] = x
			case float64:
				out[i] = FormatLuaNumber(x)
			default:
				return nil, fmt.Errorf("path segment %d must be a string or number", i)
			}
		}
		return out, nil
	}
	parts := strings.Split(s, ".")
	for i, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("path %q has an empty segment %d", s, i)
		}
	}
	return parts, nil
}

func (t *LuaTable) find(seg string) int {
	for i, e := range t.Entries {
		if KeyString(e.Key) == seg {
			return i
		}
	}
	return -1
}

func keyFromSegment(seg string) any {
	if intLike.MatchString(seg) {
		if n, err := strconv.ParseFloat(seg, 64); err == nil {
			return n
		}
	}
	return seg
}

// Lookup returns the value at path, or ok=false when any segment is missing.
func (sv *SavedVariables) Lookup(path []string) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}
	cur, ok := sv.Get(path[0])
	if !ok {
		return nil, false
	}
	for _, seg := range path[1:] {
		t, isTable := cur.(*LuaTable)
		if !isTable {
			return nil, false
		}
		i := t.find(seg)
		if i < 0 {
			return nil, false
		}
		cur = t.Entries[i].Value
	}
	return cur, true
}

// SetPath stores v at path, creating the top-level variable and any missing
// intermediate tables. It fails when an intermediate value exists but is not
// a table. Returns the previous value and whether it existed.
func (sv *SavedVariables) SetPath(path []string, v any) (previous any, existed bool, err error) {
	if len(path) == 0 {
		return nil, false, errors.New("path is empty")
	}
	if err := ValidateLuaIdentifier(path[0]); err != nil {
		return nil, false, err
	}
	vi := -1
	for i := range sv.Vars {
		if sv.Vars[i].Name == path[0] {
			vi = i
			break
		}
	}
	if len(path) == 1 {
		if vi >= 0 {
			prev := sv.Vars[vi].Value
			sv.Vars[vi].Value = v
			return prev, true, nil
		}
		sv.Vars = append(sv.Vars, LuaVar{Name: path[0], Value: v})
		return nil, false, nil
	}
	if vi < 0 {
		sv.Vars = append(sv.Vars, LuaVar{Name: path[0], Value: &LuaTable{}})
		vi = len(sv.Vars) - 1
	}
	cur, ok := sv.Vars[vi].Value.(*LuaTable)
	if !ok {
		if sv.Vars[vi].Value != nil {
			return nil, false, fmt.Errorf("%s is a %s, not a table", path[0], luaTypeName(sv.Vars[vi].Value))
		}
		cur = &LuaTable{}
		sv.Vars[vi].Value = cur
	}
	for i, seg := range path[1 : len(path)-1] {
		idx := cur.find(seg)
		if idx < 0 {
			nt := &LuaTable{}
			cur.Entries = append(cur.Entries, LuaEntry{Key: keyFromSegment(seg), Value: nt})
			cur = nt
			continue
		}
		nt, ok := cur.Entries[idx].Value.(*LuaTable)
		if !ok {
			return nil, false, fmt.Errorf("%s is a %s, not a table", strings.Join(path[:i+2], "."), luaTypeName(cur.Entries[idx].Value))
		}
		cur = nt
	}
	last := path[len(path)-1]
	if idx := cur.find(last); idx >= 0 {
		prev := cur.Entries[idx].Value
		cur.Entries[idx].Value = v
		return prev, true, nil
	}
	cur.Entries = append(cur.Entries, LuaEntry{Key: keyFromSegment(last), Value: v})
	return nil, false, nil
}

// DeletePath removes the value at path and returns it. A missing path is not
// an error; existed reports whether anything was removed.
func (sv *SavedVariables) DeletePath(path []string) (previous any, existed bool, err error) {
	if len(path) == 0 {
		return nil, false, errors.New("path is empty")
	}
	vi := -1
	for i := range sv.Vars {
		if sv.Vars[i].Name == path[0] {
			vi = i
			break
		}
	}
	if vi < 0 {
		return nil, false, nil
	}
	if len(path) == 1 {
		prev := sv.Vars[vi].Value
		sv.Vars = append(sv.Vars[:vi], sv.Vars[vi+1:]...)
		return prev, true, nil
	}
	cur, ok := sv.Vars[vi].Value.(*LuaTable)
	if !ok {
		return nil, false, nil
	}
	for _, seg := range path[1 : len(path)-1] {
		idx := cur.find(seg)
		if idx < 0 {
			return nil, false, nil
		}
		cur, ok = cur.Entries[idx].Value.(*LuaTable)
		if !ok {
			return nil, false, nil
		}
	}
	idx := cur.find(path[len(path)-1])
	if idx < 0 {
		return nil, false, nil
	}
	prev := cur.Entries[idx].Value
	cur.Entries = append(cur.Entries[:idx], cur.Entries[idx+1:]...)
	return prev, true, nil
}

func luaTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case *LuaTable:
		return "table"
	}
	return fmt.Sprintf("%T", v)
}
