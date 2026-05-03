package patterns

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromEnvRegexAcceptsMatch(t *testing.T) {
	m, err := FromEnvRegex(`(?i)Awesome\.Torrent`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	out := m.Evaluate(`release Awesome.Torrent FLAC`)
	if !out.Accepted {
		t.Fatalf("expected accept, got rejects: %+v", out)
	}
}

func TestLoadYAMLSubstringAndGlob(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "patterns.yaml")
	payload := `
match_field: torrent
patterns:
  - name: substring-flac
    substring: FLAC
  - name: widescreen-glob
    glob: "*2160p*"
`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	m, err := LoadYAML(path)
	if err != nil {
		t.Fatalf("load yaml: %v", err)
	}
	if m.FileMatchField != "torrent" {
		t.Fatalf("expected match_field torrent, got %q", m.FileMatchField)
	}
	if out := m.Evaluate(`My FLAC Release`); !out.Accepted {
		t.Fatalf("substring eval failed %+v", out)
	}
	if out := m.Evaluate(`Stuff 2160p BluRay`); !out.Accepted {
		t.Fatalf("glob eval failed %+v", out)
	}
	if out := m.Evaluate(`No match here`); out.Accepted {
		t.Fatalf("expected reject, matched %+v", out)
	}
}
