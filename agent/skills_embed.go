package agent

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
)

// embeddedSkills bundles the built-in skill definitions into the binary so the
// SDK works from any working directory (no CWD-relative path probing).
//
//go:embed all:skills
var embeddedSkills embed.FS

// materializeEmbeddedSkills extracts the embedded built-in skills to a stable,
// content-addressed cache directory and returns its path. The path is keyed by
// a hash of the embedded content, so a new SDK version re-extracts to a fresh
// dir and stale skills are never reused. Returns "" if the cache dir cannot be
// resolved or written, in which case the caller degrades to no built-in skills.
func materializeEmbeddedSkills() string {
	hash, err := embeddedSkillsHash()
	if err != nil {
		return ""
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dest := filepath.Join(base, "deep-agent-ginga", "skills-"+hash)
	if _, err := os.Stat(dest); err == nil {
		return dest // already materialized for this content
	}

	tmp := dest + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := extractEmbeddedSkills(tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return ""
	}
	if err := os.Rename(tmp, dest); err != nil {
		// A concurrent process may have created dest first; tolerate that.
		_ = os.RemoveAll(tmp)
		if _, statErr := os.Stat(dest); statErr == nil {
			return dest
		}
		return ""
	}
	return dest
}

func extractEmbeddedSkills(destRoot string) error {
	return fs.WalkDir(embeddedSkills, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("skills", p)
		if err != nil {
			return err
		}
		target := filepath.Join(destRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := embeddedSkills.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func embeddedSkillsHash() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(embeddedSkills, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := embeddedSkills.ReadFile(p)
		if err != nil {
			return err
		}
		h.Write([]byte(p))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}
