// analyze_handlers performs Go AST-based analysis of HTTP handler files.
//
// It produces a JSON report consumed by api-audit.py, replacing the regex-based
// heuristics that broke on raw-string literals, pointer-typed variables, and
// cross-package type aliases.
//
// Usage:
//
//	go run ./build/scripts/analyze_handlers/ \
//	  --repo ../meshery \
//	  --schemas-models ./models
//
// Stdout: JSON (see AnalysisOutput).
// Stderr: warnings only.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

// HandlerInfo holds the analysis result for one handler function.
type HandlerInfo struct {
	File              string  `json:"file"`
	SchemaImportUsage string  `json:"schema_import_usage"` // "TRUE", "Partial", "FALSE"
	SchemaReason      string  `json:"schema_reason"`
	RequestType       *string `json:"request_type"`  // null when not found
	ResponseType      *string `json:"response_type"` // null when not found
}

// AnalysisOutput is the top-level JSON object written to stdout.
type AnalysisOutput struct {
	SchemaModule string                  `json:"schema_module"`
	Handlers     map[string]*HandlerInfo `json:"handlers"`
	// TypeAliases maps a locally-defined type name to the full schema import path
	// it aliases.  Example: "ConnectionPage" → "github.com/meshery/schemas/models/v1beta1/connection"
	TypeAliases map[string]string `json:"type_aliases"`
	// StructFields maps a type name to its JSON field names extracted from json tags.
	StructFields map[string][]string `json:"struct_fields"`
}

// ---------------------------------------------------------------------------
// CLI flags
// ---------------------------------------------------------------------------

var (
	repoFlag          = flag.String("repo", "", "Path to target repo root (required)")
	handlersDirFlag   = flag.String("handlers-dir", "server/handlers", "Relative path to handlers dir within repo")
	modelsDirFlag     = flag.String("models-dir", "server/models", "Relative path to models dir within repo")
	schemasModelsFlag = flag.String("schemas-models", "", "Absolute path to schemas repo models/ dir")
	schemaModuleFlag  = flag.String("schema-module", "github.com/meshery/schemas", "Schema module import path")
)

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	flag.Parse()

	if *repoFlag == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --repo is required")
		os.Exit(1)
	}

	out := &AnalysisOutput{
		SchemaModule: *schemaModuleFlag,
		Handlers:     make(map[string]*HandlerInfo),
		TypeAliases:  make(map[string]string),
		StructFields: make(map[string][]string),
	}

	handlersDir := filepath.Join(*repoFlag, *handlersDirFlag)
	modelsDir := filepath.Join(*repoFlag, *modelsDirFlag)

	// 1. Build transitive alias map from local models:
	//    type LocalName = schemaPkg.RemoteName → records LocalName → schema import path
	scanAliases(modelsDir, *schemaModuleFlag, out.TypeAliases)

	// 2. Scan handler files for per-handler analysis.
	//    Also picks up struct definitions from handler files.
	scanHandlers(handlersDir, *schemaModuleFlag, out.TypeAliases, out)

	// 3. Collect struct field maps from local models dir.
	scanStructFields(modelsDir, out.StructFields)

	// 4. Collect struct field maps from schemas models dir (authoritative — scanned
	//    last so schemas definitions overwrite any local redefinitions).
	if *schemasModelsFlag != "" {
		scanStructFields(*schemasModelsFlag, out.StructFields)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR encoding JSON output:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Step 1: transitive alias scanner
// ---------------------------------------------------------------------------

// scanAliases walks dir recursively and records type aliases that point to
// schema packages.  Only one level of alias indirection is followed.
func scanAliases(dir, schemaModule string, aliases map[string]string) {
	fset := token.NewFileSet()
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: walk error in %s: %v\n", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: parse error in %s: %v\n", path, parseErr)
			return nil
		}
		schemaImports := fileSchemaImports(f, schemaModule)
		if len(schemaImports) == 0 {
			return nil
		}
		for _, decl := range f.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range genDecl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				// ts.Assign.IsValid() is true for "type X = Y" (alias), false for "type X Y" (def)
				if !ok || !ts.Assign.IsValid() {
					continue
				}
				// Unwrap pointer/slice wrappers so both
				//   type X = pkg.Y      (*ast.SelectorExpr)
				//   type X = *pkg.Y     (*ast.StarExpr → *ast.SelectorExpr)
				//   type X = []pkg.Y    (*ast.ArrayType → *ast.SelectorExpr)
				// are recognised as schema aliases.
				typeExpr := ts.Type
				switch w := typeExpr.(type) {
				case *ast.StarExpr:
					typeExpr = w.X
				case *ast.ArrayType:
					typeExpr = w.Elt
				}
				sel, ok := typeExpr.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				if importPath, found := schemaImports[pkgIdent.Name]; found {
					aliases[ts.Name.Name] = importPath
				}
			}
		}
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: unable to walk aliases dir %s: %v\n", dir, err)
	}
}

// ---------------------------------------------------------------------------
// Step 2: handler scanner
// ---------------------------------------------------------------------------

// scanHandlers parses all non-test .go files in dir (non-recursive) and
// analyses every handler method found.
func scanHandlers(dir, schemaModule string, typeAliases map[string]string, out *AnalysisOutput) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot read handlers dir %s: %v\n", dir, err)
		return
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: parse error in %s: %v\n", path, parseErr)
			continue
		}

		schemaImports := fileSchemaImports(f, schemaModule)

		// Collect struct fields from handler files too
		extractStructFieldsFromFile(f, out.StructFields)

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !isHandlerMethod(fn) {
				continue
			}
			info := analyseHandler(fn, path, schemaModule, schemaImports, typeAliases)
			out.Handlers[fn.Name.Name] = info
		}
	}
}

// analyseHandler derives the HandlerInfo for a single function declaration.
func analyseHandler(
	fn *ast.FuncDecl,
	filePath, schemaModule string,
	schemaImports, typeAliases map[string]string,
) *HandlerInfo {
	info := &HandlerInfo{File: filePath}

	// ── Direct schema usage ────────────────────────────────────────────────
	usedImports := findUsedSchemaImports(fn.Body, schemaImports)
	info.SchemaImportUsage, info.SchemaReason = classifyDirectUsage(usedImports, schemaModule)

	// ── Request type ──────────────────────────────────────────────────────
	reqVar := findJSONDecodeVar(fn.Body)
	if reqVar != "" {
		t := resolveVarType(fn.Body, reqVar)
		if t != "" {
			info.RequestType = strPtr(t)
			// Upgrade schema usage via transitive alias if direct check missed it
			if info.SchemaImportUsage == "FALSE" {
				if imp, ok := typeAliases[bareTypeName(t)]; ok {
					info.SchemaImportUsage, info.SchemaReason =
						classifyByImportPath(imp, schemaModule, "alias: "+bareTypeName(t))
				}
			}
		}
	}

	// ── Response type ─────────────────────────────────────────────────────
	respExpr := findJSONEncodeExpr(fn.Body)
	if respExpr != "" {
		t := resolveExprType(fn.Body, respExpr)
		if t != "" {
			info.ResponseType = strPtr(t)
			// Upgrade schema usage via transitive alias if direct check missed it
			if info.SchemaImportUsage == "FALSE" {
				if imp, ok := typeAliases[bareTypeName(t)]; ok {
					info.SchemaImportUsage, info.SchemaReason =
						classifyByImportPath(imp, schemaModule, "alias: "+bareTypeName(t))
				}
			}
		}
	}

	if info.SchemaImportUsage == "" {
		info.SchemaImportUsage = "FALSE"
		info.SchemaReason = "no schema imports or aliases found"
	}
	return info
}

// ---------------------------------------------------------------------------
// Step 3 & 4: struct field scanner
// ---------------------------------------------------------------------------

// scanStructFields walks dir recursively and collects JSON field names for
// every struct type it finds.  Entries written later overwrite earlier ones,
// so callers should scan local models before the authoritative schemas models.
func scanStructFields(dir string, fields map[string][]string) {
	fset := token.NewFileSet()
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: walk error in %s: %v\n", path, err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "WARNING: parse error in %s: %v\n", path, parseErr)
			return nil
		}
		extractStructFieldsFromFile(f, fields)
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: unable to walk struct fields dir %s: %v\n", dir, err)
	}
}

// extractStructFieldsFromFile harvests json tag field names from all struct
// definitions in one parsed file.
func extractStructFieldsFromFile(f *ast.File, fields map[string][]string) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			jsonFields := collectJSONFields(st)
			if len(jsonFields) > 0 {
				fields[ts.Name.Name] = jsonFields
			}
		}
	}
}

// collectJSONFields returns the json field names from a struct type.
func collectJSONFields(st *ast.StructType) []string {
	var result []string
	for _, field := range st.Fields.List {
		if field.Tag == nil {
			continue
		}
		// Tag.Value is a raw string literal including backtick delimiters.
		tag := strings.Trim(field.Tag.Value, "`")
		name := jsonTagName(tag)
		if name != "" && name != "-" {
			result = append(result, name)
		}
	}
	return result
}

// jsonTagName extracts the field name from a struct tag string.
// e.g. `json:"field_name,omitempty" db:"field_name"` → "field_name"
func jsonTagName(tag string) string {
	val := reflect.StructTag(tag).Get("json")
	if val == "" {
		return ""
	}
	if comma := strings.IndexByte(val, ','); comma >= 0 {
		val = val[:comma]
	}
	return strings.TrimSpace(val)
}

// ---------------------------------------------------------------------------
// Import helpers
// ---------------------------------------------------------------------------

// fileSchemaImports returns a map of alias → import path for all imports in
// f whose path contains schemaModule.
func fileSchemaImports(f *ast.File, schemaModule string) map[string]string {
	result := make(map[string]string)
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !strings.Contains(path, schemaModule) {
			continue
		}
		var alias string
		if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
			alias = imp.Name.Name
		} else {
			parts := strings.Split(strings.TrimRight(path, "/"), "/")
			alias = parts[len(parts)-1]
		}
		result[alias] = path
	}
	return result
}

// ---------------------------------------------------------------------------
// Schema usage classification
// ---------------------------------------------------------------------------

// findUsedSchemaImports walks the function body and returns all schema import
// aliases that appear as the X in a SelectorExpr (i.e. alias.Something).
func findUsedSchemaImports(body *ast.BlockStmt, aliases map[string]string) map[string]string {
	used := make(map[string]string)
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if importPath, found := aliases[ident.Name]; found {
			used[ident.Name] = importPath
		}
		return true
	})
	return used
}

// classifyDirectUsage derives schema_import_usage from directly-observed alias usage.
func classifyDirectUsage(usedImports map[string]string, schemaModule string) (status, reason string) {
	if len(usedImports) == 0 {
		return "FALSE", "no direct schema import usage in function body"
	}
	var versioned, coreOnly []string
	for _, path := range usedImports {
		rel := strings.TrimPrefix(path, schemaModule+"/")
		if strings.Contains(rel, "models/v") {
			versioned = append(versioned, rel)
		} else if strings.Contains(rel, "models/core") {
			coreOnly = append(coreOnly, rel)
		}
	}
	if len(versioned) > 0 {
		return "TRUE", "direct: " + strings.Join(versioned, ", ")
	}
	if len(coreOnly) > 0 {
		return "Partial", "direct: models/core only (" + strings.Join(coreOnly, ", ") + ")"
	}
	return "FALSE", "schema import present but no versioned model types used"
}

// classifyByImportPath returns schema_import_usage based on a full schema import path.
// prefix is prepended to the reason string.
func classifyByImportPath(importPath, schemaModule, prefix string) (status, reason string) {
	rel := strings.TrimPrefix(importPath, schemaModule+"/")
	if strings.Contains(rel, "models/v") {
		return "TRUE", prefix + " → " + rel
	}
	if strings.Contains(rel, "models/core") {
		return "Partial", prefix + " → models/core"
	}
	return "FALSE", "schema alias but not a versioned model type"
}

// ---------------------------------------------------------------------------
// JSON decode/encode detection
// ---------------------------------------------------------------------------

// findJSONDecodeVar finds the variable name used as the decode target in the
// first json.NewDecoder(...).Decode(arg) or json.Unmarshal(data, arg) call.
// Handles both &var and var (already-pointer) forms.
func findJSONDecodeVar(body *ast.BlockStmt) string {
	var result string
	ast.Inspect(body, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Decode":
			// Accept any .Decode(arg) whose receiver is a NewDecoder call or a stored var.
			if !isJSONDecoderExpr(sel.X) {
				return true
			}
			if len(call.Args) > 0 {
				if v := identNameFromExpr(call.Args[0]); v != "" {
					result = v
				}
			}
		case "Unmarshal":
			// json.Unmarshal(data, arg)
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "json" {
				return true
			}
			if len(call.Args) >= 2 {
				if v := identNameFromExpr(call.Args[1]); v != "" {
					result = v
				}
			}
		}
		return true
	})
	return result
}

// isJSONDecoderExpr returns true if expr is json.NewDecoder(...) or any
// identifier (a stored decoder variable).
func isJSONDecoderExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == "json" && sel.Sel.Name == "NewDecoder"
	case *ast.Ident:
		// Stored decoder: dec := json.NewDecoder(...); dec.Decode(...)
		return true
	}
	return false
}

// findJSONEncodeExpr finds the expression encoded in the first
// json.NewEncoder(w).Encode(expr), enc.Encode(expr), or json.Marshal(expr) call.
// Returns the expression as a string, or "" if not found.
func findJSONEncodeExpr(body *ast.BlockStmt) string {
	var result string
	ast.Inspect(body, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Encode":
			if !isJSONEncoderExpr(sel.X) {
				return true
			}
			if len(call.Args) > 0 {
				result = exprString(call.Args[0])
			}
		case "Marshal":
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "json" {
				return true
			}
			if len(call.Args) > 0 {
				result = exprString(call.Args[0])
			}
		}
		return true
	})
	return result
}

// isJSONEncoderExpr returns true if expr is json.NewEncoder(...) or any identifier.
func isJSONEncoderExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == "json" && sel.Sel.Name == "NewEncoder"
	case *ast.Ident:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Variable type resolution
// ---------------------------------------------------------------------------

// resolveVarType searches the function body for the declared type of varName.
// Handles: var x *T, var x T, x := &T{}, x := T{}, x := make([]T, n).
// Returns the type string, or "" if not found.
func resolveVarType(body *ast.BlockStmt, varName string) string {
	var result string
	ast.Inspect(body, func(n ast.Node) bool {
		if result != "" {
			return false
		}
		switch stmt := n.(type) {
		case *ast.DeclStmt:
			gd, ok := stmt.Decl.(*ast.GenDecl)
			if !ok {
				return true
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if name.Name != varName {
						continue
					}
					if vs.Type != nil {
						result = typeExprString(vs.Type)
						return false
					}
					// var x = expr — infer from RHS
					if len(vs.Values) > 0 {
						result = typeFromRHS(vs.Values[0])
						return false
					}
				}
			}
		case *ast.AssignStmt:
			for i, lhs := range stmt.Lhs {
				ident, ok := lhs.(*ast.Ident)
				if !ok || ident.Name != varName {
					continue
				}
				if i < len(stmt.Rhs) {
					t := typeFromRHS(stmt.Rhs[i])
					if t != "" {
						result = t
						return false
					}
				}
			}
		}
		return true
	})
	return result
}

// resolveExprType resolves the type of an expression used in json.Encode/Marshal.
// For simple variable names it delegates to resolveVarType.
// For "pkg.VarName" forms it returns the expression unchanged.
func resolveExprType(body *ast.BlockStmt, expr string) string {
	// Strip leading & or *
	clean := strings.TrimLeft(expr, "&*")
	if clean == "" {
		return ""
	}
	// Normalize composite literal expressions like "pkg.TypeName{}" to "pkg.TypeName"
	// so downstream bare-type matching works consistently.
	clean = strings.TrimSuffix(clean, "{}")
	// If the expression already contains a dot it may be a package-qualified type or
	// value reference; return the normalized form as-is.
	if strings.Contains(clean, ".") {
		return clean
	}
	// Simple identifier — try to resolve via declaration
	return resolveVarType(body, clean)
}

// typeFromRHS extracts a type string from an RHS expression:
//
//	&pkg.Type{}  → "*pkg.Type"
//	pkg.Type{}   → "pkg.Type"
//	make([]T, n) → "[]T"
func typeFromRHS(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op.String() == "&" {
			t := typeFromRHS(e.X)
			if t != "" {
				return "*" + t
			}
		}
	case *ast.CompositeLit:
		if e.Type != nil {
			return typeExprString(e.Type)
		}
	case *ast.CallExpr:
		// make([]T, n) or make(map[K]V, n)
		if ident, ok := e.Fun.(*ast.Ident); ok && ident.Name == "make" {
			if len(e.Args) > 0 {
				return typeExprString(e.Args[0])
			}
		}
	}
	return ""
}

// typeExprString converts a type expression to a string.
func typeExprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		inner := typeExprString(e.X)
		if inner != "" {
			return "*" + inner
		}
	case *ast.SelectorExpr:
		x := typeExprString(e.X)
		if x != "" {
			return x + "." + e.Sel.Name
		}
	case *ast.ArrayType:
		inner := typeExprString(e.Elt)
		if inner != "" {
			return "[]" + inner
		}
	case *ast.MapType:
		k := typeExprString(e.Key)
		v := typeExprString(e.Value)
		if k != "" && v != "" {
			return "map[" + k + "]" + v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Expression helpers
// ---------------------------------------------------------------------------

// identNameFromExpr extracts an identifier name from an expression.
// Handles: ident → name, &ident → name, *ident → name.
func identNameFromExpr(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.UnaryExpr:
		if ident, ok := e.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.StarExpr:
		if ident, ok := e.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// exprString returns a string representation of simple expressions.
// Used for response expression detection (non-type inference).
func exprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.UnaryExpr:
		inner := exprString(e.X)
		if inner != "" {
			return e.Op.String() + inner
		}
	case *ast.StarExpr:
		inner := exprString(e.X)
		if inner != "" {
			return "*" + inner
		}
	case *ast.SelectorExpr:
		x := exprString(e.X)
		if x != "" {
			return x + "." + e.Sel.Name
		}
	case *ast.CompositeLit:
		if e.Type != nil {
			t := typeExprString(e.Type)
			if t != "" {
				return t + "{}"
			}
		}
	}
	return ""
}

// bareTypeName strips pointer/slice prefixes and package qualifiers.
// Examples: "*connections.ConnectionPage" → "ConnectionPage", "[]T" → "T"
func bareTypeName(t string) string {
	t = strings.TrimLeft(t, "*[]")
	t = strings.TrimSuffix(t, "{}")
	if dot := strings.LastIndex(t, "."); dot >= 0 {
		return t[dot+1:]
	}
	return t
}

// ---------------------------------------------------------------------------
// Handler method detection
// ---------------------------------------------------------------------------

// isHandlerMethod returns true if the function has a receiver type whose name
// contains "Handler" (matches both *Handler and *Handlers).
func isHandlerMethod(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	ident, ok := recv.(*ast.Ident)
	return ok && strings.Contains(ident.Name, "Handler")
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }
