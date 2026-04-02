package codemogger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type scannedFile struct {
	AbsPath  string
	RelPath  string
	Language string
	Hash     string
	Content  string
}

var alwaysIgnore = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"target":       {},
	"build":        {},
	"dist":         {},
	".next":        {},
	"__pycache__":  {},
	".tox":         {},
	".venv":        {},
	"venv":         {},
	".mypy_cache":  {},
	".cargo":       {},
	".rustup":      {},
}

func loadIgnorePatterns(content string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		clean := strings.TrimSuffix(trimmed, "/")
		if clean == "" || strings.Contains(clean, "*") {
			continue
		}
		out[clean] = struct{}{}
	}
	return out
}

func scanDirectory(ctx context.Context, rootDir string, languages []string) ([]scannedFile, []string) {
	files := make([]scannedFile, 0)
	errors := make([]string, 0)

	ignorePatterns := make(map[string]struct{})
	if content, err := os.ReadFile(filepath.Join(rootDir, ".gitignore")); err == nil {
		ignorePatterns = loadIgnorePatterns(string(content))
	}
	langFilter := normalizeLanguageNames(languages)

	walkErr := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errors = append(errors, "cannot read "+path+": "+err.Error())
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name != "." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			if _, ok := alwaysIgnore[name]; ok {
				return filepath.SkipDir
			}
			if _, ok := ignorePatterns[name]; ok {
				return filepath.SkipDir
			}
			return nil
		}
		if name != "." && strings.HasPrefix(name, ".") {
			return nil
		}
		if _, ok := ignorePatterns[name]; ok {
			return nil
		}
		cfg := detectLanguage(name)
		if cfg == nil {
			return nil
		}
		if langFilter != nil {
			if _, ok := langFilter[cfg.Name]; !ok {
				return nil
			}
		}

		info, statErr := d.Info()
		if statErr != nil {
			errors = append(errors, "cannot read "+path+": "+statErr.Error())
			return nil
		}
		if size := info.Size(); size == 0 || size > 1_000_000 {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			errors = append(errors, "cannot read "+path+": "+readErr.Error())
			return nil
		}
		sum := sha256.Sum256(content)
		relPath, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			relPath = path
		}

		files = append(files, scannedFile{
			AbsPath:  path,
			RelPath:  relPath,
			Language: cfg.Name,
			Hash:     hex.EncodeToString(sum[:]),
			Content:  string(content),
		})
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		errors = append(errors, walkErr.Error())
	}

	return files, errors
}
