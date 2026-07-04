package tsparser

import (
	"context"
	"testing"

	"github.com/jomolungmah/witc/internal/processor"
)

const sampleTSX = `import React, { useState } from "react";
import { api } from "./api";

/** Props for the user card. */
export interface UserCardProps {
  name: string;
  age?: number;
  onClick: (id: string) => void;
  refresh(force: boolean): void;
}

export type ID = string | number;

export type Point = {
  x: number;
  y: number;
};

/** Renders a user card. */
export function UserCard({ name, onClick }: UserCardProps) {
  const [open, setOpen] = useState(false);
  return <div onClick={() => onClick(name)}>{name}</div>;
}

/**
 * The profile page.
 * @returns the page element
 */
export const ProfilePage = () => {
  const s = new Store();
  return <UserCard name="x" onClick={(id) => api.log(id)} />;
};

/** A simple item store. */
export default class Store {
  items: ID[] = [];
  private cache: Map<string, ID>;
  /** Adds an item. */
  add(item: ID): void {
    this.items.push(item);
    helper(1);
  }
  private evict(): void {}
}

export enum Color {
  Red = 1,
  Green,
}

function helper(x: number): number {
  return x * 2;
}

const format = (id: ID): string => String(id);

export { format };
`

func process(t *testing.T, path, src string) *processor.Result {
	t.Helper()
	p := &Processor{}
	result, err := p.Process(context.Background(), path, []byte(src))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	return result
}

func findInterface(t *testing.T, r *processor.Result, name string) processor.Interface {
	t.Helper()
	for _, iface := range r.Interfaces {
		if iface.Name == name {
			return iface
		}
	}
	t.Fatalf("interface %q not found", name)
	return processor.Interface{}
}

func findFunction(t *testing.T, r *processor.Result, name string) processor.Function {
	t.Helper()
	for _, fn := range r.Functions {
		if fn.Name == name {
			return fn
		}
	}
	t.Fatalf("function %q not found", name)
	return processor.Function{}
}

func TestProcess_Language(t *testing.T) {
	r := process(t, "src/components/App.tsx", sampleTSX)
	if r.Language != "typescript" {
		t.Errorf("Language = %q, want typescript", r.Language)
	}
	if r.Package != "components" {
		t.Errorf("Package = %q, want components", r.Package)
	}
}

func TestProcess_Interface(t *testing.T) {
	r := process(t, "App.tsx", sampleTSX)
	iface := findInterface(t, r, "UserCardProps")
	if !iface.Exported {
		t.Error("UserCardProps should be exported")
	}
	if iface.Doc != "Props for the user card." {
		t.Errorf("Doc = %q", iface.Doc)
	}
	if iface.Loc.Line != 5 {
		t.Errorf("Loc.Line = %d, want 5", iface.Loc.Line)
	}
	wantFields := []processor.Field{
		{Name: "name", Type: "string"},
		{Name: "age?", Type: "number"},
		{Name: "onClick", Type: "(id: string) => void"},
	}
	if len(iface.Fields) != len(wantFields) {
		t.Fatalf("Fields = %+v, want %d fields", iface.Fields, len(wantFields))
	}
	for i, want := range wantFields {
		if iface.Fields[i] != want {
			t.Errorf("Fields[%d] = %+v, want %+v", i, iface.Fields[i], want)
		}
	}
	if len(iface.Methods) != 1 || iface.Methods[0].Name != "refresh" {
		t.Fatalf("Methods = %+v, want [refresh]", iface.Methods)
	}
	if got := iface.Methods[0].Signature; got != "(force: boolean): void" {
		t.Errorf("refresh signature = %q", got)
	}
}

func TestProcess_TypeAliases(t *testing.T) {
	r := process(t, "App.tsx", sampleTSX)

	id := findInterface(t, r, "ID")
	if id.Alias != "string | number" {
		t.Errorf("ID alias = %q, want %q", id.Alias, "string | number")
	}
	if len(id.Fields) != 0 {
		t.Errorf("ID should have no fields, got %+v", id.Fields)
	}

	point := findInterface(t, r, "Point")
	if point.Alias != "" {
		t.Errorf("Point alias = %q, want empty (object type)", point.Alias)
	}
	if len(point.Fields) != 2 || point.Fields[0].Name != "x" {
		t.Errorf("Point fields = %+v", point.Fields)
	}
}

func TestProcess_Enum(t *testing.T) {
	r := process(t, "App.tsx", sampleTSX)
	color := findInterface(t, r, "Color")
	if !color.Exported {
		t.Error("Color should be exported")
	}
	if len(color.Fields) != 2 {
		t.Fatalf("Color fields = %+v", color.Fields)
	}
	if color.Fields[0] != (processor.Field{Name: "Red", Type: "= 1"}) {
		t.Errorf("Fields[0] = %+v", color.Fields[0])
	}
	if color.Fields[1] != (processor.Field{Name: "Green"}) {
		t.Errorf("Fields[1] = %+v", color.Fields[1])
	}
}

func TestProcess_Class(t *testing.T) {
	r := process(t, "App.tsx", sampleTSX)
	if len(r.Structs) != 1 {
		t.Fatalf("Structs = %+v, want 1", r.Structs)
	}
	s := r.Structs[0]
	if s.Name != "Store" || !s.Exported {
		t.Errorf("Struct = %+v, want exported Store", s)
	}
	if s.Doc != "A simple item store." {
		t.Errorf("Doc = %q", s.Doc)
	}
	// Fields include private ones (like Go's unexported struct fields).
	if len(s.Fields) != 2 || s.Fields[0].Name != "items" || s.Fields[0].Type != "ID[]" {
		t.Errorf("Fields = %+v", s.Fields)
	}
	if len(s.Methods) != 2 {
		t.Fatalf("Methods = %+v, want add and evict", s.Methods)
	}
	add := s.Methods[0]
	if add.Name != "add" || !add.Exported || add.Receiver != "Store" {
		t.Errorf("add = %+v", add)
	}
	if add.Signature != "(item: ID): void" {
		t.Errorf("add signature = %q", add.Signature)
	}
	if add.Doc != "Adds an item." {
		t.Errorf("add doc = %q", add.Doc)
	}
	if evict := s.Methods[1]; evict.Name != "evict" || evict.Exported {
		t.Errorf("evict = %+v, want unexported", evict)
	}
}

func TestProcess_Functions(t *testing.T) {
	r := process(t, "App.tsx", sampleTSX)

	card := findFunction(t, r, "UserCard")
	if !card.Exported {
		t.Error("UserCard should be exported")
	}
	if card.Doc != "Renders a user card." {
		t.Errorf("UserCard doc = %q", card.Doc)
	}
	if card.Signature != "({ name, onClick }: UserCardProps)" {
		t.Errorf("UserCard signature = %q", card.Signature)
	}

	page := findFunction(t, r, "ProfilePage")
	if !page.Exported {
		t.Error("ProfilePage (export const arrow) should be exported")
	}
	if page.Doc != "The profile page." {
		t.Errorf("ProfilePage doc = %q (JSDoc tags should be cut)", page.Doc)
	}

	if h := findFunction(t, r, "helper"); h.Exported {
		t.Error("helper should not be exported")
	}
	if h := findFunction(t, r, "helper"); h.Signature != "(x: number): number" {
		t.Errorf("helper signature = %q", h.Signature)
	}

	// format is declared unexported but re-exported via `export { format }`.
	if f := findFunction(t, r, "format"); !f.Exported {
		t.Error("format should be exported via the export clause")
	}
}

func TestProcess_CallRecords(t *testing.T) {
	r := process(t, "App.tsx", sampleTSX)

	parents := func(callee string) []string {
		var out []string
		for _, c := range r.CallGraph[callee] {
			out = append(out, c.ParentFunc)
		}
		return out
	}

	if got := parents("UserCard"); len(got) != 1 || got[0] != "ProfilePage" {
		t.Errorf("UserCard callers = %v, want [ProfilePage] (JSX render edge)", got)
	}
	if got := parents("Store"); len(got) != 1 || got[0] != "ProfilePage" {
		t.Errorf("Store callers = %v, want [ProfilePage] (new expression)", got)
	}
	if got := parents("helper"); len(got) != 1 || got[0] != "Store.add" {
		t.Errorf("helper callers = %v, want [Store.add]", got)
	}
	if got := parents("useState"); len(got) != 1 || got[0] != "UserCard" {
		t.Errorf("useState callers = %v, want [UserCard]", got)
	}
	// Member calls like api.log and this.items.push must not be recorded.
	if _, ok := r.CallGraph["log"]; ok {
		t.Error("member call api.log should not be recorded")
	}
	if _, ok := r.CallGraph["push"]; ok {
		t.Error("member call this.items.push should not be recorded")
	}
}

func TestProcess_PlainTSAllowsTypeAssertions(t *testing.T) {
	src := "const x = <Foo>bar;\nexport function f(a: string): Foo { return <Foo>a; }\n"
	r := process(t, "cast.ts", src)
	fn := findFunction(t, r, "f")
	if fn.Signature != "(a: string): Foo" {
		t.Errorf("signature = %q", fn.Signature)
	}
}

func TestProcess_PlainJSWithJSX(t *testing.T) {
	src := `// The app shell.
export function App() {
  return <Layout title="home" />;
}
function Layout(props) {
  return <div>{props.title}</div>;
}
`
	r := process(t, "app.js", src)
	app := findFunction(t, r, "App")
	if !app.Exported || app.Doc != "The app shell." {
		t.Errorf("App = %+v", app)
	}
	if got := r.CallGraph["Layout"]; len(got) != 1 || got[0].ParentFunc != "App" {
		t.Errorf("Layout callers = %+v, want App", got)
	}
	if _, ok := r.CallGraph["div"]; ok {
		t.Error("intrinsic element div should not be a call record")
	}
}

func TestSupports(t *testing.T) {
	p := &Processor{}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		if !p.Supports(ext) {
			t.Errorf("Supports(%s) = false", ext)
		}
	}
	if p.Supports(".go") || p.Supports(".css") {
		t.Error("Supports should reject non-TS/JS extensions")
	}
}

func TestProcess_ObjectConstAsAPI(t *testing.T) {
	src := `/** Space API client. */
export const spaceApi = {
  baseUrl: "/api/spaces",
  /** Fetches one space. */
  get: (id: string) => fetch("/api/spaces/" + id),
  async create(name: string) { return fetch("/api/spaces"); },
};

export const config = { retries: 3, verbose: true };
`
	r := process(t, "api.ts", src)
	if len(r.Structs) != 1 {
		t.Fatalf("Structs = %+v, want just spaceApi (config has no methods)", r.Structs)
	}
	s := r.Structs[0]
	if s.Name != "spaceApi" || !s.Exported || s.Doc != "Space API client." {
		t.Errorf("spaceApi = %+v", s)
	}
	if len(s.Fields) != 1 || s.Fields[0] != (processor.Field{Name: "baseUrl", Type: "string"}) {
		t.Errorf("Fields = %+v", s.Fields)
	}
	if len(s.Methods) != 2 {
		t.Fatalf("Methods = %+v", s.Methods)
	}
	get := s.Methods[0]
	if get.Name != "get" || get.Receiver != "spaceApi" || get.Signature != "(id: string)" {
		t.Errorf("get = %+v", get)
	}
	if get.Doc != "Fetches one space." {
		t.Errorf("get doc = %q", get.Doc)
	}
	if create := s.Methods[1]; create.Name != "create" || create.Signature != "(name: string)" {
		t.Errorf("create = %+v", create)
	}
}

func TestProcess_FactoryConsts(t *testing.T) {
	src := `import { create } from "zustand";

/** Global space state. */
export const useSpaceStore = create(() => ({ active: null }));
const queryClient = newishFactory();
export const MAX_SPACES = 10;
export const NAMES = ["a", "b"];
`
	r := process(t, "store.ts", src)
	store := findFunction(t, r, "useSpaceStore")
	if !store.Exported || store.Signature != "" || store.Doc != "Global space state." {
		t.Errorf("useSpaceStore = %+v, want exported, empty signature", store)
	}
	if qc := findFunction(t, r, "queryClient"); qc.Exported {
		t.Errorf("queryClient should be unexported, got %+v", qc)
	}
	for _, fn := range r.Functions {
		if fn.Name == "MAX_SPACES" || fn.Name == "NAMES" {
			t.Errorf("literal const %s should not be API surface", fn.Name)
		}
	}
}
