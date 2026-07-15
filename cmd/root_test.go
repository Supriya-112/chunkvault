package cmd

import (
	"bytes"
	"testing"
)

// runCmd executes the root command with the given args, capturing output,
// and returns any error. It resets output routing so tests don't leak to stderr.
func runCmd(args ...string) (string, error) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestBackupRequiresExactlyOneArg(t *testing.T) {
	if _, err := runCmd("backup"); err == nil {
		t.Fatal("expected an error when 'backup' is called with no source dir")
	}
}

func TestRestoreRequiresTwoArgs(t *testing.T) {
	if _, err := runCmd("restore", "only-one"); err == nil {
		t.Fatal("expected an error when 'restore' is called with one arg")
	}
}

func TestBackupAcceptsOneArg(t *testing.T) {
	out, err := runCmd("backup", "somedir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains([]byte(out), []byte("somedir")) {
		t.Fatalf("expected output to mention the source dir, got: %q", out)
	}
}
