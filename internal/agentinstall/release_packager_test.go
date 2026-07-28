//go:build !windows

package agentinstall

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasePackagerBuildsVerifiedTarAndZipAssets(t *testing.T) {
	root := repositoryRoot(t)
	output := filepath.Join(root, "internal", "agentinstall", ".release-fixture")
	if err := os.RemoveAll(output); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(output) })

	command := exec.Command("sh", filepath.Join(root, "scripts", "package-release.sh"))
	command.Dir = filepath.Join(root, "internal", "agentinstall")
	command.Env = append(os.Environ(),
		"OMG_RELEASE_VERSION=test-version",
		"OMG_RELEASE_OUTPUT="+output,
		"OMG_RELEASE_TARGETS=linux/amd64 windows/amd64",
	)
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("release packager failed: %v\n%s", err, combined)
	}
	if !strings.Contains(string(combined), "OMG release assets packaged.") {
		t.Fatalf("unexpected release packager output: %s", combined)
	}

	assets := []string{"omg_linux_amd64.tar.gz", "omg_windows_amd64.zip"}
	checksums := readChecksums(t, filepath.Join(output, "checksums.txt"))
	if len(checksums) != len(assets) {
		t.Fatalf("checksum count = %d, want %d: %+v", len(checksums), len(assets), checksums)
	}
	for _, assetName := range assets {
		assetPath := filepath.Join(output, assetName)
		data, err := os.ReadFile(assetPath)
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(data)
		if checksums[assetName] != hex.EncodeToString(actual[:]) {
			t.Fatalf("checksum mismatch for %s", assetName)
		}
	}
	verifyTarAsset(t, filepath.Join(output, assets[0]))
	verifyZipAsset(t, filepath.Join(output, assets[1]))
	for _, residual := range []string{".work", ".cache"} {
		if _, err := os.Stat(filepath.Join(output, residual)); !os.IsNotExist(err) {
			t.Fatalf("release packager left disposable path %s: %v", residual, err)
		}
	}
}

func readChecksums(t *testing.T, path string) map[string]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			t.Fatalf("invalid checksum entry: %q", scanner.Text())
		}
		if _, exists := result[fields[1]]; exists {
			t.Fatalf("duplicate checksum entry: %q", fields[1])
		}
		result[fields[1]] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func verifyTarAsset(t *testing.T, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	header, err := archive.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "omg" || header.Size <= 0 || header.Mode&0o111 == 0 {
		t.Fatalf("unexpected POSIX archive header: %+v", header)
	}
	if _, err := io.Copy(io.Discard, archive); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Next(); err != io.EOF {
		t.Fatalf("POSIX archive contains additional entries or failed: %v", err)
	}
}

func verifyZipAsset(t *testing.T, path string) {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "omg.exe" || archive.File[0].UncompressedSize64 == 0 {
		t.Fatalf("unexpected Windows archive: %s", fmt.Sprint(archive.File))
	}
}
