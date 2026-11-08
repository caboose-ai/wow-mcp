package wow

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// exeName matches the executables the WoW clients ship: Wow.exe, WowClassic.exe,
// WowT.exe (PTR), WowB.exe (beta), Wow-64.exe (old 64-bit builds). WowUp.exe
// and other tools do not match.
var exeName = regexp.MustCompile(`(?i)(^|[\\/])wow(classic|t|b|f|-64)?\.exe$`)

// exeInLine finds the same executables inside a full command line, where a
// Wine path such as "C:\Program Files (x86)\...\Wow.exe -launcherlogin"
// contains spaces, so the first word alone is not enough.
var exeInLine = regexp.MustCompile(`(?i)(^|[\/])wow(classic|t|b|f|-64)?\.exe(\s|$)`)

// macBinary matches the binary inside the macOS app bundle.
var macBinary = regexp.MustCompile(`/Contents/MacOS/World of Warcraft( Classic| Beta| Test)?(\s|$)`)

// WoWRunning reports whether a WoW client process appears to be running on
// this machine. It is best effort: a non-nil error means "could not tell",
// and callers should treat that as unknown rather than as "not running".
//
// Inside WSL the Linux process list cannot see Windows programs, so the
// Windows host is asked through tasklist.exe (WSL interop).
func WoWRunning() (running bool, detail string, err error) {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return false, "", err
		}
		name, ok := matchTasklist(out)
		return ok, name, nil
	}
	out, err := exec.Command("ps", "-axo", "args=").Output()
	if err != nil {
		return false, "", err
	}
	if line, ok := matchPS(out); ok {
		return true, line, nil
	}
	if IsWSL() {
		out, err := exec.Command("tasklist.exe", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return false, "", fmt.Errorf("inside WSL but tasklist.exe failed (interop disabled?): %w", err)
		}
		if name, ok := matchTasklist(out); ok {
			return true, name + " (Windows host)", nil
		}
	}
	return false, "", nil
}

// matchTasklist scans `tasklist /FO CSV /NH` output, whose first column is the
// image name, for a WoW client executable.
func matchTasklist(out []byte) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name := strings.Trim(strings.SplitN(sc.Text(), ",", 2)[0], `"`)
		if exeName.MatchString(name) {
			return name, true
		}
	}
	return "", false
}

// matchPS scans `ps -axo args=` output for a WoW client: a Wine-run .exe
// path (which may contain spaces) or the macOS app bundle binary.
func matchPS(out []byte) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if exeInLine.MatchString(line) || macBinary.MatchString(line) {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

// IsWSL reports whether this Linux process runs inside Windows Subsystem for Linux.
func IsWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}
