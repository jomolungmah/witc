package formatter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Markdown formats the summary as markdown.
func Markdown(sum *Summary) (string, error) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# witc summary: %s\n\n", sum.Root))

	if !sum.NoStructure && len(sum.Paths) > 0 {
		b.WriteString("## Structure\n\n```\n")
		b.WriteString(filepath.Base(sum.Root) + "\n")
		b.WriteString(treeFromPaths(sum.Paths))
		b.WriteString("```\n\n")
	}

	// Sort packages for deterministic output
	pkgNames := make([]string, 0, len(sum.Packages))
	for k := range sum.Packages {
		pkgNames = append(pkgNames, k)
	}
	sort.Strings(pkgNames)

	b.WriteString("## Packages\n\n")
	for _, pkg := range pkgNames {
		r := sum.Packages[pkg]
		if r == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", pkg))

		for _, s := range r.Structs {
			// Struct with fields summary
			if len(s.Fields) > 0 {
				fieldStrs := make([]string, 0, len(s.Fields))
				for _, f := range s.Fields {
					if f.Name != "" {
						fieldStrs = append(fieldStrs, f.Name+" "+f.Type)
					} else {
						fieldStrs = append(fieldStrs, f.Type)
					}
				}
				b.WriteString(fmt.Sprintf("- `type %s struct { %s }`\n", s.Name, strings.Join(fieldStrs, "; ")))
			} else if len(s.Methods) > 0 {
				b.WriteString(fmt.Sprintf("- `type %s struct`\n", s.Name))
			}
			for _, m := range s.Methods {
				b.WriteString(fmt.Sprintf("  - `func (%s) %s %s`\n", m.Receiver, m.Name, m.Signature))
			}
		}

		for _, iface := range r.Interfaces {
			if len(iface.Methods) > 0 {
				methStrs := make([]string, 0, len(iface.Methods))
				for _, m := range iface.Methods {
					if m.Name != "" {
						methStrs = append(methStrs, m.Name+" "+m.Signature)
					} else {
						methStrs = append(methStrs, m.Signature)
					}
				}
				b.WriteString(fmt.Sprintf("- `type %s interface { %s }`\n", iface.Name, strings.Join(methStrs, "; ")))
			} else {
				b.WriteString(fmt.Sprintf("- `type %s interface`\n", iface.Name))
			}
		}

		for _, fn := range r.Functions {
			sig := fn.Signature
			if strings.HasPrefix(sig, "func") {
				sig = "func " + fn.Name + sig[4:]
			} else {
				sig = "func " + fn.Name + " " + sig
			}
			b.WriteString(fmt.Sprintf("- `%s`\n", sig))
		}

		b.WriteString("\n")
	}

	return b.String(), nil
}

func treeFromPaths(paths []string) string {
	sort.Strings(paths)
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	for i, p := range paths {
		prefix := "├── "
		if i == len(paths)-1 {
			prefix = "└── "
		}
		b.WriteString(prefix + p + "\n")
	}
	return b.String()
}
