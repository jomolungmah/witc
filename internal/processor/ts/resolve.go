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
// against tsconfig baseUrl/paths aliases; anything that doesn't land on a
// scanned module file is an external package.
type resolver struct {
	// files is the set of scanned module files, slash-separated and relative
	// to the module root. Resolution is answered from this set, not the disk,
	// so skipped files (tests, dist, .d.ts) can't become call-graph targets.
	files map[string]bool
	// baseURL is tsconfig compilerOptions.baseUrl relative to the root ("" if unset).
	baseURL string
	// paths are tsconfig compilerOptions.paths aliases, e.g. "@/*" -> ["src/*"],
	// with targets already joined onto baseUrl.
	paths map[string][]string
}

// sourceExts are tried, in order, when a specifier omits the extension.
var sourceExts = []string{".ts", ".tsx", ".js", ".jsx"}

func newResolver(root string, relFiles []string) *resolver {
	r := &resolver{files: make(map[string]bool, len(relFiles))}
	for _, f := range relFiles {
		r.files[filepath.ToSlash(f)] = true
	}
	r.loadTSConfig(filepath.Join(root, "tsconfig.json"))
	return r
}

// tsconfig is the subset of tsconfig.json that affects module resolution.
type tsconfig struct {
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

func (r *resolver) loadTSConfig(configPath string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	var cfg tsconfig
	// tsconfig.json is JSONC; a config that still fails after stripping
	// comments and trailing commas just disables alias resolution.
	if err := json.Unmarshal(stripJSONC(data), &cfg); err != nil {
		return
	}
	r.baseURL = path.Clean(filepath.ToSlash(cfg.CompilerOptions.BaseURL))
	if r.baseURL == "." || r.baseURL == "/" {
		r.baseURL = ""
	}
	r.baseURL = strings.TrimPrefix(r.baseURL, "./")
	for pattern, targets := range cfg.CompilerOptions.Paths {
		joined := make([]string, 0, len(targets))
		for _, t := range targets {
			joined = append(joined, path.Join(r.baseURL, filepath.ToSlash(t)))
		}
		if r.paths == nil {
			r.paths = map[string][]string{}
		}
		r.paths[pattern] = joined
	}
}

// jsoncTrailingComma removes commas dangling before a closing brace/bracket.
// It runs after comment stripping, on the rare chance a string value contains
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
	for pattern, targets := range r.paths {
		if rest, ok := matchPattern(pattern, spec); ok {
			for _, target := range targets {
				if f := r.lookup(strings.Replace(target, "*", rest, 1)); f != "" {
					return f, ""
				}
			}
		}
	}
	if r.baseURL != "" || len(r.paths) > 0 {
		// With a baseUrl, bare specifiers may name module files from the root.
		if f := r.lookup(path.Join(r.baseURL, spec)); f != "" {
			return f, ""
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
