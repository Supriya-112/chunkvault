package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetFlags restores every flag on cmd and its subcommands to its default,
// since Cobra reuses the shared rootCmd across runCmd calls and does not reset a
// flag that is absent from a given invocation — otherwise a value set by one
// test would silently leak into later ones.
func resetFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	for _, sub := range cmd.Commands() {
		resetFlags(sub)
	}
}

// runCmd executes the root command with the given args, capturing output,
// and returns any error. It resets flags and output routing so each invocation
// starts clean and tests don't leak to stderr.
func runCmd(args ...string) (string, error) {
	resetFlags(rootCmd)
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

func TestBackupOnValidDir(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	vaultDir := t.TempDir()

	out, err := runCmd("backup", src, "--vault", vaultDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Contains([]byte(out), []byte("snapshot")) {
		t.Fatalf("expected a snapshot summary, got: %q", out)
	}
}

// TestBackupCompressFlag runs a backup -> restore through the CLI with each
// --compress value and checks the data survives the round-trip intact.
func TestBackupCompressFlag(t *testing.T) {
	want := bytes.Repeat([]byte("compress me "), 5000)
	for _, codec := range []string{"none", "gzip", "zstd"} {
		t.Run(codec, func(t *testing.T) {
			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "a.txt"), want, 0o644); err != nil {
				t.Fatal(err)
			}
			vaultDir := t.TempDir()

			out, err := runCmd("backup", src, "--vault", vaultDir, "--compress", codec)
			if err != nil {
				t.Fatalf("backup --compress %s: %v", codec, err)
			}
			snapID := parseSnapshotID(t, out)

			target := t.TempDir()
			if _, err := runCmd("restore", snapID, target, "--vault", vaultDir); err != nil {
				t.Fatalf("restore: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(target, "a.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("--compress %s: restored content differs from source", codec)
			}
		})
	}
}

func TestBackupRejectsUnknownCompress(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCmd("backup", src, "--vault", t.TempDir(), "--compress", "brotli"); err == nil {
		t.Fatal("expected an error for an unknown --compress value")
	}
}
