package formatter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jomolungmah/witc/internal/callgraph"
	"github.com/jomolungmah/witc/internal/processor"
)

// stripFuncKeyword removes a leading "func" keyword from a stored signature so
// method and interface renderings don't read "func ... func(...)".
func stripFuncKeyword(sig string) string {
	return strings.TrimPrefix(sig, "func")
}

// writeDoc renders a one-line doc comment in italics at the given indent.
func writeDoc(b *strings.Builder, doc, indent string) {
	if doc == "" {
		return
	}
	b.WriteString(indent + "_" + doc + "_\n")
}

// writeTypedCalls renders a function's internal callees inline, looked up in the
// type-checked graph by its qualified name (e.g. "pkg.Fn" or "pkg.(*T).M").
// It renders nothing when the name isn't a node (e.g. under the AST fallback).
func writeTypedCalls(b *strings.Builder, cg *callgraph.CallGraph, qualified, indent string) {
	info := cg.GetFunction(qualified)
	if info == nil || len(info.Callees) == 0 {
		return
	}
	b.WriteString(indent + "Calls: " + joinCode(uniqueCalleeNames(info)) + "\n")
}

// Output detail levels.
const (
	detailLow    = "low"
	detailMedium = "medium"
	detailHigh   = "high"
)

// section is one emitted block, tagged with a drop priority and a short name.
// Lower rank is more important and dropped last; ranks 0 (header) and 1 (API
// surface) are core and never dropped wholesale (the API surface is truncated
// instead). The name identifies droppable sections in the omission manifest.
type section struct {
	rank    int
	name    string
	content string
}

// estimateTokens is a cheap heuristic (~4 chars per token) used for budgeting.
func estimateTokens(s string) int {
	return len(s) / 4
}

// detailMaxRank maps a detail level to the highest section rank it emits.
func detailMaxRank(detail string) int {
	switch detail {
	case detailLow:
		return 1 // header + API surface
	case detailMedium:
		return 4 // + structure, call graph, metrics
	default:
		return 7 // everything
	}
}

// Markdown formats the summary as markdown, honouring sum.Detail and
// sum.MaxTokens. Content is organised into ranked sections; when a token budget
// is set, the least important sections are dropped (and the API surface
// truncated) until the estimate fits.
func Markdown(sum *Summary) (string, error) {
	detail := sum.Detail
	if detail == "" {
		detail = detailHigh
	}
	maxRank := detailMaxRank(detail)
	includeInlineCalls := detail == detailHigh

	header := fmt.Sprintf("# witc summary: %s\n\n", sum.Root)

	// Build the non-core sections first so the API budget can reserve room for
	// the header and, worst case, a manifest naming every droppable section.
	structure := structureSection(sum)
	architecture := architectureSection(sum)
	callGraph := callGraphSection(sum.CallGraph)
	metrics := metricsSection(sum.CallGraph)
	callSummary := GenerateCallSummary(sum.CallGraph)
	depMap := GenerateDependencyMap(sum.CallGraph)
	execFlow := execFlowSection(sum.CallGraph)

	// The API surface is core; build it with whatever budget remains after the
	// header and manifest reserve, so it self-degrades via its own ladder rather
	// than being blunt-clamped once those are added back to the output. A
	// negative budget means unlimited; zero means "a real ceiling with no room to
	// spare" — kept distinct so a tiny budget still degrades gracefully instead
	// of being mistaken for unlimited and rendered in full.
	apiBudget := -1
	if sum.MaxTokens > 0 {
		var droppable []string
		for _, s := range []section{
			{2, "structure", structure},
			{2, "architecture", architecture},
			{3, "call graph", callGraph},
			{4, "metrics", metrics},
			{5, "call summary", callSummary},
			{6, "dependency map", depMap},
			{7, "execution flow", execFlow},
		} {
			if s.rank <= maxRank && s.content != "" {
				droppable = append(droppable, s.name)
			}
		}
		reserve := estimateTokens(header) + estimateTokens(omittedSectionsNote(droppable))
		apiBudget = sum.MaxTokens - reserve
		if apiBudget < 0 {
			apiBudget = 0
		}
	}

	// assemble builds the full output for a given API budget. Because
	// estimateTokens floors each piece, the sum-of-floors that apiSection and
	// trimToBudget track can undercount the floor-of-the-whole by a few tokens.
	// We therefore measure the assembled result and, on overflow, shrink the API
	// budget by exactly that slack and rebuild — so graceful degradation absorbs
	// the residue instead of the blunt clamp truncating a whole trailing line.
	assemble := func(apiBudget int) (out string, total int) {
		api := apiSection(sum, includeInlineCalls, detail != detailHigh, apiBudget)

		// Sections in output order; empty ones and those above maxRank are dropped.
		candidates := []section{
			{0, "", header},
			{2, "structure", structure},
			{2, "architecture", architecture},
			{1, "", api},
			{3, "call graph", callGraph},
			{4, "metrics", metrics},
			{5, "call summary", callSummary},
			{6, "dependency map", depMap},
			{7, "execution flow", execFlow},
		}

		var kept []section
		for _, s := range candidates {
			if s.rank <= maxRank && s.content != "" {
				kept = append(kept, s)
			}
		}

		kept, dropped := trimToBudget(kept, sum.MaxTokens)

		var b strings.Builder
		for i, s := range kept {
			b.WriteString(s.content)
			// Surface dropped sections up top (after the header) so the manifest
			// survives the tail-trimming clamp and the reader learns what's missing.
			if i == 0 {
				b.WriteString(omittedSectionsNote(dropped))
			}
		}
		out = b.String()
		return out, estimateTokens(out)
	}

	out, total := assemble(apiBudget)
	// Reconcile accumulation slack: if the measured total overruns the budget,
	// give apiSection exactly that much less room and rebuild. Bounded by the
	// number of pieces, so this settles in a pass or two.
	for over := total - sum.MaxTokens; sum.MaxTokens > 0 && over > 0 && apiBudget > 0; over = total - sum.MaxTokens {
		apiBudget -= over
		if apiBudget < 0 {
			apiBudget = 0
		}
		out, total = assemble(apiBudget)
	}
	return clampTokens(out, sum.MaxTokens), nil
}

// omittedSectionsNote names the whole sections dropped to fit the budget, so a
// silent omission becomes an actionable pointer. Empty when nothing was dropped.
func omittedSectionsNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("_Sections omitted to fit the token budget: %s. Re-run with a higher --max-tokens to include them._\n\n", strings.Join(names, ", "))
}

// clampTokens is the final hard guarantee that output fits the budget. Section
// dropping and symbol truncation handle this gracefully in the common case;
// this only trims residual overhead (package headers, omission notes) by
// dropping whole trailing lines so the estimate never exceeds maxTokens.
func clampTokens(s string, maxTokens int) string {
	if maxTokens <= 0 || estimateTokens(s) <= maxTokens {
		return s
	}
	const marker = "_… output truncated to fit token budget_\n"
	limit := maxTokens*4 - len(marker)
	if limit < 0 || limit >= len(s) {
		return s
	}
	cut := s[:limit]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i+1]
	}
	return cut + marker
}

// trimToBudget drops sections, highest rank first, until the estimated total
// fits within maxTokens (0 = unlimited). Core sections (rank <= 1) are kept. It
// returns the dropped section names (in drop order) and counts the omission
// manifest against the budget as it grows, so the note itself never overflows.
func trimToBudget(secs []section, maxTokens int) (kept []section, dropped []string) {
	if maxTokens <= 0 {
		return secs, nil
	}
	for {
		total := estimateTokens(omittedSectionsNote(dropped))
		for _, s := range secs {
			total += estimateTokens(s.content)
		}
		if total <= maxTokens {
			return secs, dropped
		}
		victim, maxRank := -1, 1
		for i, s := range secs {
			if s.rank > maxRank {
				maxRank, victim = s.rank, i
			}
		}
		if victim == -1 {
			return secs, dropped // only core remains; API was already budget-truncated
		}
		dropped = append(dropped, secs[victim].name)
		secs = append(secs[:victim], secs[victim+1:]...)
	}
}

// architectureSection gives a high-altitude map of the module: entry points and
// a per-package line with its doc, symbol counts, and the in-module packages it
// depends on (calls into). Package dependencies are derived from the typed call
// graph; docs and counts come from the per-file API surface.
func architectureSection(sum *Summary) string {
	if len(sum.Packages) == 0 {
		return ""
	}

	relKeys := make([]string, 0, len(sum.Packages))
	for k := range sum.Packages {
		relKeys = append(relKeys, k)
	}
	sort.Strings(relKeys)

	fullToRel := pkgPathMap(sum.CallGraph, relKeys)
	// Several graph packages can share one display path (a Go package whose
	// directory also holds JS files); their dependencies are unioned.
	relToFulls := make(map[string][]string, len(fullToRel))
	for full, rel := range fullToRel {
		relToFulls[rel] = append(relToFulls[rel], full)
	}
	deps := internalPackageDeps(sum.CallGraph)

	var b strings.Builder
	b.WriteString("## Architecture\n\n")

	if sum.CallGraph != nil {
		if eps := entryPointNames(sum.CallGraph); len(eps) > 0 {
			b.WriteString("Entry points: " + joinCode(capStrings(eps, 10)) + "\n\n")
		}
	}

	for _, rel := range relKeys {
		r := sum.Packages[rel]
		if r == nil {
			continue
		}
		b.WriteString("- `" + rel + "`")
		if r.Doc != "" {
			b.WriteString(" — _" + r.Doc + "_")
		}
		b.WriteString(fmt.Sprintf(" (%d type(s), %d func(s))", len(r.Structs)+len(r.Interfaces), len(r.Functions)))

		depSet := map[string]bool{}
		for _, full := range relToFulls[rel] {
			for dep := range deps[full] {
				if dr := fullToRel[dep]; dr != "" && dr != rel {
					depSet[dr] = true
				}
			}
		}
		if len(depSet) > 0 {
			depRels := make([]string, 0, len(depSet))
			for dr := range depSet {
				depRels = append(depRels, dr)
			}
			sort.Strings(depRels)
			b.WriteString(" → depends on: " + joinCode(depRels))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

// internalPackageDeps derives in-module package → package edges from the call
// graph: a function calling into another module package creates a dependency.
func internalPackageDeps(cg *callgraph.CallGraph) map[string]map[string]bool {
	deps := make(map[string]map[string]bool)
	if cg == nil {
		return deps
	}
	for _, info := range cg.Functions {
		from := info.Package
		for _, callee := range info.Callees {
			target := cg.Functions[callee.Name]
			if target == nil || target.Package == "" || target.Package == from {
				continue
			}
			if deps[from] == nil {
				deps[from] = make(map[string]bool)
			}
			deps[from][target.Package] = true
		}
	}
	return deps
}

// pkgPathMap maps each full import path in the call graph to the matching
// display path used by the API surface (e.g. the module-relative directory).
func pkgPathMap(cg *callgraph.CallGraph, relKeys []string) map[string]string {
	m := make(map[string]string)
	if cg == nil {
		return m
	}
	for _, info := range cg.Functions {
		full := info.Package
		if full == "" || m[full] != "" {
			continue
		}
		best := ""
		for _, rel := range relKeys {
			if full == rel || strings.HasSuffix(full, "/"+rel) {
				if len(rel) > len(best) {
					best = rel
				}
			}
		}
		if best != "" {
			m[full] = best
		}
	}
	return m
}

func structureSection(sum *Summary) string {
	if sum.NoStructure || len(sum.Paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Structure\n\n```\n")
	b.WriteString(filepath.Base(sum.Root) + "\n")
	b.WriteString(treeFromPaths(sum.Paths))
	b.WriteString("```\n\n")
	return b.String()
}

// apiSection renders the package/symbol API surface. When budget >= 0 (a
// negative budget means unlimited), symbols are emitted in importance order
// (types, then exported and unexported functions by call-graph centrality).
// Once the budget is hit, a symbol whose
// full form won't fit degrades to its declaration line before being dropped
// entirely, with per-package notes of how many were degraded and omitted.
func apiSection(sum *Summary, includeInlineCalls, collapseUnexported bool, budget int) string {
	pkgNames := make([]string, 0, len(sum.Packages))
	for k := range sum.Packages {
		pkgNames = append(pkgNames, k)
	}
	sort.Strings(pkgNames)

	var b strings.Builder
	b.WriteString("## Packages\n\n")
	tokens := estimateTokens(b.String())

	// budget < 0 means unlimited; budget >= 0 is a hard token ceiling (0 = no room
	// to spare, which still degrades gracefully rather than rendering in full).
	// Reserve a little headroom so the closing omission notes (which are capped)
	// always fit, keeping the surface within budget without the blunt tail clamp.
	limited := budget >= 0
	contentBudget := max(budget-noteReserve, 0)

	for i, pkg := range pkgNames {
		r := sum.Packages[pkg]
		if r == nil {
			continue
		}
		head := fmt.Sprintf("### %s\n\n", pkg)
		if r.Doc != "" {
			head += "_" + r.Doc + "_\n\n"
		}
		// No room for even this package's header; name the remaining packages so
		// they stay discoverable, then stop. The note is sized to the budget left
		// so it never overflows regardless of how long the package paths are.
		if limited && tokens+estimateTokens(head) > contentBudget {
			var rest []string
			for _, p := range pkgNames[i:] {
				if sum.Packages[p] != nil {
					rest = append(rest, p)
				}
			}
			b.WriteString(omittedPackagesNote(rest, budget-tokens))
			break
		}
		b.WriteString(head)
		tokens += estimateTokens(head)

		omitted, degraded := 0, 0
		var omittedNames []string
		for _, entry := range symbolEntries(r, sum.CallGraph, includeInlineCalls, collapseUnexported) {
			et := estimateTokens(entry.content)
			if limited && tokens+et > contentBudget {
				// Before dropping the symbol, try its declaration-only form so
				// the reader still sees the signature and location.
				if ct := estimateTokens(entry.compact); entry.compact != entry.content && tokens+ct <= contentBudget {
					b.WriteString(entry.compact)
					tokens += ct
					degraded++
					continue
				}
				omitted++
				if entry.name != "" {
					omittedNames = append(omittedNames, entry.name)
				}
				continue
			}
			b.WriteString(entry.content)
			tokens += et
		}
		if degraded > 0 {
			note := fmt.Sprintf("_… %d symbol(s) shown as signature only to fit the budget; use `witc find <name>` for full detail_\n", degraded)
			if !limited || tokens+estimateTokens(note) <= budget {
				b.WriteString(note)
				tokens += estimateTokens(note)
			}
		}
		if omitted > 0 {
			avail := -1
			if limited {
				avail = budget - tokens
			}
			note := omittedSymbolsNote(omitted, omittedNames, avail)
			b.WriteString(note)
			tokens += estimateTokens(note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// noteReserve is the token headroom apiSection holds back from its budget so the
// capped closing notes always fit without the blunt tail clamp. Worst case at a
// truncation boundary is one package's degraded + omitted-symbol notes plus the
// omitted-packages note; each capped note is ~40 tokens, so 130 covers all three.
const noteReserve = 130

// omittedSymbolsNote reports how many symbols a package dropped and names as
// many as fit within avail tokens, in importance order, so the reader can pull
// any one with `witc find <name>` instead of only learning a bare count. It
// shrinks the name list (then falls back to a bare count) so the note never
// exceeds avail; avail < 0 means "no limit" (avail == 0 means no room). Returns
// "" if even a bare count won't fit.
func omittedSymbolsNote(count int, names []string, avail int) string {
	for shown := min(len(names), 8); shown >= 1; shown-- {
		list := strings.Join(names[:shown], ", ")
		if remainder := count - shown; remainder > 0 {
			list += fmt.Sprintf(", … +%d more", remainder)
		}
		note := fmt.Sprintf("_… %d symbol(s) omitted to fit the budget: %s — use `witc find <name>`_\n", count, list)
		if avail < 0 || estimateTokens(note) <= avail {
			return note
		}
	}
	bare := fmt.Sprintf("_… %d more symbol(s) omitted to fit the token budget_\n", count)
	if avail < 0 || estimateTokens(bare) <= avail {
		return bare
	}
	return ""
}

// omittedPackagesNote names the packages dropped whole (their header alone would
// not fit). It shows as many names as fit within avail tokens (up to a cap),
// shrinking to a bare count when even one name won't fit, so the note itself
// never pushes the surface over budget. avail < 0 means "no limit" (avail == 0
// means no room).
func omittedPackagesNote(names []string, avail int) string {
	if len(names) == 0 {
		return ""
	}
	for shown := min(len(names), 8); shown >= 1; shown-- {
		list := strings.Join(names[:shown], ", ")
		if remainder := len(names) - shown; remainder > 0 {
			list += fmt.Sprintf(", … +%d more", remainder)
		}
		note := fmt.Sprintf("_… %d package(s) omitted to fit the budget: %s — use `witc summarize <pkg>` or a higher --max-tokens_\n", len(names), list)
		if avail < 0 || estimateTokens(note) <= avail {
			return note
		}
	}
	return fmt.Sprintf("_… %d package(s) omitted to fit the budget — raise --max-tokens_\n", len(names))
}

// symbolEntry is a rendered symbol block paired with its name, so truncation can
// report which symbols it dropped. The name is empty for summary lines (e.g. the
// collapsed unexported-helper count) that carry no single symbol. compact is a
// declaration-only rendering (the first line: signature + location, no doc,
// methods, or inline calls) used to keep a symbol present when its full form
// won't fit, before dropping it entirely.
type symbolEntry struct {
	name    string
	content string
	compact string
}

// newSymbolEntry pairs a rendered block with its name and derives the compact
// (declaration-line-only) rendering used by the budget-degradation ladder.
func newSymbolEntry(name, content string) symbolEntry {
	return symbolEntry{name: name, content: content, compact: firstLine(content)}
}

// firstLine returns s up to and including its first newline, or all of s when it
// has none. It isolates a symbol's declaration line, which every renderer emits
// first, ahead of the doc/methods/inline-calls that follow.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i+1]
	}
	return s
}

// symbolEntries renders each symbol in a package as a standalone block, ordered
// by importance so the least useful are the first to be truncated: types, then
// exported functions by centrality, then unexported functions by centrality.
func symbolEntries(r *processor.Result, cg *callgraph.CallGraph, includeInlineCalls, collapseUnexported bool) []symbolEntry {
	var entries []symbolEntry

	for _, s := range r.Structs {
		entries = append(entries, newSymbolEntry(s.Name, renderStruct(r, s, cg, includeInlineCalls)))
	}
	for _, iface := range r.Interfaces {
		entries = append(entries, newSymbolEntry(iface.Name, renderInterface(r, iface)))
	}

	var exported, unexported []processor.Function
	for _, fn := range r.Functions {
		if fn.Exported {
			exported = append(exported, fn)
		} else {
			unexported = append(unexported, fn)
		}
	}
	byCentrality := func(fns []processor.Function) {
		sort.SliceStable(fns, func(i, j int) bool {
			ci := centrality(cg, funcQualified(r, fns[i].Name))
			cj := centrality(cg, funcQualified(r, fns[j].Name))
			if ci != cj {
				return ci > cj
			}
			return fns[i].Name < fns[j].Name
		})
	}

	// Exported API leads; unexported helpers follow (or collapse to a count at
	// lower detail levels).
	byCentrality(exported)
	for _, fn := range exported {
		entries = append(entries, newSymbolEntry(fn.Name, renderFunc(r, fn, cg, includeInlineCalls)))
	}

	if collapseUnexported {
		if len(unexported) > 0 {
			entries = append(entries, newSymbolEntry("", fmt.Sprintf("- _%d unexported helper(s) (use --detail high to expand)_\n", len(unexported))))
		}
	} else {
		byCentrality(unexported)
		for _, fn := range unexported {
			entries = append(entries, newSymbolEntry(fn.Name, renderFunc(r, fn, cg, includeInlineCalls)))
		}
	}
	return entries
}

// centrality scores a function's importance as its total call-graph degree.
func centrality(cg *callgraph.CallGraph, qualified string) int {
	if cg == nil {
		return 0
	}
	if info := cg.GetFunction(qualified); info != nil {
		return len(info.Callers) + len(info.Callees)
	}
	return 0
}

// locSuffix renders a terse " — file:line" pointer for a symbol heading, or ""
// when the location is unknown. It lets an agent jump straight to the source.
func locSuffix(loc processor.Location) string {
	if loc.Line == 0 {
		return ""
	}
	return fmt.Sprintf(" — %s:%d", loc.File, loc.Line)
}

// isGoLike reports whether a package renders with Go syntax. An empty language
// means a result predating the language field and keeps the Go rendering.
func isGoLike(r *processor.Result) bool {
	return r.Language == "go" || r.Language == ""
}

// funcQualified returns the call-graph node name for a package-level function.
// Every language's precise graph qualifies nodes as "pkg.Fn"; TS files at the
// module root have no package prefix.
func funcQualified(r *processor.Result, name string) string {
	if r.Package == "" {
		return name
	}
	return r.Package + "." + name
}

// methodQualified is funcQualified for methods, using Go's "pkg.(recv).Name"
// form or the "pkg.Class.method" form the TS builder produces.
func methodQualified(r *processor.Result, m processor.Method) string {
	if isGoLike(r) {
		return r.Package + ".(" + m.Receiver + ")." + m.Name
	}
	return funcQualified(r, m.Receiver+"."+m.Name)
}

// fieldStrings renders fields as "name type" (Go) or "name: type" (TS/JS).
func fieldStrings(fields []processor.Field, goLike bool) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		switch {
		case f.Name == "":
			out = append(out, f.Type)
		case f.Type == "":
			out = append(out, f.Name)
		case goLike:
			out = append(out, f.Name+" "+f.Type)
		default:
			out = append(out, f.Name+": "+f.Type)
		}
	}
	return out
}

func renderStruct(r *processor.Result, s processor.Struct, cg *callgraph.CallGraph, includeInlineCalls bool) string {
	goLike := isGoLike(r)
	head := func(fields []string) string {
		if goLike {
			if len(fields) > 0 {
				return fmt.Sprintf("type %s struct { %s }", s.Name, strings.Join(fields, "; "))
			}
			return fmt.Sprintf("type %s struct", s.Name)
		}
		if len(fields) > 0 {
			return fmt.Sprintf("class %s { %s }", s.Name, strings.Join(fields, "; "))
		}
		return fmt.Sprintf("class %s", s.Name)
	}

	var b strings.Builder
	if len(s.Fields) > 0 || len(s.Methods) > 0 {
		b.WriteString(fmt.Sprintf("- `%s`%s\n", head(fieldStrings(s.Fields, goLike)), locSuffix(s.Loc)))
		writeDoc(&b, s.Doc, "  ")
	}
	for _, m := range s.Methods {
		if goLike {
			b.WriteString(fmt.Sprintf("  - `func (%s) %s%s`%s\n", m.Receiver, m.Name, stripFuncKeyword(m.Signature), locSuffix(m.Loc)))
		} else {
			b.WriteString(fmt.Sprintf("  - `%s%s`%s\n", m.Name, m.Signature, locSuffix(m.Loc)))
		}
		writeDoc(&b, m.Doc, "    ")
		if includeInlineCalls {
			writeTypedCalls(&b, cg, methodQualified(r, m), "    ")
		}
	}
	return b.String()
}

func renderInterface(r *processor.Result, iface processor.Interface) string {
	goLike := isGoLike(r)
	var b strings.Builder

	members := fieldStrings(iface.Fields, goLike)
	for _, m := range iface.Methods {
		if m.Name != "" {
			members = append(members, m.Name+stripFuncKeyword(m.Signature))
		} else {
			members = append(members, m.Signature)
		}
	}

	switch {
	case !goLike && iface.Alias != "":
		b.WriteString(fmt.Sprintf("- `type %s = %s`%s\n", iface.Name, iface.Alias, locSuffix(iface.Loc)))
	case !goLike && len(members) > 0:
		b.WriteString(fmt.Sprintf("- `interface %s { %s }`%s\n", iface.Name, strings.Join(members, "; "), locSuffix(iface.Loc)))
	case !goLike:
		b.WriteString(fmt.Sprintf("- `interface %s`%s\n", iface.Name, locSuffix(iface.Loc)))
	case len(members) > 0:
		b.WriteString(fmt.Sprintf("- `type %s interface { %s }`%s\n", iface.Name, strings.Join(members, "; "), locSuffix(iface.Loc)))
	default:
		b.WriteString(fmt.Sprintf("- `type %s interface`%s\n", iface.Name, locSuffix(iface.Loc)))
	}
	writeDoc(&b, iface.Doc, "  ")
	return b.String()
}

func renderFunc(r *processor.Result, fn processor.Function, cg *callgraph.CallGraph, includeInlineCalls bool) string {
	var b strings.Builder
	sig := fn.Signature
	switch {
	case isGoLike(r) && strings.HasPrefix(sig, "func"):
		sig = "func " + fn.Name + sig[4:]
	case isGoLike(r):
		sig = "func " + fn.Name + " " + sig
	case sig == "": // factory-result const (store, wrapped component)
		sig = "const " + fn.Name
	default:
		sig = "function " + fn.Name + sig
	}
	b.WriteString(fmt.Sprintf("- `%s`%s\n", sig, locSuffix(fn.Loc)))
	writeDoc(&b, fn.Doc, "  ")
	if includeInlineCalls {
		writeTypedCalls(&b, cg, funcQualified(r, fn.Name), "  ")
	}
	return b.String()
}

func callGraphSection(cg *callgraph.CallGraph) string {
	var b strings.Builder
	b.WriteString("\n## Call Graph\n\n")

	if cg == nil || len(cg.Functions) == 0 {
		b.WriteString("*No call graph data available*\n")
		return b.String()
	}

	b.WriteString("### Function Relationships\n\n")
	b.WriteString("| Function | Calls | Called By |\n")
	b.WriteString("|----------|-------|-----------|\n")

	funcNames := make([]string, 0, len(cg.Functions))
	for name := range cg.Functions {
		funcNames = append(funcNames, name)
	}
	sort.Strings(funcNames)

	for _, funcName := range funcNames {
		callList := cg.Functions[funcName]
		callerMap := make(map[string]bool)
		for _, c := range callList.Callers {
			callerMap[c.Name] = true
		}
		callers := make([]string, 0, len(callerMap))
		for name := range callerMap {
			callers = append(callers, name)
		}
		sort.Strings(callers)
		b.WriteString(fmt.Sprintf("| `%s` | %d | %s |\n", funcName, len(callList.Callees), strings.Join(callers, ", ")))
	}

	b.WriteString("\n### Entry Points\n\n")
	if entryPoints := entryPointNames(cg); len(entryPoints) > 0 {
		for _, ep := range entryPoints {
			b.WriteString(fmt.Sprintf("- `%s`\n", ep))
		}
	} else {
		b.WriteString("_No clear entry points detected_\n")
	}

	b.WriteString("\n### Leaf Functions\n\n")
	if leafFuncs := findLeafFunctions(cg); len(leafFuncs) > 0 {
		for _, lf := range leafFuncs {
			b.WriteString(fmt.Sprintf("- `%s`\n", lf))
		}
	} else {
		b.WriteString("_No leaf functions detected_\n")
	}

	b.WriteString("\n### Cross-File Dependencies\n\n")
	showCrossFileDeps(&b, cg)
	return b.String()
}

func metricsSection(cg *callgraph.CallGraph) string {
	var b strings.Builder
	b.WriteString("\n## Metrics\n\n")

	if cg == nil || len(cg.Functions) == 0 {
		b.WriteString("*No call graph data available*\n")
		return b.String()
	}

	metrics := callgraph.CalculateMetrics(cg)
	if metrics.TotalFunctions == 0 {
		b.WriteString("*No metrics available*\n")
		return b.String()
	}

	b.WriteString("### Overview\n\n")
	b.WriteString(fmt.Sprintf("- **Total Functions:** %d\n", metrics.TotalFunctions))
	b.WriteString(fmt.Sprintf("- **Total Calls:** %d\n", metrics.TotalCalls))
	b.WriteString(fmt.Sprintf("- **Average Callees per Function:** %.2f\n", metrics.AvgCalleesPerFunc))

	if metrics.TotalCalls > 0 {
		externalPct := float64(metrics.ExternalCalls) / float64(metrics.TotalCalls) * 100
		b.WriteString(fmt.Sprintf("- **External Calls:** %d (%.1f%%)\n", metrics.ExternalCalls, externalPct))
	}
	if metrics.MaxFanIn != "" {
		b.WriteString(fmt.Sprintf("- **Most Called Function:** `%s` (called by %d functions)\n", metrics.MaxFanIn, metrics.MaxFanInCount))
	}
	if metrics.MaxFanOut != "" {
		b.WriteString(fmt.Sprintf("- **Highest Fan-out:** `%s` (calls %d other functions)\n", metrics.MaxFanOut, metrics.MaxFanOutCount))
	}

	if len(metrics.HighCouplingFuncs) > 0 {
		b.WriteString("\n### High Coupling Functions\n\n")
		b.WriteString("Functions with many dependencies (may indicate refactoring opportunities):\n\n")
		displayCount := len(metrics.HighCouplingFuncs)
		if displayCount > 10 {
			displayCount = 10
		}
		for _, fn := range metrics.HighCouplingFuncs[:displayCount] {
			b.WriteString(fmt.Sprintf("- `%s`\n", fn))
		}
	}
	return b.String()
}

// execFlowSection traces execution flow for a handful of entry points that
// actually drive calls, so the section stays useful without dumping everything.
func execFlowSection(cg *callgraph.CallGraph) string {
	if cg == nil {
		return ""
	}
	var b strings.Builder
	const maxFlows = 6
	count := 0
	for _, ep := range entryPointNames(cg) {
		info := cg.GetFunction(ep)
		if info == nil || len(info.Callees) == 0 {
			continue
		}
		b.WriteString(GenerateCallFlow(ep, cg))
		if count++; count >= maxFlows {
			break
		}
	}
	return b.String()
}

func findLeafFunctions(cg *callgraph.CallGraph) []string {
	var leaves []string
	for funcName := range cg.Functions {
		if len(cg.Functions[funcName].Callees) == 0 {
			leaves = append(leaves, funcName)
		}
	}
	leaves = deduplicateStrings(leaves)
	sort.Strings(leaves)
	return leaves
}

func showCrossFileDeps(b *strings.Builder, cg *callgraph.CallGraph) {
	hasCrossFile := false

	for callee := range cg.Functions {
		filesForCallee := make(map[string]bool)
		for _, callerInfo := range cg.Functions {
			for _, caller := range callerInfo.Callers {
				if caller.Name == callee {
					filesForCallee[caller.File] = true
				}
			}
		}

		if len(filesForCallee) > 1 {
			hasCrossFile = true
			break
		}
	}

	if hasCrossFile {
		b.WriteString("Functions called from multiple files:\n\n")
		for callee := range cg.Functions {
			files := make(map[string]bool)
			for _, callerInfo := range cg.Functions {
				for _, caller := range callerInfo.Callers {
					if caller.Name == callee {
						files[caller.File] = true
					}
				}
			}

			if len(files) > 1 {
				b.WriteString(fmt.Sprintf("- `%s` (called from %d files)\n", callee, len(files)))
				for f := range files {
					b.WriteString(fmt.Sprintf("  - `%s`\n", filepath.Base(f)))
				}
			}
		}
	} else {
		b.WriteString("_No cross-file dependencies detected_\n")
	}
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

func deduplicateStrings(s []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, str := range s {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}
