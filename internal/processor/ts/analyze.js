// witc TypeScript sidecar: resolves call/new/JSX targets with the TypeScript
// type checker and emits them as JSON for the Go side to turn into a call
// graph. Run as: node analyze.js, with a request on stdin:
//
//   { "root": "/abs/project", "groups": [ { "tsconfig": "frontend/tsconfig.json" | "", "files": ["rel/paths.ts", ...] } ] }
//
// and WITC_TSLIB pointing at the typescript package to use (normally the
// analyzed project's own node_modules/typescript). Response on stdout:
//
//   { "decls": [{"file","func"}], "edges": [{"fromFile","fromFunc","toFile","toFunc","line"}], "externals": [{"fromFile","module"}] }
//
// Naming matches witc's import-resolving builder: "Fn", "Class.method",
// "apiObject.member", "Iface.method"; the Go side adds the package prefix.
"use strict";

const fs = require("fs");
const path = require("path");
const ts = require(process.env.WITC_TSLIB || "typescript");

const req = JSON.parse(fs.readFileSync(0, "utf8"));
const root = req.root;
const out = { decls: [], edges: [], externals: [] };

const toRel = (p) => path.relative(root, p).split(path.sep).join("/");
const toAbs = (p) => path.resolve(root, p);

for (const group of req.groups) {
  analyzeGroup(group);
}
process.stdout.write(JSON.stringify(out));

function analyzeGroup(group) {
  const options = compilerOptions(group);
  const files = group.files.map(toAbs);
  const program = ts.createProgram(files, options);
  const checker = program.getTypeChecker();
  const ours = new Set(group.files);

  for (const sf of program.getSourceFiles()) {
    const relFile = toRel(sf.fileName);
    if (!ours.has(relFile)) continue;
    collectDecls(sf, relFile);
    walk(sf, sf, relFile, checker);
  }
}

function compilerOptions(group) {
  if (group.tsconfig) {
    const cfgPath = toAbs(group.tsconfig);
    const read = ts.readConfigFile(cfgPath, ts.sys.readFile);
    if (!read.error) {
      const parsed = ts.parseJsonConfigFileContent(read.config, ts.sys, path.dirname(cfgPath));
      const o = parsed.options;
      // Solution-style configs (Vite's tsconfig.json with only "references")
      // keep the real options in the referenced configs; fill missing keys
      // from them so paths aliases and jsx settings still apply.
      for (const ref of parsed.projectReferences || []) {
        const refPath = ts.resolveProjectReferencePath(ref);
        const refRead = ts.readConfigFile(refPath, ts.sys.readFile);
        if (refRead.error) continue;
        const refParsed = ts.parseJsonConfigFileContent(refRead.config, ts.sys, path.dirname(refPath));
        for (const k of Object.keys(refParsed.options)) {
          if (o[k] === undefined) o[k] = refParsed.options[k];
        }
      }
      o.noEmit = true;
      o.skipLibCheck = true;
      o.allowJs = true;
      if (o.jsx === undefined) o.jsx = ts.JsxEmit.Preserve;
      return o;
    }
  }
  return {
    allowJs: true,
    checkJs: false,
    noEmit: true,
    skipLibCheck: true,
    jsx: ts.JsxEmit.Preserve,
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.Bundler || ts.ModuleResolutionKind.NodeJs,
    esModuleInterop: true,
  };
}

// collectDecls registers every named function-like declaration so uncalled
// functions still become graph nodes.
function collectDecls(node, relFile) {
  if (isFunctionLike(node)) {
    const name = declName(node);
    if (name) out.decls.push({ file: relFile, func: name });
  }
  ts.forEachChild(node, (c) => collectDecls(c, relFile));
}

function walk(node, sf, relFile, checker) {
  let target = null;
  if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
    target = unwrap(node.expression);
  } else if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
    const tag = node.tagName;
    // Intrinsic elements (<div>) are lowercase identifiers; skip them.
    if (!(ts.isIdentifier(tag) && /^[a-z]/.test(tag.text))) target = tag;
  }
  if (target) {
    reference(node, target, sf, relFile, checker);
  }
  ts.forEachChild(node, (c) => walk(c, sf, relFile, checker));
}

// reference resolves what a call/new/JSX target refers to and records an
// internal edge or an external call.
function reference(site, target, sf, relFile, checker) {
  const from = callerName(site);
  if (!from) return; // module-level statement, not part of any function

  const decl = resolveDecl(target, checker);
  if (!decl) return; // locals, parameters, unresolvable: no stable node

  const line = sf.getLineAndCharacterOfPosition(target.getStart(sf)).line + 1;
  const declFile = decl.getSourceFile().fileName;
  const idx = declFile.lastIndexOf("/node_modules/");
  if (idx >= 0) {
    out.externals.push({ fromFile: relFile, module: packageName(declFile.slice(idx + "/node_modules/".length)) });
    return;
  }
  const toFile = toRel(declFile);
  if (toFile.startsWith("..") || decl.getSourceFile().isDeclarationFile) {
    out.externals.push({ fromFile: relFile, module: "" }); // lib.d.ts globals etc.
    return;
  }
  const toFunc = declName(decl);
  if (!toFunc) return;
  out.edges.push({ fromFile: relFile, fromFunc: from, toFile, toFunc, line });
}

// resolveDecl finds the declaration behind an expression: the symbol at the
// reference (through import aliases), preferring its value declaration. This
// deliberately resolves the *callee symbol*, not the call signature — calling
// a store created by zustand's create() must point at the store const in the
// project, not at zustand's .d.ts.
function resolveDecl(expr, checker) {
  let sym = checker.getSymbolAtLocation(expr);
  if (sym && sym.flags & ts.SymbolFlags.Alias) {
    try {
      sym = checker.getAliasedSymbol(sym);
    } catch {
      return null;
    }
  }
  if (!sym) return null;
  return sym.valueDeclaration || (sym.declarations && sym.declarations[0]) || null;
}

function unwrap(e) {
  while (
    ts.isParenthesizedExpression(e) ||
    ts.isNonNullExpression(e) ||
    (ts.isAsExpression && ts.isAsExpression(e)) ||
    (ts.isSatisfiesExpression && ts.isSatisfiesExpression(e))
  ) {
    e = e.expression;
  }
  return e;
}

function isFunctionLike(n) {
  return (
    ts.isFunctionDeclaration(n) ||
    ts.isMethodDeclaration(n) ||
    ts.isConstructorDeclaration(n) ||
    ts.isGetAccessor(n) ||
    ts.isSetAccessor(n) ||
    ts.isArrowFunction(n) ||
    ts.isFunctionExpression(n)
  );
}

// callerName walks outward to the nearest enclosing declaration with a stable
// name, so calls inside local callbacks attribute to the containing component
// or function.
function callerName(node) {
  for (let n = node.parent; n; n = n.parent) {
    if (isFunctionLike(n)) {
      const name = declName(n);
      if (name) return name;
    }
  }
  return null;
}

// declName produces witc's name for a declaration: "Fn" for module-level
// functions and function/factory consts, "Class.method" for class members,
// "apiObject.member" for module-level object consts, "Iface.method" for
// interface and object-type members, "Class" for classes. Returns null for
// locals and anonymous values.
function declName(d) {
  if (ts.isFunctionDeclaration(d)) {
    return d.name ? d.name.text : null;
  }
  if (ts.isClassDeclaration(d)) {
    return d.name ? d.name.text : null;
  }
  if (ts.isConstructorDeclaration(d)) {
    const owner = memberOwner(d.parent);
    return owner ? owner + ".constructor" : null;
  }
  if (ts.isMethodDeclaration(d) || ts.isGetAccessor(d) || ts.isSetAccessor(d) || ts.isPropertyAssignment(d)) {
    const member = memberName(d.name);
    const owner = memberOwner(d.parent);
    return member && owner ? owner + "." + member : null;
  }
  if (ts.isMethodSignature(d) || ts.isPropertySignature(d)) {
    const member = memberName(d.name);
    const owner = memberOwner(d.parent);
    return member && owner ? owner + "." + member : null;
  }
  if (ts.isVariableDeclaration(d)) {
    return moduleLevelVarName(d);
  }
  if (ts.isArrowFunction(d) || ts.isFunctionExpression(d)) {
    if (ts.isVariableDeclaration(d.parent)) return moduleLevelVarName(d.parent);
    if (ts.isPropertyAssignment(d.parent) || ts.isPropertyDeclaration(d.parent)) return declName(d.parent);
    return null;
  }
  if (ts.isPropertyDeclaration(d) && d.initializer && isFunctionLike(d.initializer)) {
    // class field holding an arrow function: handler = () => {}
    const member = memberName(d.name);
    const owner = memberOwner(d.parent);
    return member && owner ? owner + "." + member : null;
  }
  return null;
}

// memberOwner names the containing class, interface, type alias, or
// module-level object const of a member declaration.
function memberOwner(parent) {
  if (!parent) return null;
  if (ts.isClassDeclaration(parent) || ts.isInterfaceDeclaration(parent)) {
    return parent.name ? parent.name.text : null;
  }
  if (ts.isTypeLiteralNode(parent) && ts.isTypeAliasDeclaration(parent.parent)) {
    return parent.parent.name.text;
  }
  if (ts.isObjectLiteralExpression(parent) && ts.isVariableDeclaration(parent.parent)) {
    return moduleLevelVarName(parent.parent);
  }
  return null;
}

function memberName(name) {
  if (!name) return null;
  if (ts.isIdentifier(name) || ts.isStringLiteral(name) || ts.isPrivateIdentifier(name)) return name.text;
  return null;
}

// moduleLevelVarName returns a variable declarator's name only when it is a
// top-level statement, so local closures don't become package-level nodes.
function moduleLevelVarName(d) {
  if (!ts.isIdentifier(d.name)) return null;
  const stmt = d.parent && d.parent.parent; // VariableDeclarationList -> VariableStatement
  if (!stmt || !ts.isVariableStatement(stmt) || !ts.isSourceFile(stmt.parent)) return null;
  return d.name.text;
}

// packageName extracts the npm package from a node_modules-relative path:
// "react-dom/client.d.ts" -> "react-dom", "@scope/pkg/x.d.ts" -> "@scope/pkg".
function packageName(p) {
  const parts = p.split("/");
  if (parts[0].startsWith("@") && parts.length >= 2) return parts[0] + "/" + parts[1];
  return parts[0];
}
