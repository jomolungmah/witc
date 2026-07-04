package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// File represents a discovered source file.
type File struct {
	Path string
	Ext  string
}

// Options controls which files a scan collects.
type Options struct {
	// Extensions is the set of file extensions (with leading dot) to collect,
	// e.g. [".go", ".ts", ".tsx"]. It comes from the registered language
	// processors; files with other extensions are skipped.
	Extensions []string
	// IncludeTests keeps test files (e.g. _test.go, *.test.ts, __tests__/) that
	// are skipped by default.
	IncludeTests bool
}

// skipDirs are directory names to skip when walking: dependency and VCS
// directories, plus JS build output that is unambiguously generated (dist,
// .next, coverage). Deliberately not "build" or "out" — those names carry
// real source in some projects; .gitignore handles them where they are output.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"testdata":     true,
	"dist":         true,
	".next":        true,
	"coverage":     true,
}

// isGeneratedFile reports files that match an extension but are never worth
// summarizing: TypeScript declaration files and minified bundles.
func isGeneratedFile(name string) bool {
	return strings.HasSuffix(name, ".d.ts") || strings.HasSuffix(name, ".min.js")
}

// isTestFile reports whether a file is a test by the naming convention of its
// language: Go's _test.go suffix, the JS/TS *.test.* and *.spec.* infixes, and
// anything under a __tests__ directory.
func isTestFile(relSlash string) bool {
	name := relSlash
	if i := strings.LastIndexByte(relSlash, '/'); i >= 0 {
		name = relSlash[i+1:]
	}
	if strings.HasSuffix(name, "_test.go") {
		return true
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".spec") {
		return true
	}
	return strings.HasPrefix(relSlash, "__tests__/") || strings.Contains(relSlash, "/__tests__/")
}

// Scan walks the directory tree and returns source files matching the
// extensions in opts. Paths are relative to root. Skips skipDirs, generated
// files (.d.ts, .min.js), and (unless opts.IncludeTests) test files by each
// language's naming convention. Respects .gitignore if present.
func Scan(root string, opts Options) ([]File, error) {
	exts := make(map[string]bool, len(opts.Extensions))
	for _, e := range opts.Extensions {
		exts[e] = true
	}

	ignorer, _ := gitignore.CompileIgnoreFile(filepath.Join(root, ".gitignore"))

	var files []File
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "." {
			return nil
		}
		if ignorer != nil && ignorer.MatchesPath(relSlash) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if !exts[ext] || isGeneratedFile(d.Name()) {
			return nil
		}
		if !opts.IncludeTests && isTestFile(relSlash) {
			return nil
		}
		files = append(files, File{Path: rel, Ext: ext})
		return nil
	})
	return files, err
}
