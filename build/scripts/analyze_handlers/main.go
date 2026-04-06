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
	File               string   `json:"file"`
	SchemaImportUsage  string   `json:"schema_import_usage"` // "TRUE", "Partial", "FALSE"
	SchemaReason       string   `json:"schema_reason"`
	RequestTypes       []string `json:"request_types"`         // all decoded types found
	ResponseTypes      []string `json:"response_types"`        // all encoded types found
	BodyReadViaReadAll bool     `json:"body_read_via_readall"` // true if io.ReadAll/ioutil.ReadAll detected
}

// RouteEntry represents a single registered HTTP route.
type RouteEntry struct {
	Path      string   `json:"path"`
	Methods   []string `json:"methods"`
	Handler   string   `json:"handler"`
	Commented bool     `json:"commented"`
}

// AnalysisOutput is the top-level JSON object written to stdout.
type AnalysisOutput struct {
	SchemaModule string                  `json:"schema_module"`
	Handlers     map[string]*HandlerInfo `json:"handlers"`
	// TypeAliases maps a package-qualified local type name to the full schema
	// import path it aliases. Example:
	// "models.ConnectionPage" → "github.com/meshery/schemas/models/v1beta1/connection"
	TypeAliases map[string]string `json:"type_aliases"`
	// StructFields maps a package-qualified type name to its JSON field names
	// extracted from json tags.
	StructFields map[string][]string `json:"struct_fields"`
	// Routes contains registered HTTP routes extracted from the router file.
	// Only populated when --router-file is provided.
	Routes []RouteEntry `json:"routes,omitempty"`
}

type StructDef struct {
	Type           *ast.StructType
	PackageName    string
	ImportPackages map[string]string
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
	routerFileFlag    = flag.String("router-file", "", "Path to router file for route extraction (optional)")
	routerDialectFlag = flag.String("router-dialect", "gorilla", "Router framework: gorilla or echo")
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

	// allStructDefs accumulates AST struct definitions across all scanned
	// directories so embedded struct fields can be resolved after scanning.
	allStructDefs := make(map[string]StructDef)

	handlersDir := filepath.Join(*repoFlag, *handlersDirFlag)
	modelsDir := filepath.Join(*repoFlag, *modelsDirFlag)

	// 1. Build transitive alias map from local models:
	//    type LocalName = schemaPkg.RemoteName → records LocalName → schema import path
	scanAliases(modelsDir, *schemaModuleFlag, out.TypeAliases)

	// 2. Scan handler files for per-handler analysis.
	//    Also picks up struct definitions from handler files.
	scanHandlers(handlersDir, *schemaModuleFlag, out.TypeAliases, out, allStructDefs)

	// 3. Collect struct field maps from local models dir.
	scanStructFields(modelsDir, out.StructFields, allStructDefs)

	// 4. Collect struct field maps from schemas models dir (authoritative — scanned
	//    last so schemas definitions overwrite any local redefinitions).
	if *schemasModelsFlag != "" {
		scanStructFields(*schemasModelsFlag, out.StructFields, allStructDefs)
	}

	// 5. Resolve embedded struct fields: re-compute JSON fields for all structs
	//    using the full set of struct definitions collected across all directories.
	resolveEmbeddedFields(out.StructFields, allStructDefs)

	// 6. Parse router file for route extraction (optional).
	if *routerFileFlag != "" {
		routes, err := parseRouterFile(*routerFileFlag, *routerDialectFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: router parsing failed: %v\n", err)
		} else {
			out.Routes = routes
		}
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
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: walk error in %s: %v\n", path, err)
			return nil
		}
		if d.IsDir() {
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
		pkgName := filePackageName(f)
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
					aliases[qualifiedTypeName(pkgName, ts.Name.Name)] = importPath
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

// scanHandlers parses all non-test .go files in dir (recursively) and
// analyses every handler method found.
func scanHandlers(dir, schemaModule string, typeAliases map[string]string, out *AnalysisOutput, allStructDefs map[string]StructDef) {
	fset := token.NewFileSet()
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: walk error in %s: %v\n", path, err)
			return nil
		}
		if d.IsDir() {
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

		pkgName := filePackageName(f)
		schemaImports := fileSchemaImports(f, schemaModule)
		importPackages := fileImportPackageNames(f)

		// Collect struct fields and definitions from handler files too
		extractStructFieldsFromFile(f, pkgName, out.StructFields, allStructDefs)

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !isHandlerMethod(fn) {
				continue
			}
			info := analyseHandler(fn, path, pkgName, schemaModule, schemaImports, typeAliases, importPackages)
			out.Handlers[fn.Name.Name] = info
		}
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: unable to walk handlers dir %s: %v\n", dir, err)
	}
}

// analyseHandler derives the HandlerInfo for a single function declaration.
func analyseHandler(
	fn *ast.FuncDecl,
	filePath, pkgName, schemaModule string,
	schemaImports, typeAliases, importPackages map[string]string,
) *HandlerInfo {
	info := &HandlerInfo{File: filePath}

	// ── Direct schema usage ────────────────────────────────────────────────
	usedImports := findUsedSchemaImports(fn.Body, schemaImports)
	info.SchemaImportUsage, info.SchemaReason = classifyDirectUsage(usedImports, schemaModule)

	// ── Request types (all json.Decode/Unmarshal targets) ─────────────────
	reqVars := findAllJSONDecodeVars(fn.Body)
	for _, reqVar := range reqVars {
		t := resolveVarType(fn.Body, reqVar, pkgName, importPackages)
		if t != "" {
			info.RequestTypes = append(info.RequestTypes, t)
			if info.SchemaImportUsage == "FALSE" {
				if imp, ok := typeAliases[typeLookupKey(t, pkgName, importPackages)]; ok {
					info.SchemaImportUsage, info.SchemaReason =
						classifyByImportPath(imp, schemaModule, "alias: "+bareTypeName(t))
				}
			}
		}
	}

	// ── Response types (all json.Encode/Marshal expressions) ──────────────
	respExprs := findAllJSONEncodeExprs(fn.Body)
	for _, respExpr := range respExprs {
		t := resolveExprType(fn.Body, respExpr, pkgName, importPackages)
		if t != "" {
			info.ResponseTypes = append(info.ResponseTypes, t)
			if info.SchemaImportUsage == "FALSE" {
				if imp, ok := typeAliases[typeLookupKey(t, pkgName, importPackages)]; ok {
					info.SchemaImportUsage, info.SchemaReason =
						classifyByImportPath(imp, schemaModule, "alias: "+bareTypeName(t))
				}
			}
		}
	}

	// ── io.ReadAll / ioutil.ReadAll detection ─────────────────────────────
	info.BodyReadViaReadAll = findBodyReadAll(fn.Body)

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
// allStructDefs accumulates raw AST definitions for later embedded-field resolution.
func scanStructFields(dir string, fields map[string][]string, allStructDefs map[string]StructDef) {
	fset := token.NewFileSet()
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: walk error in %s: %v\n", path, err)
			return nil
		}
		if d.IsDir() {
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
		extractStructFieldsFromFile(f, filePackageName(f), fields, allStructDefs)
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: unable to walk struct fields dir %s: %v\n", dir, err)
	}
}

// extractStructFieldsFromFile harvests json tag field names from all struct
// definitions in one parsed file and records raw AST definitions for embedded-field
// resolution.
func extractStructFieldsFromFile(f *ast.File, pkgName string, fields map[string][]string, allStructDefs map[string]StructDef) {
	importPackages := fileImportPackageNames(f)
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
			typeKey := qualifiedTypeName(pkgName, ts.Name.Name)
			// Record AST definition for embedded-field resolution later.
			allStructDefs[typeKey] = StructDef{
				Type:           st,
				PackageName:    pkgName,
				ImportPackages: importPackages,
			}
			// Collect direct (non-embedded) JSON fields as a first pass.
			// Embedded fields are resolved in resolveEmbeddedFields after all scanning.
			jsonFields := collectJSONFields(st)
			if len(jsonFields) > 0 {
				fields[typeKey] = jsonFields
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

// resolveEmbeddedFields re-computes JSON field lists for all structs by
// recursively expanding embedded (anonymous) struct fields.  This must be
// called after all directories have been scanned so that allStructDefs
// contains definitions from both local models and schema models.
func resolveEmbeddedFields(fields map[string][]string, allStructDefs map[string]StructDef) {
	for name, def := range allStructDefs {
		visited := map[string]bool{name: true}
		resolved := collectJSONFieldsRecursive(def, allStructDefs, visited)
		if len(resolved) > 0 {
			fields[name] = resolved
		}
	}
}

// collectJSONFieldsRecursive returns json field names from a struct type,
// recursively expanding anonymous (embedded) fields.  The visited set
// prevents infinite recursion from self-referential struct embeddings.
func collectJSONFieldsRecursive(def StructDef, allStructDefs map[string]StructDef, visited map[string]bool) []string {
	var result []string
	for _, field := range def.Type.Fields.List {
		// Named field with a json tag — normal case.
		if field.Tag != nil {
			tag := strings.Trim(field.Tag.Value, "`")
			name := jsonTagName(tag)
			if name != "" && name != "-" {
				result = append(result, name)
			}
			continue
		}
		// Anonymous (embedded) field — no names and typically no tag.
		// Resolve the embedded type and recursively collect its fields.
		if len(field.Names) == 0 {
			embeddedName := embeddedTypeKey(field.Type, def.PackageName, def.ImportPackages)
			if embeddedName == "" || visited[embeddedName] {
				continue
			}
			visited[embeddedName] = true
			if embDef, ok := allStructDefs[embeddedName]; ok {
				result = append(result, collectJSONFieldsRecursive(embDef, allStructDefs, visited)...)
			}
		}
	}
	return result
}

// embeddedTypeKey extracts the package-qualified type key from an embedded field's
// type expression. Handles: Ident (T), StarExpr (*T), SelectorExpr (pkg.T), *pkg.T.
func embeddedTypeKey(expr ast.Expr, currentPkg string, importPackages map[string]string) string {
	// Unwrap pointer
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return qualifiedTypeName(currentPkg, e.Name)
	case *ast.SelectorExpr:
		pkgIdent, ok := e.X.(*ast.Ident)
		if !ok {
			return ""
		}
		pkgName := importPackages[pkgIdent.Name]
		if pkgName == "" {
			pkgName = pkgIdent.Name
		}
		return qualifiedTypeName(pkgName, e.Sel.Name)
	}
	return ""
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

func filePackageName(f *ast.File) string {
	if f != nil && f.Name != nil {
		return f.Name.Name
	}
	return ""
}

func fileImportPackageNames(f *ast.File) map[string]string {
	result := make(map[string]string)
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path == "" {
			continue
		}
		if imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
			continue
		}
		alias := importPathBase(path)
		if imp.Name != nil && imp.Name.Name != "" {
			alias = imp.Name.Name
		}
		result[alias] = importPathBase(path)
	}
	return result
}

func importPathBase(path string) string {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
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

// findAllJSONDecodeVars finds ALL variable names used as decode targets in
// json.NewDecoder(...).Decode(arg) or json.Unmarshal(data, arg) calls.
// Handles both &var and var (already-pointer) forms.
func findAllJSONDecodeVars(body *ast.BlockStmt) []string {
	decoderVars := findJSONDecoderVars(body)
	seen := make(map[string]bool)
	var result []string
	ast.Inspect(body, func(n ast.Node) bool {
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
			if !isJSONDecoderExpr(sel.X, decoderVars) {
				return true
			}
			if len(call.Args) > 0 {
				if v := identNameFromExpr(call.Args[0]); v != "" && !seen[v] {
					seen[v] = true
					result = append(result, v)
				}
			}
		case "Unmarshal":
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "json" {
				return true
			}
			if len(call.Args) >= 2 {
				if v := identNameFromExpr(call.Args[1]); v != "" && !seen[v] {
					seen[v] = true
					result = append(result, v)
				}
			}
		}
		return true
	})
	return result
}

func findJSONDecoderVars(body *ast.BlockStmt) map[string]bool {
	result := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
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
				for i, value := range vs.Values {
					if !isJSONNewDecoderCall(value) || i >= len(vs.Names) {
						continue
					}
					result[vs.Names[i].Name] = true
				}
			}
		case *ast.AssignStmt:
			for i, rhs := range stmt.Rhs {
				if !isJSONNewDecoderCall(rhs) || i >= len(stmt.Lhs) {
					continue
				}
				if ident, ok := stmt.Lhs[i].(*ast.Ident); ok {
					result[ident.Name] = true
				}
			}
		}
		return true
	})
	return result
}

func isJSONNewDecoderCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "json" && sel.Sel.Name == "NewDecoder"
}

// isJSONDecoderExpr returns true if expr is json.NewDecoder(...) or a stored
// decoder variable previously assigned from json.NewDecoder(...).
func isJSONDecoderExpr(expr ast.Expr, decoderVars map[string]bool) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		return isJSONNewDecoderCall(e)
	case *ast.Ident:
		return decoderVars[e.Name]
	}
	return false
}

// findAllJSONEncodeExprs finds ALL expressions encoded via
// json.NewEncoder(w).Encode(expr), enc.Encode(expr), or json.Marshal(expr) calls.
// Returns expressions as strings.
func findAllJSONEncodeExprs(body *ast.BlockStmt) []string {
	seen := make(map[string]bool)
	var result []string
	ast.Inspect(body, func(n ast.Node) bool {
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
				if s := exprString(call.Args[0]); s != "" && !seen[s] {
					seen[s] = true
					result = append(result, s)
				}
			}
		case "Marshal":
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok || pkgIdent.Name != "json" {
				return true
			}
			if len(call.Args) > 0 {
				if s := exprString(call.Args[0]); s != "" && !seen[s] {
					seen[s] = true
					result = append(result, s)
				}
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
func resolveVarType(body *ast.BlockStmt, varName, currentPkg string, importPackages map[string]string) string {
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
						result = canonicalizeTypeRef(typeExprString(vs.Type), currentPkg, importPackages)
						return false
					}
					// var x = expr — infer from RHS
					if len(vs.Values) > 0 {
						result = typeFromRHS(vs.Values[0], currentPkg, importPackages)
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
					t := typeFromRHS(stmt.Rhs[i], currentPkg, importPackages)
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
func resolveExprType(body *ast.BlockStmt, expr, currentPkg string, importPackages map[string]string) string {
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
		return canonicalizeTypeRef(clean, currentPkg, importPackages)
	}
	// Simple identifier — try to resolve via declaration
	if resolved := resolveVarType(body, clean, currentPkg, importPackages); resolved != "" {
		return resolved
	}
	if isLocalTypeName(clean) {
		return canonicalizeTypeRef(clean, currentPkg, importPackages)
	}
	return ""
}

// typeFromRHS extracts a type string from an RHS expression:
//
//	&pkg.Type{}  → "*pkg.Type"
//	pkg.Type{}   → "pkg.Type"
//	make([]T, n) → "[]T"
func typeFromRHS(expr ast.Expr, currentPkg string, importPackages map[string]string) string {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if e.Op.String() == "&" {
			t := typeFromRHS(e.X, currentPkg, importPackages)
			if t != "" {
				return "*" + t
			}
		}
	case *ast.CompositeLit:
		if e.Type != nil {
			return canonicalizeTypeRef(typeExprString(e.Type), currentPkg, importPackages)
		}
	case *ast.CallExpr:
		// make([]T, n) or make(map[K]V, n)
		if ident, ok := e.Fun.(*ast.Ident); ok && ident.Name == "make" {
			if len(e.Args) > 0 {
				return canonicalizeTypeRef(typeExprString(e.Args[0]), currentPkg, importPackages)
			}
		}
		if ident, ok := e.Fun.(*ast.Ident); ok && ident.Name == "new" {
			if len(e.Args) > 0 {
				return canonicalizeTypeRef("*"+typeExprString(e.Args[0]), currentPkg, importPackages)
			}
		}
	}
	return ""
}

func qualifiedTypeName(pkgName, typeName string) string {
	if pkgName == "" || typeName == "" {
		return typeName
	}
	return pkgName + "." + typeName
}

func canonicalizeTypeRef(typeName, currentPkg string, importPackages map[string]string) string {
	if typeName == "" {
		return ""
	}

	prefix := ""
	for {
		switch {
		case strings.HasPrefix(typeName, "*"):
			prefix += "*"
			typeName = typeName[1:]
		case strings.HasPrefix(typeName, "[]"):
			prefix += "[]"
			typeName = typeName[2:]
		default:
			goto normalize
		}
	}

normalize:
	if typeName == "" {
		return prefix
	}
	if strings.Contains(typeName, ".") {
		parts := strings.SplitN(typeName, ".", 2)
		pkgName := importPackages[parts[0]]
		if pkgName == "" {
			pkgName = parts[0]
		}
		return prefix + qualifiedTypeName(pkgName, parts[1])
	}
	if isLocalTypeName(typeName) {
		return prefix + qualifiedTypeName(currentPkg, typeName)
	}
	return prefix + typeName
}

func typeLookupKey(typeName, currentPkg string, importPackages map[string]string) string {
	key := canonicalizeTypeRef(strings.TrimSuffix(typeName, "{}"), currentPkg, importPackages)
	for strings.HasPrefix(key, "*") {
		key = key[1:]
	}
	for strings.HasPrefix(key, "[]") {
		key = key[2:]
	}
	return key
}

func isLocalTypeName(typeName string) bool {
	if typeName == "" || builtinTypeNames[typeName] {
		return false
	}
	for i, r := range typeName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

var builtinTypeNames = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true,
	"complex128": true, "error": true, "float32": true, "float64": true, "int": true,
	"int8": true, "int16": true, "int32": true, "int64": true, "rune": true,
	"string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true,
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

// isHandlerMethod returns true if the function is a handler:
//   - Method with receiver type containing "Handler" (Gorilla Mux pattern), OR
//   - Function (no receiver) whose first parameter is echo.Context (Echo pattern).
func isHandlerMethod(fn *ast.FuncDecl) bool {
	// Check receiver-based pattern (Gorilla Mux: func (h *Handler) Foo(...))
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recv := fn.Recv.List[0].Type
		if star, ok := recv.(*ast.StarExpr); ok {
			recv = star.X
		}
		ident, ok := recv.(*ast.Ident)
		if ok && strings.Contains(ident.Name, "Handler") {
			return true
		}
	}
	// Check Echo-style handler: func Foo(c echo.Context) or func (s *Server) Foo(c echo.Context)
	if fn.Type != nil && fn.Type.Params != nil {
		for _, param := range fn.Type.Params.List {
			if isEchoContextType(param.Type) {
				return true
			}
		}
	}
	return false
}

// isEchoContextType returns true if the type expression refers to echo.Context.
func isEchoContextType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	return ok && pkgIdent.Name == "echo" && sel.Sel.Name == "Context"
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// findBodyReadAll returns true if the function body contains a call to
// io.ReadAll or ioutil.ReadAll, indicating the handler reads the request
// body as raw bytes rather than via json.Decode/Unmarshal.
func findBodyReadAll(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
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
		if sel.Sel.Name != "ReadAll" {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if ok && (pkgIdent.Name == "io" || pkgIdent.Name == "ioutil") {
			found = true
		}
		return true
	})
	return found
}

// ---------------------------------------------------------------------------
// Router file parsing
// ---------------------------------------------------------------------------

// middlewareNames lists known middleware wrapper function names.
// These are stripped when extracting the actual handler from a middleware chain.
var middlewareNames = map[string]bool{
	"ProviderMiddleware":        true,
	"AuthMiddleware":            true,
	"SessionInjectorMiddleware": true,
	"KubernetesMiddleware":      true,
	"K8sFSMMiddleware":          true,
	"GraphqlMiddleware":         true,
	"NoCacheMiddleware":         true,
}

// parseRouterFile parses a Go router file and extracts registered routes.
func parseRouterFile(filePath, dialect string) ([]RouteEntry, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}

	switch dialect {
	case "gorilla":
		return parseGorillaRoutes(f, fset), nil
	case "echo":
		return parseEchoRoutes(f, fset), nil
	default:
		return nil, fmt.Errorf("unknown router dialect: %s", dialect)
	}
}

// parseGorillaRoutes extracts routes from a Gorilla Mux router file.
// It walks all function bodies looking for chains like:
//
//	gMux.Handle("/path", handler).Methods("GET", "POST")
//	gMux.HandleFunc("/path", handler).Methods("GET")
//	gMux.PathPrefix("/path").Handler(handler).Methods("GET")
func parseGorillaRoutes(f *ast.File, fset *token.FileSet) []RouteEntry {
	var routes []RouteEntry

	// Walk all function declarations for route registration statements.
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			exprStmt, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			entry := extractGorillaRoute(exprStmt.X)
			if entry != nil {
				routes = append(routes, *entry)
			}
		}
	}
	return routes
}

// extractGorillaRoute extracts a RouteEntry from a Gorilla Mux registration expression.
// Returns nil if the expression is not a recognized route registration.
func extractGorillaRoute(expr ast.Expr) *RouteEntry {
	// The expression may be a chain of method calls. We need to find:
	// 1. The .Methods("GET", "POST") call (outermost, optional)
	// 2. The .Handle/.HandleFunc/.PathPrefix call (provides path)
	// 3. The handler argument

	var methods []string
	current := expr

	// Peel off .Methods(...) if present at the top level.
	if call, ok := current.(*ast.CallExpr); ok {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Methods" {
			methods = extractStringArgs(call.Args)
			current = sel.X
		}
	}

	// Peel off .Handler(handler) if present (PathPrefix pattern).
	var handlerArg ast.Expr
	if call, ok := current.(*ast.CallExpr); ok {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Handler" {
			if len(call.Args) > 0 {
				handlerArg = call.Args[0]
			}
			current = sel.X
		}
	}

	// Now we should be at the core registration call: gMux.Handle/HandleFunc/PathPrefix
	call, ok := current.(*ast.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	// Verify the receiver is a router variable (heuristic: any identifier is accepted).
	switch sel.Sel.Name {
	case "Handle", "HandleFunc":
		if len(call.Args) < 1 {
			return nil
		}
		path := extractStringLiteral(call.Args[0])
		if path == "" {
			return nil
		}
		// Handler is the last argument (index 1 for Handle, or a func literal for HandleFunc).
		handler := ""
		if len(call.Args) >= 2 {
			if handlerArg == nil {
				handlerArg = call.Args[1]
			}
		}
		if handlerArg != nil {
			handler = extractHandlerName(handlerArg)
		} else if len(call.Args) >= 2 {
			handler = extractHandlerName(call.Args[1])
		}
		return &RouteEntry{
			Path:    path,
			Methods: methods,
			Handler: handler,
		}

	case "PathPrefix":
		if len(call.Args) < 1 {
			return nil
		}
		path := extractStringLiteral(call.Args[0])
		if path == "" {
			return nil
		}
		handler := ""
		if handlerArg != nil {
			handler = extractHandlerName(handlerArg)
		}
		return &RouteEntry{
			Path:    path,
			Methods: methods,
			Handler: handler,
		}
	}

	return nil
}

// parseEchoRoutes extracts routes from an Echo framework router file.
// It supports multi-level group nesting:
//
//	g1 := e.Group("/api")
//	g2 := g1.Group("/v1")
//	g2.GET("/users", handler)
func parseEchoRoutes(f *ast.File, fset *token.FileSet) []RouteEntry {
	var routes []RouteEntry

	// echoHTTPMethods maps Echo method names to HTTP methods.
	echoHTTPMethods := map[string]string{
		"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "DELETE",
		"PATCH": "PATCH", "OPTIONS": "OPTIONS", "HEAD": "HEAD",
		"Any": "", // special: all methods
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		// Pass 1: build group base paths.
		// Maps variable name → accumulated base path.
		groupBases := make(map[string]string)

		for _, stmt := range fn.Body.List {
			// Look for: g := expr.Group("/prefix")
			assignStmt, ok := stmt.(*ast.AssignStmt)
			if !ok || len(assignStmt.Lhs) == 0 || len(assignStmt.Rhs) == 0 {
				continue
			}
			ident, ok := assignStmt.Lhs[0].(*ast.Ident)
			if !ok {
				continue
			}
			call, ok := assignStmt.Rhs[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" {
				continue
			}
			if len(call.Args) < 1 {
				continue
			}
			prefix := extractStringLiteral(call.Args[0])
			if prefix == "" {
				continue
			}
			// Resolve parent group base path for multi-level nesting.
			parentBase := ""
			if parentIdent, ok := sel.X.(*ast.Ident); ok {
				parentBase = groupBases[parentIdent.Name]
			}
			groupBases[ident.Name] = parentBase + prefix
		}

		// Pass 2: extract route registrations.
		for _, stmt := range fn.Body.List {
			exprStmt, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := exprStmt.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			httpMethod, isEchoMethod := echoHTTPMethods[sel.Sel.Name]
			if !isEchoMethod {
				continue
			}
			if len(call.Args) < 2 {
				continue
			}
			pathSuffix := extractStringLiteral(call.Args[0])
			handler := extractHandlerName(call.Args[1])

			// Resolve receiver's group base.
			basePath := ""
			if recvIdent, ok := sel.X.(*ast.Ident); ok {
				basePath = groupBases[recvIdent.Name]
			}
			fullPath := basePath + pathSuffix

			var methods []string
			if httpMethod != "" {
				methods = []string{httpMethod}
			}
			routes = append(routes, RouteEntry{
				Path:    fullPath,
				Methods: methods,
				Handler: handler,
			})
		}
	}
	return routes
}

// extractStringLiteral extracts a string value from a basic literal expression.
func extractStringLiteral(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	return strings.Trim(lit.Value, `"`+"`")
}

// extractStringArgs extracts string values from a list of argument expressions.
func extractStringArgs(args []ast.Expr) []string {
	var result []string
	for _, arg := range args {
		if s := extractStringLiteral(arg); s != "" {
			result = append(result, s)
		}
	}
	return result
}

// extractHandlerName extracts the handler function name from a possibly
// nested middleware chain like:
//
//	h.ProviderMiddleware(h.AuthMiddleware(h.SessionInjectorMiddleware(h.ActualHandler), ...))
//
// It recursively unwraps known middleware names to find the innermost handler.
func extractHandlerName(expr ast.Expr) string {
	// Unwrap parenthesized expressions: ((handler)) → handler
	if paren, ok := expr.(*ast.ParenExpr); ok {
		return extractHandlerName(paren.X)
	}
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		// h.HandlerName (direct reference)
		return e.Sel.Name
	case *ast.CallExpr:
		// Could be middleware wrapping: h.Middleware(innerHandler, ...)
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if middlewareNames[sel.Sel.Name] {
				// Unwrap: the first argument is the inner handler
				if len(e.Args) > 0 {
					return extractHandlerName(e.Args[0])
				}
			}
			// Type conversion wrappers like http.HandlerFunc(h.Foo)
			if sel.Sel.Name == "HandlerFunc" {
				if len(e.Args) > 0 {
					return extractHandlerName(e.Args[0])
				}
			}
			// Not middleware — check arguments for handler references
			for _, arg := range e.Args {
				if name := extractHandlerName(arg); name != "" && !middlewareNames[name] {
					return name
				}
			}
			// Fall back to the function name itself
			return sel.Sel.Name
		}
		// Local HandlerFunc(h.Foo) — check first argument
		if ident, ok := e.Fun.(*ast.Ident); ok {
			if ident.Name == "HandlerFunc" && len(e.Args) > 0 {
				return extractHandlerName(e.Args[0])
			}
		}
	case *ast.Ident:
		return e.Name
	}
	return ""
}
