package wow

import (
	"bufio"
	"bytes"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// exeName matches the executables the WoW clients ship: Wow.exe, WowClassic.exe,
// WowT.exe (PTR), WowB.exe (beta), Wow-64.exe (old 64-bit builds). WowUp.exe
// and other tools do not match.
var exeName = regexp.MustCompile(`(?i)(^|[\\/])wow(classic|t|b|f|-64)?\.exe$`)

// macBinary matches the binary inside the macOS app bundle.
var macBinary = regexp.MustCompile(`/Contents/MacOS/World of Warcraft( Classic| Beta| Test)?(\s|$)`)

// WoWRunning reports whether a WoW client process appears to be running on
// this machine. It is best effort: a non-nil error means "could not tell",
// and callers should treat that as unknown rather than as "not running".
func WoWRunning() (running bool, detail string, err error) {
	var out []byte
	if runtime.GOOS == "windows" {
		out, err = exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
		if err != nil {
			return false, "", err
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			line := sc.Text()
			// "Wow.exe","1234","Console","1","1,234 K"
			name := strings.Trim(strings.SplitN(line, ",", 2)[0], `"`)
			if exeName.MatchString(name) {
				return true, name, nil
			}
		}
		return false, "", nil
	}
	out, err = exec.Command("ps", "-axo", "args=").Output()
	if err != nil {
		return false, "", err
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		first := line
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			first = line[:i]
		}
		if exeName.MatchString(first) || macBinary.MatchString(line) {
			return true, strings.TrimSpace(line), nil
		}
	}
	return false, "", nil
}
