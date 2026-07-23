package instructions

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRemovePreservesEncodingsAndOuterBytes(t *testing.T) {
	cases := []struct {
		name   string
		data   []byte
		wantNL string
	}{
		{"empty UTF-8", nil, "\n"},
		{"existing LF no final newline", []byte("before\nafter"), "\n"},
		{"existing CRLF final newline", []byte("before\r\n"), "\r\n"},
		{"UTF-8 BOM", append([]byte{0xef, 0xbb, 0xbf}, []byte("before\n")...), "\n"},
		{"UTF-16 LE BOM", encodeText("before\r\n", encodingUTF16LE), "\r\n"},
		{"UTF-16 BE BOM", encodeText("before", encodingUTF16BE), "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "guide.txt")
			if err := os.WriteFile(path, tc.data, 0640); err != nil {
				t.Fatal(err)
			}
			s, err := New(root, []Target{{Path: "guide.txt"}}, "follow\nthese rules")
			if err != nil {
				t.Fatal(err)
			}
			beforePlan, err := s.Plan()
			if err != nil {
				t.Fatal(err)
			}
			if beforePlan[0].Action != ActionCreate {
				t.Fatalf("plan action = %s", beforePlan[0].Action)
			}
			if _, err := s.Apply(); err != nil {
				t.Fatal(err)
			}
			applied, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(applied, token(beginMarker, encodingFor(tc.data))) {
				t.Fatal("managed marker missing")
			}
			if tc.wantNL == "\r\n" && !bytes.Contains(applied, token("\r\n", encodingFor(tc.data))) {
				t.Fatal("CRLF was not retained")
			}
			second, err := s.Apply()
			if err != nil {
				t.Fatal(err)
			}
			if second[0].Action != ActionNone {
				t.Fatalf("second apply = %s", second[0].Action)
			}
			if _, err := s.Remove(); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("remove changed outer bytes:\n got % x\nwant % x", got, tc.data)
			}
		})
	}
}

func TestPlanAndStatusAreReadOnlyAndNestedTargetsAreIndependent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(first, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(root, []Target{{Path: "AGENTS.md"}, {Path: "nested/CLAUDE.md"}}, "instructions")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Status(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Plan(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatal("read-only operation mutated target")
	}
	if _, err := os.Stat(filepath.Join(root, "nested", "CLAUDE.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plan/status created nested target")
	}
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "nested/CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Contains(data, []byte(beginMarker)) {
			t.Fatalf("nested target %q not managed: %v", name, err)
		}
	}
}

func TestRejectsMalformedAndUnsafeInputsWithoutMutation(t *testing.T) {
	root := t.TempDir()
	cases := []string{
		"<!-- OMG BEGIN v1 -->\n", // unclosed
		"<!-- OMG BEGIN v1 -->\n<!-- OMG END v1 -->\n<!-- OMG BEGIN v1 -->\n<!-- OMG END v1 -->", // duplicate
	}
	for _, data := range cases {
		path := filepath.Join(root, "bad.txt")
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		s, err := New(root, []Target{{Path: "bad.txt"}}, "x")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Apply(); !errors.Is(err, ErrMalformedBlock) {
			t.Fatalf("Apply error = %v", err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != data {
			t.Fatal("malformed target changed")
		}
	}
	bad := filepath.Join(root, "bytes.txt")
	original := []byte{0xff, 0x80, 0x81}
	for _, target := range []string{"../escape", "nested/../escape", "/tmp/escape"} {
		if _, err := New(root, []Target{{Path: target}}, "x"); !errors.Is(err, ErrUnsafeTarget) {
			t.Fatalf("%q error = %v", target, err)
		}
	}
	if err := os.WriteFile(bad, original, 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := New(root, []Target{{Path: "bytes.txt"}}, "x")
	if _, err := s.Apply(); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Fatalf("unsupported bytes error = %v", err)
	}
	got, _ := os.ReadFile(bad)
	if !bytes.Equal(got, original) {
		t.Fatal("unsupported bytes changed")
	}
}

func TestRejectsSymlinkAndPreservesModeAndOriginalOnPreRenameFailure(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	link := filepath.Join(root, "link.txt")
	if err := os.WriteFile(real, []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s, _ := New(root, []Target{{Path: "link.txt"}}, "x")
	if _, err := s.Apply(); !errors.Is(err, ErrUnsafeTarget) {
		t.Fatalf("symlink error = %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was not preserved")
	}
	modePath := filepath.Join(root, "mode.txt")
	if err := os.WriteFile(modePath, []byte("keep"), 0751); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(modePath, 0751); err != nil {
		t.Fatal(err)
	}
	modeService, _ := New(root, []Target{{Path: "mode.txt"}}, "x")
	if _, err := modeService.Apply(); err != nil {
		t.Fatal(err)
	}
	assertPreservedInstructionMode(t, modePath)
	before, _ := os.ReadFile(modePath)
	modeService.instructions = "changed"
	modeService.beforeRename = func() error { return errors.New("injected") }
	if _, err := modeService.Apply(); err == nil {
		t.Fatal("expected injected failure")
	}
	after, _ := os.ReadFile(modePath)
	if !bytes.Equal(before, after) {
		t.Fatal("pre-rename failure changed original")
	}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".omg-instructions-") {
			t.Fatal("temporary file leaked")
		}
	}
}

func TestRemoveDeletesTargetCreatedByApply(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "AGENTS.md")
	service, err := New(root, []Target{{Path: "AGENTS.md"}}, "managed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created target remains after remove: %v", err)
	}
}

func TestRemoveRollsBackEarlierTargetsWhenLaterWriteFails(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	if err := os.WriteFile(first, []byte("first original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	service, err := New(root, []Target{{Path: "first.md"}, {Path: "second.md"}}, "managed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	firstBefore, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBefore, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	service.beforeRename = func() error {
		writes++
		if writes == 2 {
			return errors.New("injected second remove failure")
		}
		return nil
	}
	if _, err := service.Remove(); err == nil {
		t.Fatal("Remove unexpectedly succeeded")
	}
	for path, want := range map[string][]byte{first: firstBefore, second: secondBefore} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s changed after failed transaction:\n got %q\nwant %q", path, got, want)
		}
	}
}

func TestRemoveRollbackRestoresDeletedCreatedTarget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	if err := os.WriteFile(second, []byte("second original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	service, err := New(root, []Target{{Path: "first.md"}, {Path: "second.md"}}, "managed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	firstBefore, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBefore, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	service.beforeRename = func() error { return errors.New("injected second remove failure") }
	if _, err := service.Remove(); err == nil {
		t.Fatal("Remove unexpectedly succeeded")
	}
	for path, want := range map[string][]byte{first: firstBefore, second: secondBefore} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s changed after failed transaction:\n got %q\nwant %q", path, got, want)
		}
	}
}

func TestApplyRollsBackEarlierTargetsWhenLaterWriteFails(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	firstBefore := []byte("first original\n")
	secondBefore := []byte("second original\n")
	if err := os.WriteFile(first, firstBefore, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, secondBefore, 0644); err != nil {
		t.Fatal(err)
	}
	service, err := New(root, []Target{{Path: "first.md"}, {Path: "second.md"}}, "managed")
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	service.beforeRename = func() error {
		writes++
		if writes == 2 {
			return errors.New("injected second write failure")
		}
		return nil
	}
	if _, err := service.Apply(); err == nil {
		t.Fatal("Apply unexpectedly succeeded")
	}
	for path, want := range map[string][]byte{first: firstBefore, second: secondBefore} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s changed after failed transaction:\n got %q\nwant %q", path, got, want)
		}
	}
}

func TestApplyRollbackRemovesNewlyCreatedTarget(t *testing.T) {
	root := t.TempDir()
	second := filepath.Join(root, "second.md")
	secondBefore := []byte("second original\n")
	if err := os.WriteFile(second, secondBefore, 0644); err != nil {
		t.Fatal(err)
	}
	service, err := New(root, []Target{{Path: "first.md"}, {Path: "second.md"}}, "managed")
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	service.beforeRename = func() error {
		writes++
		if writes == 2 {
			return errors.New("injected second write failure")
		}
		return nil
	}
	if _, err := service.Apply(); err == nil {
		t.Fatal("Apply unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(root, "first.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created target remains after rollback: %v", err)
	}
	got, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secondBefore) {
		t.Fatalf("second target changed after failed transaction:\n got %q\nwant %q", got, secondBefore)
	}
}

func TestApplyReportsRollbackFailure(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	if err := os.WriteFile(first, []byte("first original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	service, err := New(root, []Target{{Path: "first.md"}, {Path: "second.md"}}, "managed")
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	service.beforeRename = func() error {
		writes++
		if writes == 2 {
			if err := os.WriteFile(first, []byte("external change\n"), 0644); err != nil {
				return err
			}
			return errors.New("injected second write failure")
		}
		return nil
	}
	_, err = service.Apply()
	if !errors.Is(err, ErrIO) || !errors.Is(err, ErrChanged) {
		t.Fatalf("Apply error does not include write and rollback failures: %v", err)
	}
}

func TestUTF16PlanDiffsApplyToDecodedUnicodeContent(t *testing.T) {
	for _, enc := range []encoding{encodingUTF16LE, encodingUTF16BE} {
		t.Run(fmt.Sprintf("%d", enc), func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "guide.md")
			before := "外側\n"
			if err := os.WriteFile(target, encodeText(before, enc), 0644); err != nil {
				t.Fatal(err)
			}
			service, err := New(root, []Target{{Path: "guide.md"}}, "遵守\n規則")
			if err != nil {
				t.Fatal(err)
			}
			assertUTF16PlanPatch(t, root, target, service, false, before)

			service.instructions = "更新\n規則"
			applied, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			current, err := decodeText(applied, enc)
			if err != nil {
				t.Fatal(err)
			}
			assertUTF16PlanPatch(t, root, target, service, false, current)

			applied, err = os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			current, err = decodeText(applied, enc)
			if err != nil {
				t.Fatal(err)
			}
			assertUTF16PlanPatch(t, root, target, service, true, current)
		})
	}
}

func assertUTF16PlanPatch(t *testing.T, root, target string, service *Service, removal bool, before string) {
	t.Helper()
	var (
		plans []Plan
		err   error
	)
	if removal {
		plans, err = service.PlanRemoval()
	} else {
		plans, err = service.Plan()
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Diff == "" || strings.ContainsRune(plans[0].Diff, '\x00') {
		t.Fatalf("unreviewable plan: %#v", plans)
	}
	if !strings.Contains(plans[0].Diff, "規則") {
		t.Fatalf("plan omitted decoded Unicode: %q", plans[0].Diff)
	}
	if removal {
		_, err = service.Remove()
	} else {
		_, err = service.Apply()
	}
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, err := os.ReadFile(target)
	afterExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	after := ""
	if afterExists {
		enc, err := detectEncoding(afterBytes)
		if err != nil {
			t.Fatal(err)
		}
		after, err = decodeText(afterBytes, enc)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte(before), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("patch", "-p1", "--batch", "--silent")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(plans[0].Diff)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("patch rejected decoded diff: %v\n%s\n%s", err, output, plans[0].Diff)
	}
	got, err := os.ReadFile(target)
	gotExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if gotExists != afterExists || (gotExists && string(got) != after) {
		t.Fatalf("decoded patch differs from operation:\n exists got=%t want=%t\n got %q\nwant %q", gotExists, afterExists, got, after)
	}
	if afterExists {
		if err := os.WriteFile(target, afterBytes, 0644); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestPlanDiffsApplyToExactManagedTargetBytes(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "guide.md")
	service, err := New(root, []Target{{Path: "guide.md"}}, "first instruction")
	if err != nil {
		t.Fatal(err)
	}
	assertPlanPatchMatchesApply(t, root, target, service, false)

	if err := os.WriteFile(target, []byte("existing\n"), 0644); err != nil {
		t.Fatal(err)
	}
	assertPlanPatchMatchesApply(t, root, target, service, false)

	if _, err := service.Apply(); err != nil {
		t.Fatal(err)
	}
	service.instructions = "updated instruction\nwith another line"
	assertPlanPatchMatchesApply(t, root, target, service, false)
	assertPlanPatchMatchesApply(t, root, target, service, true)
}

func assertPlanPatchMatchesApply(t *testing.T, root, target string, service *Service, removal bool) {
	t.Helper()
	before, err := os.ReadFile(target)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var plans []Plan
	if removal {
		plans, err = service.PlanRemoval()
	} else {
		plans, err = service.Plan()
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Action == ActionNone || plans[0].Diff == "" {
		t.Fatalf("unexpected plan: %#v", plans)
	}
	if removal {
		_, err = service.Remove()
	} else {
		_, err = service.Apply()
	}
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(target)
	wantExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if existed {
		if err := os.WriteFile(target, before, 0644); err != nil {
			t.Fatal(err)
		}
	} else if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	cmd := exec.Command("patch", "-p1", "--batch", "--silent")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(plans[0].Diff)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("patch rejected diff: %v\n%s\n%s", err, output, plans[0].Diff)
	}
	got, err := os.ReadFile(target)
	gotExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if gotExists != wantExists || (wantExists && !bytes.Equal(got, want)) {
		t.Fatalf("patch result differs from Apply:\n exists got=%t want=%t\n got %q\nwant %q", gotExists, wantExists, got, want)
	}
}

func encodingFor(data []byte) encoding {
	enc, err := detectEncoding(data)
	if err != nil {
		return encodingUTF8
	}
	return enc
}
