// Package wow reads and edits the on-disk state of a World of Warcraft
// install: the AddOns folder, the WTF configuration tree, and the Lua
// SavedVariables files the client writes on logout.
//
// Every write in this package goes through WriteFileSafely, which keeps a
// timestamped backup next to the file and replaces it atomically. Callers
// should refuse to write while the game client is running (see WoWRunning):
// the client rewrites WTF on logout and would discard the edit.
package wow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// BackupSuffix ends every backup file this package writes.
const BackupSuffix = ".wow-mcp.bak"

// ValidateName rejects empty or path-like names so that a tool argument can
// never escape the install tree. kind names the argument in the error.
func ValidateName(kind, s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is empty", kind)
	}
	if strings.ContainsAny(s, `/\`) || strings.Contains(s, "..") {
		return fmt.Errorf("%s %q must not contain path separators or '..'", kind, s)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s %q contains control characters", kind, s)
		}
	}
	return nil
}

// ValidateRelPath checks a slash-separated path that must stay inside a
// directory: no absolute paths, no "..", no empty segments.
func ValidateRelPath(kind, s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s is empty", kind)
	}
	if filepath.IsAbs(s) || strings.HasPrefix(s, "/") || strings.HasPrefix(s, `\`) {
		return fmt.Errorf("%s %q must be relative", kind, s)
	}
	for _, seg := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return fmt.Errorf("%s %q must not contain '..'", kind, s)
		}
	}
	return nil
}

// DetectEOL returns "\r\n" when the data uses Windows line endings, else "\n".
func DetectEOL(b []byte) string {
	if bytes.Contains(b, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// SplitLines splits on either line ending and drops a trailing empty line.
func SplitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// WriteFileSafely replaces path with data. When the file already exists its
// previous content is first copied to a sibling backup named
// "<file>.<UTC timestamp>.wow-mcp.bak", and the backup path is returned.
// The new content is written to a temp file in the same directory and
// renamed into place so a crash can never leave a half-written file.
func WriteFileSafely(path string, data []byte) (backup string, err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	st, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		mode = st.Mode().Perm()
		old, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s for backup: %w", path, err)
		}
		backup = fmt.Sprintf("%s.%s%s", path, time.Now().UTC().Format("20060102T150405.000Z"), BackupSuffix)
		if err := os.WriteFile(backup, old, mode); err != nil {
			return "", fmt.Errorf("write backup %s: %w", backup, err)
		}
	case errors.Is(statErr, os.ErrNotExist):
		// first write of a new file: nothing to back up
	default:
		return "", statErr
	}

	tmp, err := os.CreateTemp(dir, ".wow-mcp-*.tmp")
	if err != nil {
		return backup, err
	}
	tmpName := tmp.Name()
	fail := func(e error) (string, error) {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return backup, e
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	_ = tmp.Chmod(mode) // best effort; Windows only honours the read-only bit
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return backup, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return backup, err
	}
	return backup, nil
}

// ListBackups returns the backup files WriteFileSafely has left next to path,
// newest first.
func ListBackups(path string) ([]string, error) {
	matches, err := filepath.Glob(path + ".*" + BackupSuffix)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
		matches[i], matches[j] = matches[j], matches[i]
	}
	return matches, nil
}
