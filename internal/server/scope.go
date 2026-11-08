package server

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/caboose-ai/wow-mcp/internal/wow"
)

// scope is where account-level or character-level files live.
type scope struct {
	Dir       string // account dir or character dir
	Label     string // "ACCOUNT" or "ACCOUNT/Realm/Char"
	Account   *wow.Account
	Character *wow.Character // nil for account scope
}

// resolveScope picks the account folder, or the character folder when a
// character is named.
func resolveScope(fl *wow.Flavor, account, realm, character string) (*scope, error) {
	if character == "" {
		if realm != "" {
			return nil, errors.New("realm was given without a character; pass character too, or neither for the account scope")
		}
		acc, err := fl.ResolveAccount(account)
		if err != nil {
			return nil, err
		}
		return &scope{Dir: acc.Dir, Label: acc.Name, Account: acc}, nil
	}
	ch, err := fl.ResolveCharacter(account, realm, character)
	if err != nil {
		return nil, err
	}
	acc, err := fl.ResolveAccount(ch.Account)
	if err != nil {
		return nil, err
	}
	return &scope{Dir: ch.Dir, Label: ch.Account + "/" + ch.Realm + "/" + ch.Name, Account: acc, Character: ch}, nil
}

// savedVariablesFile is <scope>/SavedVariables/<addon>.lua.
func (sc *scope) savedVariablesFile(addon string) string {
	return filepath.Join(sc.Dir, "SavedVariables", addon+".lua")
}

// requireAddonKnown makes sure name is an installed addon folder, or one of
// Blizzard's built-in addons (which live inside the game data, not on disk).
func requireAddonKnown(fl *wow.Flavor, name string) (string, error) {
	if err := wow.ValidateName("addon", name); err != nil {
		return "", err
	}
	if strings.Contains(name, ":") {
		return "", fmt.Errorf("addon name %q must not contain ':'", name)
	}
	if strings.HasPrefix(strings.ToLower(name), "blizzard_") {
		return name, nil
	}
	a, err := wow.FindAddon(fl, name)
	if err != nil {
		return "", fmt.Errorf("%w (only installed addons and Blizzard_* can be addressed)", err)
	}
	return a.Name, nil
}
