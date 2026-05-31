package packagemanager

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAptManagerUpdate(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	apt := NewAptManager(runner)

	if err := apt.Update(context.Background()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantCommands(t, runner, []string{"sudo apt-get update"})
}

func TestAptManagerInstall(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	apt := NewAptManager(runner)

	err := apt.Install(context.Background(),
		Package{Name: "cmake", Version: "3.30.0"},
		Package{Name: "ninja-build"},
	)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	wantCommands(t, runner, []string{"sudo apt-get install -y -- cmake=3.30.0 ninja-build"})
}

func TestAptManagerInstallNoPackages(t *testing.T) {
	runner := &MockRunner{Strict: true}
	apt := NewAptManager(runner)

	if err := apt.Install(context.Background()); err == nil {
		t.Fatal("Install with no packages: nil error, want error")
	}
	if len(runner.Commands) != 0 {
		t.Errorf("ran %d commands, want 0", len(runner.Commands))
	}
}

func TestAptManagerInstallRejectsEmptyName(t *testing.T) {
	runner := &MockRunner{Strict: true}
	apt := NewAptManager(runner)

	if err := apt.Install(context.Background(), Package{Name: ""}); err == nil {
		t.Fatal("Install with empty name: nil error, want error")
	}
	if len(runner.Commands) != 0 {
		t.Errorf("ran %d commands, want 0", len(runner.Commands))
	}
}

func TestAptManagerRemove(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	apt := NewAptManager(runner)

	if err := apt.Remove(context.Background(), "tt-smi", "tt-flash"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	wantCommands(t, runner, []string{"sudo apt-get remove -y -- tt-smi tt-flash"})
}

func TestAptManagerAddRepo(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	apt := NewAptManager(runner)

	err := apt.AddRepo(context.Background(), Repository{
		Name: "tenstorrent",
		URI:  "https://ppa.tenstorrent.com/ubuntu/",
	})
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	wantCommands(t, runner, []string{"sudo add-apt-repository -y https://ppa.tenstorrent.com/ubuntu/"})
}

func TestAptManagerAddRepoRejectsEmptyURI(t *testing.T) {
	runner := &MockRunner{Strict: true}
	apt := NewAptManager(runner)

	if err := apt.AddRepo(context.Background(), Repository{Name: "x"}); err == nil {
		t.Fatal("AddRepo with empty URI: nil error, want error")
	}
	if len(runner.Commands) != 0 {
		t.Errorf("ran %d commands, want 0", len(runner.Commands))
	}
}

func TestAptManagerNoSudo(t *testing.T) {
	runner := &MockRunner{Strict: true, Responses: []CommandResponse{{}}}
	apt := NewAptManager(runner)
	apt.Sudo = false

	if err := apt.Update(context.Background()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	wantCommands(t, runner, []string{"apt-get update"})
}

func TestAptManagerInstallError(t *testing.T) {
	runner := &MockRunner{Responses: []CommandResponse{{
		Output: []byte("E: Unable to locate package bogus"),
		Err:    errors.New("exit status 100"),
	}}}
	apt := NewAptManager(runner)

	err := apt.Install(context.Background(), Package{Name: "bogus"})
	if err == nil {
		t.Fatal("Install: nil error, want error")
	}
	if got := err.Error(); !strings.Contains(got, "Unable to locate package") {
		t.Errorf("error %q does not include command output", got)
	}
}

func TestAptManagerIsInstalled(t *testing.T) {
	tests := []struct {
		name     string
		response CommandResponse
		want     bool
		wantErr  bool
	}{
		{
			name:     "installed",
			response: CommandResponse{Output: []byte("install ok installed")},
			want:     true,
		},
		{
			name: "not installed (unknown)",
			response: CommandResponse{
				Output: []byte("dpkg-query: no packages found matching tt-smi"),
				Err:    errors.New("exit status 1"),
			},
			want: false,
		},
		{
			name:     "config files only",
			response: CommandResponse{Output: []byte("deinstall ok config-files")},
			want:     false,
		},
		{
			name: "unexpected failure",
			response: CommandResponse{
				Output: []byte("dpkg-query: command not found"),
				Err:    errors.New("exec: dpkg-query: not found"),
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runner := &MockRunner{Responses: []CommandResponse{tc.response}}
			apt := NewAptManager(runner)

			got, err := apt.IsInstalled(context.Background(), "tt-smi")
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
			// dpkg-query queries must never use sudo.
			wantCommands(t, runner, []string{"dpkg-query -W -f=${Status} tt-smi"})
		})
	}
}

type fakeResolver map[string]string

func (f fakeResolver) ResolvePackage(virtual string) (string, bool) {
	concrete, ok := f[virtual]
	return concrete, ok
}

func TestResolvePackages(t *testing.T) {
	resolver := fakeResolver{"cmake": "cmake", "ninja": "ninja-build"}

	pkgs, err := ResolvePackages(resolver,
		VirtualPackage{Name: "cmake", Version: "3.30.0"},
		VirtualPackage{Name: "ninja"},
	)
	if err != nil {
		t.Fatalf("ResolvePackages: %v", err)
	}
	want := []Package{
		{Name: "cmake", Version: "3.30.0"},
		{Name: "ninja-build"},
	}
	if !reflect.DeepEqual(pkgs, want) {
		t.Errorf("ResolvePackages = %v, want %v", pkgs, want)
	}
}

func TestResolvePackagesUnknown(t *testing.T) {
	resolver := fakeResolver{}
	if _, err := ResolvePackages(resolver, VirtualPackage{Name: "cmake"}); err == nil {
		t.Fatal("ResolvePackages with unknown virtual: nil error, want error")
	}
}

func wantCommands(t *testing.T, runner *MockRunner, want []string) {
	t.Helper()
	got := runner.CommandStrings()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v, want %v", got, want)
	}
}
