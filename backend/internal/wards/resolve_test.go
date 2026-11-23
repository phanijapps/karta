package wards

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveVaultRoot(t *testing.T) {
	home := t.TempDir()
	// os.UserHomeDir reads HOME on unix, USERPROFILE on Windows — set
	// both so the fixture home wins on every platform.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	daemonHome := filepath.Join(home, ".zbot")
	abs := filepath.Join(t.TempDir(), "vault")
	absTwo := filepath.Join(t.TempDir(), "vault")

	cases := []struct {
		name       string
		configured string
		want       string
		wantErr    bool
	}{
		{name: "empty means OS-aware default", configured: "", want: filepath.Join(home, "Documents", "wards")},
		{name: "bare tilde is home", configured: "~", want: home},
		{name: "tilde expands", configured: "~/vault", want: filepath.Join(home, "vault")},
		{name: "absolute passes through cleaned", configured: abs + string(filepath.Separator), want: abs},
		{name: "relative rejected typed", configured: "wards", wantErr: true},
		{name: "whitespace trimmed before checks", configured: "  " + absTwo + "  ", want: absTwo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveVaultRoot(tc.configured, daemonHome)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				var ve *VaultRootError
				if !errors.As(err, &ve) {
					t.Fatalf("want VaultRootError, got %T: %v", err, err)
				}
				if !strings.Contains(err.Error(), "wards.vault_root") {
					t.Fatalf("error must name the config field: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveVaultRoot: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// The constitution's vault exception is ENFORCED: the vault never
// lives inside the daemon home (~/.zbot), however the operator spells
// it — but an unrelated `.zbot` directory elsewhere, or a sibling like
// ~/.zbot-wards, is fine.
func TestResolveVaultRootRejectsDaemonHomeContainment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	daemonHome := filepath.Join(home, ".zbot")
	if err := os.MkdirAll(daemonHome, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("rejected: the daemon home itself", func(t *testing.T) {
		_, err := ResolveVaultRoot(daemonHome, daemonHome)
		assertInsideHome(t, err)
	})
	t.Run("rejected: beneath the daemon home", func(t *testing.T) {
		_, err := ResolveVaultRoot(filepath.Join(daemonHome, "wards"), daemonHome)
		assertInsideHome(t, err)
	})
	t.Run("rejected: tilde-spelled", func(t *testing.T) {
		_, err := ResolveVaultRoot("~/.zbot/wards", daemonHome)
		assertInsideHome(t, err)
	})
	t.Run("rejected: symlink alias of the daemon home", func(t *testing.T) {
		outside := t.TempDir()
		alias := filepath.Join(outside, "zbot-alias")
		if err := os.Symlink(daemonHome, alias); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		_, err := ResolveVaultRoot(filepath.Join(alias, "wards"), daemonHome)
		assertInsideHome(t, err)
	})
	t.Run("allowed: unrelated .zbot elsewhere", func(t *testing.T) {
		other := filepath.Join(t.TempDir(), ".zbot", "wards")
		got, err := ResolveVaultRoot(other, daemonHome)
		if err != nil || got != other {
			t.Fatalf("unrelated .zbot must be allowed, got %q %v", got, err)
		}
	})
	t.Run("allowed: sibling like ~/.zbot-wards", func(t *testing.T) {
		sibling := filepath.Join(home, ".zbot-wards")
		got, err := ResolveVaultRoot(sibling, daemonHome)
		if err != nil || got != sibling {
			t.Fatalf("sibling of the home must be allowed, got %q %v", got, err)
		}
	})
}

func assertInsideHome(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want inside-daemon-home rejection")
	}
	var ve *VaultRootError
	if !errors.As(err, &ve) {
		t.Fatalf("want VaultRootError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "outside the daemon home") {
		t.Fatalf("error must name the rule: %v", err)
	}
}

// The default is only a default: on a headless home with no Documents
// dir, bootstrap creates the whole chain (design risk table). The
// resolved root is asserted BEFORE bootstrap so the test can never run
// against the developer's real home.
func TestDefaultVaultRootMissingDocuments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := os.Stat(filepath.Join(home, "Documents")); err == nil {
		t.Fatal("fixture: Documents already exists")
	}
	root, err := DefaultVaultRoot()
	if err != nil {
		t.Fatalf("DefaultVaultRoot: %v", err)
	}
	want := filepath.Join(home, "Documents", "wards")
	if root != want {
		t.Fatalf("resolved root %q must point into the fixture home %q before any bootstrap", root, want)
	}
	if err := Bootstrap(context.Background(), root); err != nil {
		t.Fatalf("Bootstrap under missing Documents: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("vault not created at %s: %v", root, err)
	}
}
