package tsparser

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// resolver maps import specifiers to module files. It resolves relative
// specifiers against the importing file's directory and bare specifiers
// against the nearest tsconfig's baseUrl/paths aliases, so monorepos with a
// tsconfig.json inside a frontend/ subdirectory resolve the same as a
// single-app repo; anything that doesn't land on a scanned module file is an
// external package.
type resolver struct {
	// files is the set of scanned module files, slash-separated and relative
	// to the module root. Resolution is answered from this set, not the disk,
	// so skipped files (tests, dist, .d.ts) can't become call-graph targets.
	files map[string]bool
	// configs maps a directory (relative, "." for root) to its parsed
	// tsconfig.json; nil marks a directory checked and found without one.
	configs map[string]*pathsConfig
}

// pathsConfig is the resolution-relevant part of one tsconfig.json, with all
// targets already joined onto the tsconfig's own directory.
type pathsConfig struct {
	// base is the directory bare specifiers resolve against; empty when the
	// tsconfig does not set baseUrl explicitly.
	base string
	// paths maps alias patterns ("@/*") to root-relative target patterns.
	paths map[string][]string
}

// sourceExts are tried, in order, when a specifier omits the extension.
var sourceExts = []string{".ts", ".tsx", ".js", ".jsx"}

func newResolver(root string, relFiles []string) *resolver {
	r := &resolver{
		files:   make(map[string]bool, len(relFiles)),
		configs: map[string]*pathsConfig{},
	}
	for _, f := range relFiles {
		f = filepath.ToSlash(f)
		r.files[f] = true
		// Load the tsconfig.json (if any) of every ancestor directory once, so
		// nearestConfig can answer from the map alone.
		for dir := path.Dir(f); ; dir = path.Dir(dir) {
			if _, seen := r.configs[dir]; !seen {
				r.configs[dir] = loadTSConfig(root, dir)
			}
			if dir == "." {
				break
			}
		}
	}
	return r
}

// tsconfig is the subset of tsconfig.json that affects module resolution.
type tsconfig struct {
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// loadTSConfig parses root/dir/tsconfig.json into a pathsConfig with targets
// joined onto dir, or nil when the file is missing or unusable.
func loadTSConfig(root, dir string) *pathsConfig {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), "tsconfig.json"))
	if err != nil {
		return nil
	}
	var cfg tsconfig
	// tsconfig.json is JSONC; a config that still fails after stripping
	// comments and trailing commas just disables alias resolution.
	if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
		return nil
	}

	pc := &pathsConfig{}
	if raw := cfg.CompilerOptions.BaseURL; raw != "" {
		pc.base = path.Join(dir, filepath.ToSlash(raw))
	}
	// Without an explicit baseUrl, paths targets are relative to the tsconfig.
	targetBase := pc.base
	if targetBase == "" {
		targetBase = dir
	}
	for pattern, targets := range cfg.CompilerOptions.Paths {
		joined := make([]string, 0, len(targets))
		for _, t := range targets {
			joined = append(joined, path.Join(targetBase, filepath.ToSlash(t)))
		}
		if pc.paths == nil {
			pc.paths = map[string][]string{}
		}
		pc.paths[pattern] = joined
	}
	if pc.base == "" && pc.paths == nil {
		return nil // nothing resolution-relevant in this config
	}
	return pc
}

// jsoncTrailingComma removes commas dangling before a closing brace/bracket.
// It runs after comment stripping; on the rare chance a string value contains
// ",}" it would corrupt it, but tsconfig resolution fields never do.
var jsoncTrailingComma = regexp.MustCompile(`,(\s*[}\]])`)

// stripJSONC removes comments and trailing commas so tsconfig.json (which is
// JSONC) parses as JSON. String contents, including "//" in URLs, are preserved.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inStr := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inStr {
			out = append(out, c)
			switch c {
			case '\\':
				if i+1 < len(data) {
					out = append(out, data[i+1])
					i++
				}
			case '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i-- // keep the newline
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i+1 < len(data) && !(data[i] == '*' && data[i+1] == '/') {
				i++
			}
			i++ // consume the closing "/"
		default:
			out = append(out, c)
		}
	}
	return jsoncTrailingComma.ReplaceAll(out, []byte("$1"))
}

// nearestConfig returns the closest tsconfig at or above dir, or nil.
func (r *resolver) nearestConfig(dir string) *pathsConfig {
	for {
		if cfg := r.configs[dir]; cfg != nil {
			return cfg
		}
		if dir == "." {
			return nil
		}
		dir = path.Dir(dir)
	}
}

// resolve maps an import specifier in fromDir to a module file. It returns
// the matched file ("" when the import is external) and, for external
// imports, the package name ("react", "@scope/pkg").
func (r *resolver) resolve(fromDir, spec string) (file, pkg string) {
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
		if f := r.lookup(path.Join(fromDir, spec)); f != "" {
			return f, ""
		}
		return "", "" // relative miss: points at an unscanned file, not a package
	}
	if cfg := r.nearestConfig(fromDir); cfg != nil {
		for pattern, targets := range cfg.paths {
			if rest, ok := matchPattern(pattern, spec); ok {
				for _, target := range targets {
					if f := r.lookup(strings.Replace(target, "*", rest, 1)); f != "" {
						return f, ""
					}
				}
			}
		}
		if cfg.base != "" {
			// An explicit baseUrl lets bare specifiers name module files.
			if f := r.lookup(path.Join(cfg.base, spec)); f != "" {
				return f, ""
			}
		}
	}
	return "", packageName(spec)
}

// matchPattern matches a tsconfig paths pattern ("@/*", "lib") against a
// specifier, returning the part bound to "*".
func matchPattern(pattern, spec string) (rest string, ok bool) {
	prefix, wild := strings.CutSuffix(pattern, "*")
	if !wild {
		return "", pattern == spec
	}
	return strings.CutPrefix(spec, prefix)
}

// lookup finds the module file a specifier path refers to, trying the exact
// path, then implied extensions, then directory index files.
func (r *resolver) lookup(p string) string {
	p = path.Clean(p)
	if r.files[p] {
		return p
	}
	for _, ext := range sourceExts {
		if r.files[p+ext] {
			return p + ext
		}
	}
	for _, ext := range sourceExts {
		if idx := p + "/index" + ext; r.files[idx] {
			return idx
		}
	}
	return ""
}

// packageName extracts the npm package from a bare specifier:
// "react-dom/client" -> "react-dom", "@scope/pkg/sub" -> "@scope/pkg".
func packageName(spec string) string {
	parts := strings.Split(spec, "/")
	if strings.HasPrefix(spec, "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}
