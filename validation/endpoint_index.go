package validation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// schemaEndpoint represents one endpoint defined in a construct's api.yml.
// One entry per (method, path) — never collapsed across verbs.
type schemaEndpoint struct {
	Method            string             // "GET", "POST", etc. — one per entry
	Path              string             // "/api/integrations/connections/{connectionId}"
	OperationID       string             // "getConnection"
	Tags              []string           // operation tags -> derive Category / Sub-Category
	XInternal         []string           // ["meshery"], ["cloud"], or nil (= both repos)
	RequestShape      *schemaShape       // nil for GET/DELETE without body
	ResponseShape     *schemaShape       // from primary 2xx response
	SuccessStatusCode int                // primary 2xx response code (200/201/202/204)
	Deprecated        bool               // operation-level OR construct-level
	Public            bool               // true if explicitly security: []
	HasSuccessRef     bool               // true if a 2xx response has a $ref schema
	Has2xx            bool               // true if there is a 2xx response at all
	RequestBody       bool               // true if the operation declares a requestBody
	QueryParams       []schemaQueryParam // query parameters declared on the operation
	Construct         string             // "connection" — from extractConstructName
	Version           string             // "v1beta1"
	SourceFile        string             // "schemas/constructs/v1beta1/connection/api.yml"
}

// schemaQueryParam describes one query parameter declared in an OpenAPI spec.
type schemaQueryParam struct {
	Name     string
	Required bool
}

// schemaShape is a structural summary of a schema component.
// Fields are fully resolved ($refs followed) at index-build time.
type schemaShape struct {
	Name         string                // "ConnectionPayload"
	Fields       map[string]fieldShape // keyed by JSON wire name
	GoType       string                // from x-go-type annotation, if present
	TopLevelType string                // "object", "array", "string", etc.
	// AllowsAdditionalProperties is true when the schema explicitly permits
	// undeclared object keys via additionalProperties: true or a typed
	// additionalProperties schema. The matcher uses this to avoid false-positive
	// "extra field" drift against intentionally open-ended objects.
	AllowsAdditionalProperties bool
}

// fieldShape describes one field in a schema.
type fieldShape struct {
	Name      string // JSON wire name
	Type      string // "string", "integer", "object", "array", etc.
	Format    string // "uuid", "date-time", etc.
	Required  bool
	RefTarget string // if originally a $ref, what it resolved to
}

// schemaIndex holds all endpoints from a schemas repo walk.
type schemaIndex struct {
	Endpoints []schemaEndpoint // sorted by (Path, Method)
}

// buildEndpointIndex walks the meshery/schemas constructs tree and produces
// a deterministic index of every API endpoint defined across api.yml files.
// It delegates to walkValidatedConstructSpecs, which is the single canonical
// walker shared with the schema validator.
func buildEndpointIndex(rootDir string) (*schemaIndex, error) {
	index := &schemaIndex{}
	if err := walkValidatedConstructSpecs(rootDir, func(spec constructSpec) error {
		return indexEndpointsFromSpec(spec, index)
	}); err != nil {
		return nil, err
	}

	sortSchemaEndpoints(index.Endpoints)

	return index, nil
}

// indexEndpointsFromSpec appends every operation in one loaded api.yml to idx.
// It intentionally does not skip deprecated specs; whole-tree callers perform
// that filtering before this seam, while authoring mode must inspect the exact
// files the author supplied.
func indexEndpointsFromSpec(spec constructSpec, idx *schemaIndex) error {
	if idx == nil || !spec.APIExists {
		return nil
	}
	if spec.LoadErr != nil {
		return nil // schema validation reports load failures.
	}
	doc := spec.Doc
	if doc == nil || doc.Paths == nil {
		return nil
	}
	constructDeprecated := isDeprecatedDoc(doc)
	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		for _, method := range httpMethods {
			op := getOperation(pathItem, method)
			if op == nil {
				continue
			}

			xInternal, err := parseXInternal(op.Extensions)
			if err != nil {
				return err
			}

			ep := schemaEndpoint{
				Method:      strings.ToUpper(method),
				Path:        path,
				OperationID: op.OperationID,
				Tags:        append([]string(nil), op.Tags...),
				XInternal:   xInternal,
				Deprecated:  constructDeprecated || op.Deprecated,
				Public:      isExplicitlyPublic(op, doc),
				Construct:   spec.Construct,
				Version:     spec.Version,
				SourceFile:  spec.RelativePath,
			}

			if op.RequestBody != nil && op.RequestBody.Value != nil {
				ep.RequestBody = true
				for _, media := range op.RequestBody.Value.Content {
					if media != nil && media.Schema != nil {
						ep.RequestShape = buildSchemaShape(media.Schema)
						break
					}
				}
			}

			ep.ResponseShape, ep.HasSuccessRef, ep.Has2xx, ep.SuccessStatusCode = pickResponseShape(op)
			ep.QueryParams = mergeQueryParams(pathItem.Parameters, op.Parameters)

			idx.Endpoints = append(idx.Endpoints, ep)
		}
	}
	return nil
}

func sortSchemaEndpoints(endpoints []schemaEndpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Path != endpoints[j].Path {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})
}

func sortSchemaEndpointsBySource(endpoints []schemaEndpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].SourceFile != endpoints[j].SourceFile {
			return endpoints[i].SourceFile < endpoints[j].SourceFile
		}
		if endpoints[i].Path != endpoints[j].Path {
			return endpoints[i].Path < endpoints[j].Path
		}
		return endpoints[i].Method < endpoints[j].Method
	})
}

func buildEndpointIndexFromFiles(rootDir string, files []string) (*schemaIndex, []AuthoringInputFile, error) {
	index := &schemaIndex{}
	inputs := make([]AuthoringInputFile, 0, len(files))
	for _, file := range files {
		spec, input, err := loadConstructSpec(rootDir, file)
		if err != nil {
			return nil, nil, err
		}
		if err := indexEndpointsFromSpec(spec, index); err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, input)
	}
	sortSchemaEndpointsBySource(index.Endpoints)
	return index, inputs, nil
}

func loadConstructSpec(rootDir, path string) (constructSpec, AuthoringInputFile, error) {
	if strings.TrimSpace(path) == "" {
		return constructSpec{}, AuthoringInputFile{}, fmt.Errorf("empty api file path")
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(rootDir, path)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return constructSpec{}, AuthoringInputFile{}, fmt.Errorf("%s: %w", path, err)
	}
	if info.IsDir() {
		absPath = filepath.Join(absPath, "api.yml")
		info, err = os.Stat(absPath)
		if err != nil {
			return constructSpec{}, AuthoringInputFile{}, fmt.Errorf("%s: %w", absPath, err)
		}
	}
	if info.IsDir() || filepath.Base(absPath) != "api.yml" {
		return constructSpec{}, AuthoringInputFile{}, fmt.Errorf("%s is not an api.yml file or construct directory", path)
	}

	relPath := relativeToRoot(absPath, rootDir)
	version, construct := versionConstructFromAPIPath(relPath)
	constructDir := filepath.Dir(absPath)
	doc, loadErr := loadAPISpec(absPath)
	deprecated := isDeprecatedDoc(doc)

	spec := constructSpec{
		Version:      version,
		Construct:    construct,
		ConstructDir: constructDir,
		APIYMLPath:   absPath,
		RelativePath: relPath,
		APIExists:    true,
		Doc:          doc,
		LoadErr:      loadErr,
	}
	input := AuthoringInputFile{
		Path:       relPath,
		Version:    version,
		Construct:  construct,
		Deprecated: deprecated,
	}
	return spec, input, loadErr
}

func versionConstructFromAPIPath(relPath string) (string, string) {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for i, part := range parts {
		if part == "constructs" && i+2 < len(parts) {
			return parts[i+1], parts[i+2]
		}
	}
	if len(parts) >= 3 {
		return parts[len(parts)-3], parts[len(parts)-2]
	}
	return "", strings.TrimSuffix(filepath.Base(filepath.Dir(relPath)), string(filepath.Separator))
}

// mergeQueryParams collects query parameters from path-level and
// operation-level parameter lists. Operation-level parameters override
// path-level parameters with the same name, per the OpenAPI 3 spec.
func mergeQueryParams(pathLevel, opLevel openapi3.Parameters) []schemaQueryParam {
	// Use a map to deduplicate by name, with op-level winning.
	byName := make(map[string]schemaQueryParam)
	for _, ref := range pathLevel {
		if ref == nil || ref.Value == nil || ref.Value.In != openapi3.ParameterInQuery {
			continue
		}
		p := ref.Value
		byName[p.Name] = schemaQueryParam{Name: p.Name, Required: p.Required}
	}
	for _, ref := range opLevel {
		if ref == nil || ref.Value == nil || ref.Value.In != openapi3.ParameterInQuery {
			continue
		}
		p := ref.Value
		byName[p.Name] = schemaQueryParam{Name: p.Name, Required: p.Required}
	}
	if len(byName) == 0 {
		return nil
	}
	out := make([]schemaQueryParam, 0, len(byName))
	for _, qp := range byName {
		out = append(out, qp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// parseXInternal extracts the explicit x-internal target list from operation
// extensions.
func parseXInternal(extensions map[string]any) ([]string, error) {
	if extensions == nil {
		return nil, nil
	}
	raw, ok := extensions["x-internal"]
	if !ok {
		return nil, nil
	}
	return parseXInternalTargets(raw)
}

func parseXInternalTargets(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []any:
		out := make([]string, 0, len(v))
		seen := make(map[string]bool, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("x-internal array values must be strings")
			}
			if !validInternalTags[s] {
				return nil, fmt.Errorf(`x-internal value %q is invalid`, s)
			}
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(v))
		seen := make(map[string]bool, len(v))
		for _, item := range v {
			if !validInternalTags[item] {
				return nil, fmt.Errorf(`x-internal value %q is invalid`, item)
			}
			if seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	default:
		return nil, fmt.Errorf("x-internal must be an array")
	}
}

// pickResponseShape selects the primary 2xx response and converts it to a
// schemaShape. Returns the shape (nil if none), whether the 2xx had a $ref
// schema, whether any 2xx response existed, and the chosen 2xx status code.
func pickResponseShape(op *openapi3.Operation) (*schemaShape, bool, bool, int) {
	codes := collectResponseCodes(op)
	has2xx := false
	for code := range codes {
		if len(code) > 0 && code[0] == '2' {
			has2xx = true
			break
		}
	}
	if op.Responses == nil {
		return nil, false, has2xx, 0
	}

	// Prefer 200, then 201, then 202, 204, then any 2xx in sorted order.
	preferred := []string{"200", "201", "202", "204"}
	var resp *openapi3.ResponseRef
	chosenCode := ""
	for _, code := range preferred {
		if r := op.Responses.Value(code); r != nil {
			resp = r
			chosenCode = code
			break
		}
	}
	if resp == nil {
		var twoXX []string
		for code := range op.Responses.Map() {
			if len(code) > 0 && code[0] == '2' {
				twoXX = append(twoXX, code)
			}
		}
		sort.Strings(twoXX)
		if len(twoXX) > 0 {
			chosenCode = twoXX[0]
			resp = op.Responses.Value(twoXX[0])
		}
	}
	if resp == nil || resp.Value == nil {
		return nil, false, has2xx, parseStatusCode(chosenCode)
	}

	for _, media := range resp.Value.Content {
		if media == nil || media.Schema == nil {
			continue
		}
		shape := buildSchemaShape(media.Schema)
		hasRef := media.Schema.Ref != ""
		if !hasRef && media.Schema.Value != nil && media.Schema.Value.Items != nil &&
			media.Schema.Value.Items.Ref != "" {
			hasRef = true
		}
		return shape, hasRef, has2xx, parseStatusCode(chosenCode)
	}
	return nil, false, has2xx, parseStatusCode(chosenCode)
}

func parseStatusCode(code string) int {
	n, err := strconv.Atoi(code)
	if err != nil {
		return 0
	}
	return n
}

// buildSchemaShape resolves a schema reference into a structural fingerprint.
// $ref targets are followed; arrays are described by their item shape's name.
func buildSchemaShape(ref *openapi3.SchemaRef) *schemaShape {
	if ref == nil {
		return nil
	}
	shape := &schemaShape{Fields: map[string]fieldShape{}}

	if ref.Ref != "" {
		shape.Name = refTail(ref.Ref)
	}

	schema := ref.Value
	if schema == nil {
		return shape
	}

	if shape.Name == "" {
		if schema.Title != "" {
			shape.Name = schema.Title
		}
	}
	if schema.Type != nil && len(schema.Type.Slice()) > 0 {
		shape.TopLevelType = schema.Type.Slice()[0]
	}
	if raw, ok := schema.Extensions["x-go-type"]; ok {
		if s, ok := raw.(string); ok {
			shape.GoType = s
		}
	}

	// Array schema: describe via items.
	if schema.Type != nil && schema.Type.Is("array") {
		if schema.Items != nil {
			inner := buildSchemaShape(schema.Items)
			if inner != nil {
				shape.Fields = inner.Fields
				shape.AllowsAdditionalProperties = inner.AllowsAdditionalProperties
				if shape.Name == "" {
					shape.Name = "[]" + inner.Name
				}
			}
		}
		return shape
	}

	if schema.AdditionalProperties.Has != nil && *schema.AdditionalProperties.Has {
		shape.AllowsAdditionalProperties = true
	}
	if schema.AdditionalProperties.Schema != nil {
		shape.AllowsAdditionalProperties = true
	}

	requiredSet := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		requiredSet[name] = true
	}

	for name, propRef := range schema.Properties {
		fs := fieldShape{
			Name:     name,
			Required: requiredSet[name],
		}
		if propRef != nil && propRef.Ref != "" {
			fs.RefTarget = refTail(propRef.Ref)
		}
		if propRef != nil && propRef.Value != nil {
			pv := propRef.Value
			if pv.Type != nil && len(pv.Type.Slice()) > 0 {
				fs.Type = pv.Type.Slice()[0]
			}
			fs.Format = pv.Format
			if fs.Type == "array" && pv.Items != nil {
				if pv.Items.Ref != "" {
					fs.RefTarget = refTail(pv.Items.Ref)
				} else if pv.Items.Value != nil && pv.Items.Value.Type != nil && len(pv.Items.Value.Type.Slice()) > 0 {
					fs.RefTarget = pv.Items.Value.Type.Slice()[0]
				}
			}
		}
		shape.Fields[name] = fs
	}

	return shape
}
