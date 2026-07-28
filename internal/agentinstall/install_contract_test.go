package agentinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}

func readRepositoryFile(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOneInstallScriptsShareReleaseAndAgentContracts(t *testing.T) {
	posix := readRepositoryFile(t, "install")
	powershell := readRepositoryFile(t, "install.ps1")
	packager := readRepositoryFile(t, "scripts/package-release.sh")

	for _, required := range []string{
		"OMG_INSTALL_SOURCE", "OMG_INSTALL_SHA256", "checksums.txt", "agent install",
		"omg_${os_name}_${arch_name}.tar.gz", "refusing to replace symlinked destination",
	} {
		if !strings.Contains(posix, required) {
			t.Errorf("POSIX installer missing %q", required)
		}
	}
	for _, required := range []string{
		"OMG_INSTALL_SOURCE", "OMG_INSTALL_SHA256", "checksums.txt", "agent install",
		"omg_windows_${Architecture}.zip", "reparse-point destination",
	} {
		if !strings.Contains(powershell, required) {
			t.Errorf("PowerShell installer missing %q", required)
		}
	}
	for _, required := range []string{
		"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64",
		"omg_${os_name}_${arch_name}.tar.gz", "omg_${os_name}_${arch_name}.zip", "checksums.txt", "CGO_ENABLED=0", "-trimpath",
	} {
		if !strings.Contains(packager, required) {
			t.Errorf("release packager missing %q", required)
		}
	}
	for _, forbidden := range []string{"curl -k", "wget --no-check-certificate", "chmod 777", "Invoke-Expression", "iex "} {
		if strings.Contains(strings.ToLower(posix+"\n"+powershell+"\n"+packager), strings.ToLower(forbidden)) {
			t.Errorf("installer contract contains forbidden pattern %q", forbidden)
		}
	}
}

func TestInstallScriptsDoNotEmbedPrivateMachinePathsOrSecrets(t *testing.T) {
	content := readRepositoryFile(t, "install") + readRepositoryFile(t, "install.ps1") + readRepositoryFile(t, "scripts/package-release.sh")
	for _, forbidden := range []string{"/Users/", "C:\\Users\\", "omgdt_", "PRIVATE KEY", "Authorization:"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("install contract contains forbidden value %q", forbidden)
		}
	}
}
