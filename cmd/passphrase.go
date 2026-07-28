package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Supriya-112/chunkvault/internal/vault"
)

// passphraseEnv lets scripts and tests supply a passphrase without a terminal.
const passphraseEnv = "CHUNKVAULT_PASSPHRASE"

// passphraseForVault returns the passphrase needed to open vaultDir, or nil when
// the vault is not encrypted. It is the read path used by restore and stats.
func passphraseForVault(cmd *cobra.Command, vaultDir, passphraseFile string) ([]byte, error) {
	encrypted, err := vault.IsEncrypted(vaultDir)
	if err != nil {
		return nil, err
	}
	if !encrypted {
		return nil, nil
	}
	return vaultPassphrase(cmd, passphraseFile, false)
}

// vaultPassphrase resolves the passphrase for an encrypted vault, in order of
// preference: the CHUNKVAULT_PASSPHRASE environment variable, a --passphrase-file,
// or an interactive no-echo prompt. needConfirm is set only when a new encrypted
// vault is being created, so the user re-types the passphrase they are setting.
func vaultPassphrase(cmd *cobra.Command, passphraseFile string, needConfirm bool) ([]byte, error) {
	if v, ok := os.LookupEnv(passphraseEnv); ok {
		return []byte(v), nil
	}
	if passphraseFile != "" {
		data, err := os.ReadFile(passphraseFile)
		if err != nil {
			return nil, fmt.Errorf("reading passphrase file: %w", err)
		}
		return []byte(strings.TrimRight(string(data), "\r\n")), nil
	}
	return promptPassphrase(cmd, needConfirm)
}

// promptPassphrase reads a passphrase from the terminal without echoing it.
func promptPassphrase(cmd *cobra.Command, needConfirm bool) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("no passphrase available: set %s or --passphrase-file, or run in a terminal", passphraseEnv)
	}
	errOut := cmd.ErrOrStderr()

	fmt.Fprint(errOut, "passphrase: ")
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(errOut)
	if err != nil {
		return nil, err
	}
	if len(pw) == 0 {
		return nil, fmt.Errorf("passphrase must not be empty")
	}
	if needConfirm {
		fmt.Fprint(errOut, "confirm passphrase: ")
		confirm, err := term.ReadPassword(fd)
		fmt.Fprintln(errOut)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(pw, confirm) {
			return nil, fmt.Errorf("passphrases do not match")
		}
	}
	return pw, nil
}
