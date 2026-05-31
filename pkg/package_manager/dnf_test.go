package packagemanager

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDnfManagerUpdate(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	dnf := NewDnfManager(runner)

	if err := dnf.Update(context.Background()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantCommands(t, runner, []string{"sudo dnf makecache"})
}

func TestDnfManagerInstall(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	dnf := NewDnfManager(runner)

	err := dnf.Install(context.Background(),
		Package{Name: "tenstorrent-dkms", Version: "2.8.0"},
		Package{Name: "cmake"},
	)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	wantCommands(t, runner, []string{"sudo dnf install -y -- tenstorrent-dkms-2.8.0 cmake"})
}

func TestDnfManagerInstallNoPackages(t *testing.T) {
	runner := &MockRunner{Strict: true}
	dnf := NewDnfManager(runner)

	if err := dnf.Install(context.Background()); err == nil {
		t.Fatal("Install with no packages: nil error, want error")
	}
	if len(runner.Commands) != 0 {
		t.Errorf("ran %d commands, want 0", len(runner.Commands))
	}
}

func TestDnfManagerInstallRejectsEmptyName(t *testing.T) {
	runner := &MockRunner{Strict: true}
	dnf := NewDnfManager(runner)

	if err := dnf.Install(context.Background(), Package{Name: ""}); err == nil {
		t.Fatal("Install with empty name: nil error, want error")
	}
	if len(runner.Commands) != 0 {
		t.Errorf("ran %d commands, want 0", len(runner.Commands))
	}
}

func TestDnfManagerRemove(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	dnf := NewDnfManager(runner)

	if err := dnf.Remove(context.Background(), "tt-smi", "tt-flash"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	wantCommands(t, runner, []string{"sudo dnf remove -y -- tt-smi tt-flash"})
}

func TestDnfManagerAddRepo(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	dnf := NewDnfManager(runner)

	err := dnf.AddRepo(context.Background(), Repository{
		Name: "tenstorrent",
		URI:  "https://repo.example.invalid/tenstorrent.repo",
	})
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	wantCommands(t, runner, []string{"sudo dnf config-manager --add-repo https://repo.example.invalid/tenstorrent.repo"})
}

func TestDnfManagerAddRepoRejectsEmptyURI(t *testing.T) {
	runner := &MockRunner{Strict: true}
	dnf := NewDnfManager(runner)

	if err := dnf.AddRepo(context.Background(), Repository{Name: "x"}); err == nil {
		t.Fatal("AddRepo with empty URI: nil error, want error")
	}
	if len(runner.Commands) != 0 {
		t.Errorf("ran %d commands, want 0", len(runner.Commands))
	}
}

func TestDnfManagerNoSudo(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	dnf := NewDnfManager(runner)
	dnf.Sudo = false

	if err := dnf.Update(context.Background()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantCommands(t, runner, []string{"dnf makecache"})
}

func TestDnfManagerInstallError(t *testing.T) {
	runner := &MockRunner{Responses: []CommandResponse{{
		Output: []byte("Error: Unable to find a match: bogus"),
		Err:    errors.New("exit status 1"),
	}}}
	dnf := NewDnfManager(runner)

	err := dnf.Install(context.Background(), Package{Name: "bogus"})
	if err == nil {
		t.Fatal("Install: nil error, want error")
	}
	if got := err.Error(); !strings.Contains(got, "Unable to find a match") {
		t.Errorf("error %q does not include command output", got)
	}
}

func TestDnfManagerIsInstalled(t *testing.T) {
	tests := []struct {
		name     string
		response CommandResponse
		want     bool
		wantErr  bool
	}{
		{
			name:     "installed",
			response: CommandResponse{Output: []byte("cmake-3.30.0-1.fc40.x86_64")},
			want:     true,
		},
		{
			name: "not installed",
			response: CommandResponse{
				Output: []byte("package tt-smi is not installed"),
				Err:    errors.New("exit status 1"),
			},
			want: false,
		},
		{
			name: "not installed via exit code (localized output)",
			response: CommandResponse{
				Output: []byte("paquet tt-smi n'est pas installé"),
				Err:    exitErr(t, 1),
			},
			want: false,
		},
		{
			name: "unexpected failure",
			response: CommandResponse{
				Output: []byte("rpm: command not found"),
				Err:    errors.New("exec: rpm: not found"),
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &MockRunner{Responses: []CommandResponse{tc.response}}
			dnf := NewDnfManager(runner)

			got, err := dnf.IsInstalled(context.Background(), "tt-smi")
			if tc.wantErr {
				if err == nil {
					t.Fatal("IsInstalled: nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("IsInstalled: %v", err)
			}
			if got != tc.want {
				t.Errorf("IsInstalled = %v, want %v", got, tc.want)
			}
			// rpm queries must never use sudo.
			wantCommands(t, runner, []string{"rpm -q tt-smi"})
		})
	}
}
