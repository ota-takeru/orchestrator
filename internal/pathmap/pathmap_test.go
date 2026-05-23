package pathmap

import (
	"context"
	"testing"

	"github.com/ota-takeru/orchestrator/internal/platform"
)

func TestPathMappingWindowsToWSL(t *testing.T) {
	service, err := NewService([]Environment{
		{ID: "windows-main", OSFamily: platform.OSFamilyWindows, AllowedRoot: `C:\dev\app`},
		{ID: "wsl-sidecar", OSFamily: platform.OSFamilyWSL, AllowedRoot: "/mnt/c/dev/app"},
	}, []Mapping{
		{
			FromEnvironmentID:       "windows-main",
			ToEnvironmentID:         "wsl-sidecar",
			FromRoot:                `C:\dev\app`,
			ToRoot:                  "/mnt/c/dev/app",
			Mode:                    platform.MappingSameFilesystem,
			WriteOwnerEnvironmentID: "windows-main",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.ToEnvironmentPath(context.Background(), "windows-main", "wsl-sidecar", `C:\dev\app\src\main.go`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/mnt/c/dev/app/src/main.go" {
		t.Fatalf("mapped path = %q", got)
	}
}

func TestValidateRejectsPathOutsideAllowedRoot(t *testing.T) {
	env := Environment{ID: "linux-main", OSFamily: platform.OSFamilyLinux, AllowedRoot: "/home/user/app"}
	if err := ValidatePath(env, "/home/user/app/main.go", PurposeRead); err != nil {
		t.Fatalf("expected path inside root to pass: %v", err)
	}
	if err := ValidatePath(env, "/home/user/other/main.go", PurposeRead); err == nil {
		t.Fatal("expected path outside root to fail")
	}
}

func TestValidateRejectsUnsafeWindowsSegments(t *testing.T) {
	env := Environment{ID: "windows-main", OSFamily: platform.OSFamilyWindows, AllowedRoot: `C:\dev\app`}
	if err := ValidatePath(env, `C:\dev\app\NUL.txt`, PurposeRead); err == nil {
		t.Fatal("expected reserved windows segment to fail")
	}
	if err := ValidatePath(env, `\\server\share\app`, PurposeRead); err == nil {
		t.Fatal("expected UNC path to fail")
	}
}

func TestSameFilesystemMappingRequiresWriteOwner(t *testing.T) {
	_, err := NewService([]Environment{
		{ID: "windows-main", OSFamily: platform.OSFamilyWindows, AllowedRoot: `C:\dev\app`},
		{ID: "wsl-sidecar", OSFamily: platform.OSFamilyWSL, AllowedRoot: "/mnt/c/dev/app"},
	}, []Mapping{
		{
			FromEnvironmentID: "windows-main",
			ToEnvironmentID:   "wsl-sidecar",
			FromRoot:          `C:\dev\app`,
			ToRoot:            "/mnt/c/dev/app",
			Mode:              platform.MappingSameFilesystem,
		},
	})
	if err == nil {
		t.Fatal("expected same_filesystem mapping without write owner to fail")
	}
}
