package scanner

import (
	"io/fs"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

// File represents a discovered source file.
type File struct {
	Path string
	Ext  string
}

// skipDirs are directory names to skip when walking.
var skipDirs = map[string]bool{
	"vendor":      true,
	"node_modules": true,
	".git":        true,
	"testdata":    true,
}

// Scan walks the directory tree and returns discovered source files.
// Paths are relative to root. Skips vendor, node_modules, .git, testdata.
// Respects .gitignore if present at root.
func Scan(root string) ([]File, error) {
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
		if ext != ".go" {
			return nil
		}
		files = append(files, File{Path: rel, Ext: ext})
		return nil
	})
	return files, err
}
