package packagemanager

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestMockRunnerRecordsAndResponds(t *testing.T) {
	wantErr := errors.New("boom")
	runner := &MockRunner{
		Responses: []CommandResponse{
			{Output: []byte("ok")},
			{Err: wantErr},
		},
	}
	ctx := context.Background()

	out, err := runner.Run(ctx, "apt-get", "update")
	if err != nil {
		t.Fatalf("first Run err = %v, want nil", err)
	}
	if string(out) != "ok" {
		t.Errorf("first Run output = %q, want ok", out)
	}

	if _, err := runner.Run(ctx, "apt-get", "install", "-y", "cmake"); !errors.Is(err, wantErr) {
		t.Errorf("second Run err = %v, want %v", err, wantErr)
	}

	// Responses exhausted -> zero output, nil error.
	if out, err := runner.Run(ctx, "apt-get", "clean"); out != nil || err != nil {
		t.Errorf("third Run = (%q, %v), want (nil, nil)", out, err)
	}

	want := []string{
		"apt-get update",
		"apt-get install -y cmake",
		"apt-get clean",
	}
	if got := runner.CommandStrings(); !reflect.DeepEqual(got, want) {
		t.Errorf("CommandStrings = %v, want %v", got, want)
	}
}

func TestMockRunnerRunFuncOverrides(t *testing.T) {
	var seen RecordedCommand
	runner := &MockRunner{
		Responses: []CommandResponse{{Output: []byte("ignored")}},
		RunFunc: func(_ context.Context, name string, args ...string) ([]byte, error) {
			seen = RecordedCommand{Name: name, Args: args}
			return []byte("from func"), nil
		},
	}

	out, err := runner.Run(context.Background(), "dnf", "makecache")
	if err != nil {
		t.Fatalf("Run err = %v", err)
	}
	if string(out) != "from func" {
		t.Errorf("output = %q, want 'from func'", out)
	}
	if seen.String() != "dnf makecache" {
		t.Errorf("RunFunc saw %q, want 'dnf makecache'", seen.String())
	}
	// Responses must be untouched when RunFunc is set.
	if len(runner.Responses) != 1 {
		t.Errorf("Responses consumed = %d, want 1 remaining", 1-len(runner.Responses))
	}
}

func TestMockRunnerStrictModeFailsOnUnprogrammed(t *testing.T) {
	runner := &MockRunner{
		Strict:    true,
		Responses: []CommandResponse{{Output: []byte("ok")}},
	}
	ctx := context.Background()

	if _, err := runner.Run(ctx, "apt-get", "update"); err != nil {
		t.Fatalf("first Run err = %v, want nil", err)
	}
	if _, err := runner.Run(ctx, "apt-get", "install", "cmake"); err == nil {
		t.Fatal("expected strict error on unprogrammed command, got nil")
	}
}

func TestMockRunnerCopiesArgs(t *testing.T) {
	runner := &MockRunner{}
	args := []string{"install", "cmake"}
	if _, err := runner.Run(context.Background(), "apt-get", args...); err != nil {
		t.Fatalf("Run err = %v", err)
	}
	// Mutating the caller's slice must not change the recorded command.
	args[1] = "ninja-build"
	if got := runner.CommandStrings()[0]; got != "apt-get install cmake" {
		t.Errorf("recorded command = %q, want 'apt-get install cmake' (args not copied)", got)
	}
}

func TestRecordedCommandString(t *testing.T) {
	tests := []struct {
		name string
		cmd  RecordedCommand
		want string
	}{
		{"no args", RecordedCommand{Name: "apt-get"}, "apt-get"},
		{"with args", RecordedCommand{Name: "apt-get", Args: []string{"install", "cmake"}}, "apt-get install cmake"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cmd.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMockPackageManagerRecordsCallSequence(t *testing.T) {
	ctx := context.Background()
	pm := &MockPackageManager{}

	if err := pm.Update(ctx); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := pm.AddRepo(ctx, Repository{Name: "tenstorrent", URI: "https://ppa.tenstorrent.com/ubuntu/"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := pm.Install(ctx, Package{Name: "kmd", Version: "2.8.0"}, Package{Name: "cmake"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed, err := pm.IsInstalled(ctx, "kmd")
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Error("kmd should be installed after Install")
	}

	if err := pm.Remove(ctx, "kmd"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if installed, _ := pm.IsInstalled(ctx, "kmd"); installed {
		t.Error("kmd should not be installed after Remove")
	}

	want := []string{
		"update",
		"add-repo:tenstorrent",
		"install:kmd=2.8.0,cmake",
		"is-installed:kmd",
		"remove:kmd",
		"is-installed:kmd",
	}
	if !reflect.DeepEqual(pm.Calls, want) {
		t.Errorf("Calls = %v, want %v", pm.Calls, want)
	}
}

func TestMockPackageManagerProgrammedErrors(t *testing.T) {
	ctx := context.Background()
	failUpdate := errors.New("network down")
	failInstall := errors.New("pin not found")

	tests := []struct {
		name    string
		op      func(pm *MockPackageManager) error
		errs    map[string]error
		wantErr error
	}{
		{
			name:    "update error",
			op:      func(pm *MockPackageManager) error { return pm.Update(ctx) },
			errs:    map[string]error{"update": failUpdate},
			wantErr: failUpdate,
		},
		{
			name:    "install error leaves state unchanged",
			op:      func(pm *MockPackageManager) error { return pm.Install(ctx, Package{Name: "kmd"}) },
			errs:    map[string]error{"install": failInstall},
			wantErr: failInstall,
		},
		{
			name:    "no error configured",
			op:      func(pm *MockPackageManager) error { return pm.Update(ctx) },
			errs:    nil,
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := &MockPackageManager{Errors: tt.errs}
			err := tt.op(pm)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.name == "install error leaves state unchanged" {
				if installed, _ := pm.IsInstalled(ctx, "kmd"); installed {
					t.Error("package must not be marked installed when Install fails")
				}
			}
		})
	}
}

// TestMockPackageManagerSatisfiesInterface ensures the mock is a usable
// PackageManager value.
func TestMockPackageManagerSatisfiesInterface(t *testing.T) {
	var pm PackageManager = &MockPackageManager{}
	if err := pm.Update(context.Background()); err != nil {
		t.Fatalf("Update via interface: %v", err)
	}
}
