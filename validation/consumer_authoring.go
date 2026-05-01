package validation

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	AuthoringVerdictReady                 = "ready"
	AuthoringVerdictContractMismatch      = "contract-mismatch"
	AuthoringVerdictMissingImplementation = "missing-implementation"
	AuthoringVerdictXInternalDrift        = "x-internal-drift"
	AuthoringVerdictSecurityDrift         = "security-drift"
	AuthoringVerdictInsufficientEvidence  = "insufficient-evidence"
	AuthoringVerdictAdvisoryOnly          = "advisory-only"
	AuthoringVerdictScopeMismatch         = "scope-mismatch"
	authoringSeverityInfo                 = "info"
	authoringSeverityWarning              = "warning"
	authoringSeverityError                = "error"
	authoringConsumerMeshery              = "meshery"
	authoringConsumerCloud                = "cloud"
	authoringConsumerCloudRepo            = "meshery-cloud"
)

type ConsumerAuthoringOptions struct {
	RootDir     string
	APIFiles    []string
	MesheryRepo string
	CloudRepo   string
	Hints       bool
}

type ConsumerAuthoringResult struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Inputs        AuthoringInputs     `json:"inputs"`
	Summary       AuthoringSummary    `json:"summary"`
	Endpoints     []AuthoringEndpoint `json:"endpoints"`
}

type AuthoringInputs struct {
	APIFiles      []AuthoringInputFile `json:"apiFiles"`
	ConsumerScope []string             `json:"consumerScope"`
	SchemaRoot    string               `json:"schemaRoot"`
}

type AuthoringInputFile struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	Construct  string `json:"construct"`
	Deprecated bool   `json:"deprecated"`
}

type AuthoringSummary struct {
	Endpoints             int `json:"endpoints"`
	Ready                 int `json:"ready"`
	ContractMismatch      int `json:"contractMismatch"`
	MissingImplementation int `json:"missingImplementation"`
	XInternalDrift        int `json:"xInternalDrift"`
	SecurityDrift         int `json:"securityDrift"`
	InsufficientEvidence  int `json:"insufficientEvidence"`
	AdvisoryOnly          int `json:"advisoryOnly"`
	ScopeMismatch         int `json:"scopeMismatch"`
}

type AuthoringEndpoint struct {
	SourceFile  string              `json:"sourceFile"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	OperationID string              `json:"operationId"`
	Tags        []string            `json:"tags"`
	XInternal   []string            `json:"xInternal"`
	Deprecated  bool                `json:"deprecated"`
	Public      bool                `json:"public"`
	Verdict     string              `json:"verdict"`
	PerConsumer []AuthoringConsumer `json:"perConsumer"`
	Hints       []AuthoringFinding  `json:"hints"`
}

type AuthoringConsumer struct {
	Name            string             `json:"name"`
	Status          string             `json:"status"`
	HandlerName     string             `json:"handlerName"`
	HandlerFile     string             `json:"handlerFile"`
	HandlerLine     int                `json:"handlerLine"`
	RouterFile      string             `json:"routerFile"`
	RouterLine      int                `json:"routerLine"`
	ImportsSchemas  bool               `json:"importsSchemas"`
	WritesRaw       bool               `json:"writesRaw"`
	RequestType     string             `json:"requestType,omitempty"`
	ResponseType    string             `json:"responseType,omitempty"`
	AnonymousAccess string             `json:"anonymousAccess,omitempty"`
	Findings        []AuthoringFinding `json:"findings"`
}

type AuthoringFinding struct {
	Kind     string            `json:"kind"`
	Severity string            `json:"severity"`
	Message  string            `json:"message"`
	Evidence map[string]string `json:"evidence"`
}

func RunConsumerAuthoringAudit(opts ConsumerAuthoringOptions) (*ConsumerAuthoringResult, error) {
	return runConsumerAuthoringAudit(opts, nil, nil)
}

func runConsumerAuthoringAudit(opts ConsumerAuthoringOptions, mesheryTree, cloudTree sourceTree) (*ConsumerAuthoringResult, error) {
	if opts.RootDir == "" {
		return nil, fmt.Errorf("consumer-authoring-audit: RootDir is required")
	}
	if len(opts.APIFiles) == 0 {
		return nil, fmt.Errorf("consumer-authoring-audit: at least one api file is required")
	}

	idx, inputs, err := buildEndpointIndexFromFiles(opts.RootDir, opts.APIFiles)
	if err != nil {
		return nil, fmt.Errorf("consumer-authoring-audit: build endpoint index: %w", err)
	}

	if mesheryTree == nil && opts.MesheryRepo != "" {
		mesheryTree = localTree{root: opts.MesheryRepo}
	}
	if cloudTree == nil && opts.CloudRepo != "" {
		cloudTree = localTree{root: opts.CloudRepo}
	}

	var mesheryEndpoints []consumerEndpoint
	if mesheryTree != nil {
		mesheryEndpoints, err = parseGorillaRoutes(mesheryTree)
		if err != nil {
			return nil, fmt.Errorf("consumer-authoring-audit: parse meshery routes: %w", err)
		}
		mesheryEndpoints = indexHandlers(mesheryTree, mesheryEndpoints)
	}

	var cloudEndpoints []consumerEndpoint
	if cloudTree != nil {
		cloudEndpoints, err = parseEchoRoutes(cloudTree)
		if err != nil {
			return nil, fmt.Errorf("consumer-authoring-audit: parse cloud routes: %w", err)
		}
		cloudEndpoints = indexHandlers(cloudTree, cloudEndpoints)
	}

	match := matchEndpoints(idx, mesheryEndpoints, cloudEndpoints)
	scope := authoringConsumerScope(mesheryTree != nil, cloudTree != nil)
	endpoints := buildAuthoringEndpoints(idx, match, mesheryTree != nil, cloudTree != nil, mesheryEndpoints, cloudEndpoints, opts.Hints)

	result := &ConsumerAuthoringResult{
		SchemaVersion: 1,
		Inputs: AuthoringInputs{
			APIFiles:      inputs,
			ConsumerScope: scope,
			SchemaRoot:    filepath.Clean(opts.RootDir),
		},
		Endpoints: endpoints,
	}
	result.Summary = summarizeAuthoringEndpoints(endpoints)
	normalizeAuthoringResult(result)
	return result, nil
}

func authoringConsumerScope(mesheryProvided, cloudProvided bool) []string {
	out := []string{}
	if mesheryProvided {
		out = append(out, authoringConsumerMeshery)
	}
	if cloudProvided {
		out = append(out, authoringConsumerCloud)
	}
	return out
}

func buildAuthoringEndpoints(idx *schemaIndex, match *matchResult, mesheryProvided, cloudProvided bool, mesheryEndpoints, cloudEndpoints []consumerEndpoint, hintsEnabled bool) []AuthoringEndpoint {
	if idx == nil {
		return nil
	}
	matchLen := 0
	if match != nil {
		matchLen = len(match.Matched)
	}
	matchIndex := make(map[schemaRowKey]endpointMatch, matchLen)
	if match != nil {
		for _, m := range match.Matched {
			matchIndex[schemaRowKeyOf(m.Schema)] = m
		}
	}

	out := make([]AuthoringEndpoint, 0, len(idx.Endpoints))
	for _, ep := range idx.Endpoints {
		m := matchIndex[schemaRowKeyOf(ep)]
		endpoint := AuthoringEndpoint{
			SourceFile:  ep.SourceFile,
			Method:      ep.Method,
			Path:        ep.Path,
			OperationID: ep.OperationID,
			Tags:        append([]string(nil), ep.Tags...),
			XInternal:   append([]string(nil), ep.XInternal...),
			Deprecated:  ep.Deprecated,
			Public:      ep.Public,
			PerConsumer: []AuthoringConsumer{},
			Hints:       []AuthoringFinding{},
		}

		var statuses []string
		if mesheryProvided && xInternalAllows(ep.XInternal, authoringConsumerMeshery) {
			consumers := filterConsumersByRepo(m.Consumers, authoringConsumerMeshery)
			c := buildAuthoringConsumer(authoringConsumerMeshery, authoringConsumerMeshery, ep, consumers)
			endpoint.PerConsumer = append(endpoint.PerConsumer, c)
			statuses = append(statuses, c.Status)
		} else if mesheryProvided {
			consumers := findMatchingAuthoringConsumers(ep, mesheryEndpoints, authoringConsumerMeshery)
			if len(consumers) > 0 {
				c := buildXInternalDriftConsumer(authoringConsumerMeshery, ep, consumers[0])
				endpoint.PerConsumer = append(endpoint.PerConsumer, c)
				statuses = append(statuses, c.Status)
			}
		}
		if cloudProvided && xInternalAllows(ep.XInternal, authoringConsumerCloud) {
			consumers := filterConsumersByRepo(m.Consumers, authoringConsumerCloudRepo)
			c := buildAuthoringConsumer(authoringConsumerCloud, authoringConsumerCloudRepo, ep, consumers)
			endpoint.PerConsumer = append(endpoint.PerConsumer, c)
			statuses = append(statuses, c.Status)
		} else if cloudProvided {
			consumers := findMatchingAuthoringConsumers(ep, cloudEndpoints, authoringConsumerCloudRepo)
			if len(consumers) > 0 {
				c := buildXInternalDriftConsumer(authoringConsumerCloud, ep, consumers[0])
				endpoint.PerConsumer = append(endpoint.PerConsumer, c)
				statuses = append(statuses, c.Status)
			}
		}

		endpoint.Verdict = collapseAuthoringVerdict(statuses)
		if hintsEnabled {
			if hasMissingAuthoringConsumer(endpoint.PerConsumer) {
				endpoint.Hints = nearbyRouteHints(ep, mesheryEndpoints, cloudEndpoints)
			}
		}
		sortAuthoringFindings(endpoint.Hints)
		out = append(out, endpoint)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceFile != out[j].SourceFile {
			return out[i].SourceFile < out[j].SourceFile
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

func buildAuthoringConsumer(name, repo string, ep schemaEndpoint, consumers []consumerEndpoint) AuthoringConsumer {
	if len(consumers) == 0 {
		return AuthoringConsumer{
			Name:     name,
			Status:   AuthoringVerdictMissingImplementation,
			Findings: []AuthoringFinding{missingImplementationFinding(name, ep)},
		}
	}

	primary := consumers[0]
	c := AuthoringConsumer{
		Name:            name,
		HandlerName:     primary.HandlerName,
		HandlerFile:     primary.HandlerFile,
		HandlerLine:     primary.HandlerLine,
		RouterFile:      primary.RouterFile,
		RouterLine:      primary.RouterLine,
		ImportsSchemas:  primary.ImportsSchemas,
		WritesRaw:       primary.WritesRawResponse,
		RequestType:     authoringTypeRef(primary.RequestType),
		ResponseType:    authoringTypeRef(primary.ResponseType),
		AnonymousAccess: formatAnonymousAccess(primary.AnonymousAccess),
		Findings:        findingsFromAuthoringAssessment(repo, ep, consumers),
	}
	c.Status = statusFromAuthoringFindings(c.Findings)
	sortAuthoringFindings(c.Findings)
	return c
}

func buildXInternalDriftConsumer(name string, ep schemaEndpoint, consumer consumerEndpoint) AuthoringConsumer {
	c := AuthoringConsumer{
		Name:            name,
		Status:          AuthoringVerdictXInternalDrift,
		HandlerName:     consumer.HandlerName,
		HandlerFile:     consumer.HandlerFile,
		HandlerLine:     consumer.HandlerLine,
		RouterFile:      consumer.RouterFile,
		RouterLine:      consumer.RouterLine,
		ImportsSchemas:  consumer.ImportsSchemas,
		WritesRaw:       consumer.WritesRawResponse,
		RequestType:     authoringTypeRef(consumer.RequestType),
		ResponseType:    authoringTypeRef(consumer.ResponseType),
		AnonymousAccess: formatAnonymousAccess(consumer.AnonymousAccess),
		Findings: []AuthoringFinding{{
			Kind:     "x-internal",
			Severity: authoringSeverityError,
			Message: fmt.Sprintf(
				"%s route exists for %s %s, but schema x-internal is [%s]",
				name,
				ep.Method,
				ep.Path,
				strings.Join(ep.XInternal, ", "),
			),
			Evidence: map[string]string{
				"handler": describeHandler(consumer),
			},
		}},
	}
	sortAuthoringFindings(c.Findings)
	return c
}

func authoringTypeRef(info *goTypeInfo) string {
	if info == nil {
		return ""
	}
	return formatGoTypeRef(info)
}

func findMatchingAuthoringConsumers(ep schemaEndpoint, consumers []consumerEndpoint, repo string) []consumerEndpoint {
	key := normalizeMatchKey(ep.Method, ep.Path)
	loose := looseMatchKey(ep.Method, ep.Path)
	anyKey := matchKey{Method: "ANY", Path: key.Path}
	var exact []consumerEndpoint
	for _, c := range consumers {
		if c.Repo != repo {
			continue
		}
		cKey := normalizeMatchKey(c.Method, c.Path)
		if cKey == key || cKey == anyKey {
			exact = append(exact, c)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	var looseMatches []consumerEndpoint
	for _, c := range consumers {
		if c.Repo != repo {
			continue
		}
		cKey := looseMatchKey(c.Method, c.Path)
		if cKey == loose || cKey == (matchKey{Method: "ANY", Path: loose.Path}) {
			looseMatches = append(looseMatches, consumerWithParamMismatchNote(c, ep.Path))
		}
	}
	return looseMatches
}

func findingsFromAuthoringAssessment(repo string, ep schemaEndpoint, consumers []consumerEndpoint) []AuthoringFinding {
	var findings []AuthoringFinding
	if len(consumers) > 1 {
		var handlers []string
		for _, c := range consumers {
			handlers = append(handlers, describeHandler(c))
		}
		findings = append(findings, newAuthoringFinding("handler-evidence", authoringSeverityInfo,
			fmt.Sprintf("%s has multiple registrations: %s", repo, strings.Join(handlers, ", "))))
	}
	for i := range consumers {
		findings = append(findings, assessAuthoringConsumer(repo, ep, &consumers[i])...)
	}
	return uniqueAuthoringFindings(findings)
}

func assessAuthoringConsumer(repo string, ep schemaEndpoint, c *consumerEndpoint) []AuthoringFinding {
	if c == nil {
		return []AuthoringFinding{newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
			fmt.Sprintf("%s route could not be inspected", repo))}
	}

	var findings []AuthoringFinding
	for _, note := range c.Notes {
		findings = append(findings, newAuthoringFinding(classifyAuthoringFindingKind(note), authoringSeverityInfo, note))
	}
	for _, qpNote := range assessQueryParams(ep.QueryParams, c.QueryParams) {
		findings = append(findings, newAuthoringFinding("query-param", authoringSeverityInfo, fmt.Sprintf("%s: %s", repo, qpNote)))
	}
	findings = append(findings, securityAuthoringFindings(repo, ep, c)...)

	if c.HandlerName == "" {
		return append(findings, newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
			fmt.Sprintf("%s handler could not be resolved from route registration", repo)))
	}
	if c.HandlerName == "(anonymous)" {
		return append(findings, newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
			fmt.Sprintf("%s handler is anonymous and could not be audited", repo)))
	}
	if c.HandlerFile == "" {
		return append(findings, newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
			fmt.Sprintf("%s handler %q could not be joined to a source file", repo, c.HandlerName)))
	}

	hints := hintsFrom(ep)
	if hints.isBodyless() {
		return append(findings, assessAuthoringBodylessConsumer(repo, c, hints)...)
	}
	if hints.isRawOrScalarResponse() {
		return append(findings, assessAuthoringRawOrScalarConsumer(repo, c, hints)...)
	}

	findings = append(findings, expectedStatusAuthoringFindings(c, hints)...)

	if !hints.RequestBodyDeclared && c.RequestType != nil {
		findings = append(findings, newAuthoringFinding("request-body", authoringSeverityError,
			fmt.Sprintf("%s handler %s decodes a request body (%s) but schema declares no requestBody",
				repo, describeHandler(*c), formatGoTypeRef(c.RequestType))))
	}
	if !hints.ResponseHasContent && c.ResponseType != nil {
		findings = append(findings, newAuthoringFinding("response-body", authoringSeverityError,
			fmt.Sprintf("%s handler %s encodes a typed response (%s) but schema declares no success response body",
				repo, describeHandler(*c), formatGoTypeRef(c.ResponseType))))
	}

	sides := []struct {
		name       string
		info       *goTypeInfo
		shape      *schemaShape
		assessment shapeAssessment
		declared   bool
	}{
		{"request", c.RequestType, ep.RequestShape, verifyShapeDetailed(ep.RequestShape, c.RequestType, true), hints.RequestBodyDeclared},
		{"response", c.ResponseType, ep.ResponseShape, verifyShapeDetailed(ep.ResponseShape, c.ResponseType, false), hints.ResponseHasContent},
	}

	var compared bool
	for _, side := range sides {
		if !side.declared {
			continue
		}
		if side.assessment.status == 0 && len(side.assessment.diffs) == 0 && side.assessment.reason == "" {
			if side.info == nil {
				findings = append(findings, newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
					fmt.Sprintf("%s handler %s has no comparable %s type evidence", repo, describeHandler(*c), side.name)))
			}
			continue
		}
		switch side.assessment.status {
		case shapeDiff:
			for _, msg := range side.assessment.drift {
				findings = append(findings, newAuthoringFinding(kindForBodySide(side.name), authoringSeverityError, fmt.Sprintf("%s %s", repo, msg)))
			}
			for _, msg := range formatFieldDiffs(repo, side.name, side.assessment.diffs) {
				findings = append(findings, newAuthoringFinding(kindForBodySide(side.name), authoringSeverityError, msg))
			}
		case shapeUnverified:
			if side.assessment.reason != "" {
				findings = append(findings, newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
					fmt.Sprintf("%s: %s", repo, side.assessment.reason)))
			}
		case shapeOK:
			compared = true
			if origin := classifyGoTypeOrigin(side.info); origin != goTypeOriginDirectSchema {
				findings = append(findings, authoringOriginFinding(side.name, side.info, origin))
			}
		}
	}

	if !compared && c.WritesRawResponse && hints.ResponseHasContent {
		findings = append(findings, newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
			fmt.Sprintf("%s handler %s writes a raw response; response shape could not be verified against schema",
				repo, describeHandler(*c))))
	}
	if !c.ImportsSchemas {
		findings = append(findings, newAuthoringFinding("handler-evidence", authoringSeverityInfo,
			fmt.Sprintf("%s handler %s does not import github.com/meshery/schemas/models; authoring mode relies on shape evidence instead",
				repo, describeHandler(*c))))
	}
	return findings
}

func assessAuthoringBodylessConsumer(repo string, c *consumerEndpoint, hints endpointContractHints) []AuthoringFinding {
	var findings []AuthoringFinding
	findings = append(findings, expectedStatusAuthoringFindings(c, hints)...)
	if c.RequestType != nil {
		findings = append(findings, newAuthoringFinding("request-body", authoringSeverityError,
			fmt.Sprintf("%s handler %s decodes a request body (%s) but schema declares no requestBody",
				repo, describeHandler(*c), formatGoTypeRef(c.RequestType))))
	}
	if c.ResponseType != nil {
		findings = append(findings, newAuthoringFinding("response-body", authoringSeverityError,
			fmt.Sprintf("%s handler %s encodes a typed response (%s) but schema declares no success response body",
				repo, describeHandler(*c), formatGoTypeRef(c.ResponseType))))
	}
	if c.WritesRawResponse {
		findings = append(findings, newAuthoringFinding("response-body", authoringSeverityError,
			fmt.Sprintf("%s handler %s writes a raw response body but schema declares no success response body",
				repo, describeHandler(*c))))
	}
	return findings
}

func assessAuthoringRawOrScalarConsumer(repo string, c *consumerEndpoint, hints endpointContractHints) []AuthoringFinding {
	var findings []AuthoringFinding
	findings = append(findings, expectedStatusAuthoringFindings(c, hints)...)
	if c.RequestType != nil {
		findings = append(findings, newAuthoringFinding("request-body", authoringSeverityError,
			fmt.Sprintf("%s handler %s decodes a request body (%s) but schema declares no requestBody",
				repo, describeHandler(*c), formatGoTypeRef(c.RequestType))))
	}
	if c.ResponseType != nil {
		findings = append(findings, newAuthoringFinding("response-body", authoringSeverityError,
			fmt.Sprintf("%s handler %s encodes a typed response (%s) but schema declares a raw/scalar success response",
				repo, describeHandler(*c), formatGoTypeRef(c.ResponseType))))
	}
	if c.WritesRawResponse {
		findings = append(findings, newAuthoringFinding("handler-evidence", authoringSeverityInfo,
			fmt.Sprintf("%s handler %s writes a raw response; authoring mode treats this as implementation evidence only",
				repo, describeHandler(*c))))
	} else if c.ResponseType == nil {
		findings = append(findings, newAuthoringFinding("insufficient-evidence", authoringSeverityWarning,
			fmt.Sprintf("%s handler %s did not expose a recognized raw-response write pattern",
				repo, describeHandler(*c))))
	}
	return findings
}

func securityAuthoringFindings(repo string, ep schemaEndpoint, c *consumerEndpoint) []AuthoringFinding {
	if c == nil {
		return nil
	}
	if c.AnonymousAccess == nil {
		return []AuthoringFinding{newAuthoringFinding("security", authoringSeverityWarning,
			fmt.Sprintf("%s route access could not be determined; verify public/auth annotation manually", repo))}
	}
	if ep.Public && !*c.AnonymousAccess {
		return []AuthoringFinding{newAuthoringFinding("security", authoringSeverityError,
			fmt.Sprintf("%s schema marks endpoint public (security: []) but route does not allow anonymous access", repo))}
	}
	if !ep.Public && *c.AnonymousAccess {
		return []AuthoringFinding{newAuthoringFinding("security", authoringSeverityError,
			fmt.Sprintf("%s schema requires auth but route allows anonymous access", repo))}
	}
	return nil
}

func expectedStatusAuthoringFindings(c *consumerEndpoint, hints endpointContractHints) []AuthoringFinding {
	var findings []AuthoringFinding
	for _, msg := range expectedSuccessStatusDrift(c, hints) {
		findings = append(findings, newAuthoringFinding("status-code", authoringSeverityError, msg))
	}
	return findings
}

func authoringOriginFinding(side string, info *goTypeInfo, origin goTypeOrigin) AuthoringFinding {
	switch origin {
	case goTypeOriginSchemaAlias:
		return newAuthoringFinding("handler-evidence", authoringSeverityInfo,
			fmt.Sprintf("%s uses local alias %q with matching shape", side, formatGoTypeRef(info)))
	case goTypeOriginLocalStruct:
		return newAuthoringFinding("handler-evidence", authoringSeverityInfo,
			fmt.Sprintf("%s uses local struct %q with matching shape", side, formatGoTypeRef(info)))
	default:
		return newAuthoringFinding("handler-evidence", authoringSeverityInfo,
			fmt.Sprintf("%s uses non-direct schema type %q with matching shape", side, formatGoTypeRef(info)))
	}
}

func kindForBodySide(side string) string {
	if side == "request" {
		return "request-body"
	}
	return "response-body"
}

func newAuthoringFinding(kind, severity, message string) AuthoringFinding {
	return AuthoringFinding{
		Kind:     kind,
		Severity: severity,
		Message:  message,
		Evidence: map[string]string{},
	}
}

func statusFromAuthoringFindings(findings []AuthoringFinding) string {
	if len(findings) == 0 {
		return AuthoringVerdictReady
	}
	for _, f := range findings {
		if f.Kind == "x-internal" && f.Severity == authoringSeverityError {
			return AuthoringVerdictXInternalDrift
		}
	}
	for _, f := range findings {
		if f.Kind == "security" && f.Severity == authoringSeverityError {
			return AuthoringVerdictSecurityDrift
		}
	}
	for _, f := range findings {
		if f.Severity == authoringSeverityError {
			return AuthoringVerdictContractMismatch
		}
	}
	for _, f := range findings {
		if f.Kind == "insufficient-evidence" || f.Severity == authoringSeverityWarning {
			return AuthoringVerdictInsufficientEvidence
		}
	}
	return AuthoringVerdictAdvisoryOnly
}

func formatAnonymousAccess(value *bool) string {
	if value == nil {
		return "unknown"
	}
	if *value {
		return "public"
	}
	return "auth"
}

func collapseAuthoringVerdict(statuses []string) string {
	if len(statuses) == 0 {
		return AuthoringVerdictScopeMismatch
	}
	priority := []string{
		AuthoringVerdictXInternalDrift,
		AuthoringVerdictSecurityDrift,
		AuthoringVerdictContractMismatch,
		AuthoringVerdictMissingImplementation,
		AuthoringVerdictInsufficientEvidence,
		AuthoringVerdictAdvisoryOnly,
	}
	for _, want := range priority {
		for _, status := range statuses {
			if status == want {
				return want
			}
		}
	}
	return AuthoringVerdictReady
}

func classifyAuthoringFindingKind(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "query param"):
		return "query-param"
	case strings.Contains(lower, "expected") && strings.Contains(lower, "success status"):
		return "status-code"
	case strings.Contains(lower, "status"):
		return "status-code"
	case strings.Contains(lower, "request"):
		return "request-body"
	case strings.Contains(lower, "response"):
		return "response-body"
	case strings.Contains(lower, "path parameter"):
		return "path-method"
	default:
		return "handler-evidence"
	}
}

func uniqueAuthoringFindings(findings []AuthoringFinding) []AuthoringFinding {
	if len(findings) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(findings))
	out := make([]AuthoringFinding, 0, len(findings))
	for _, finding := range findings {
		key := finding.Kind + "\x00" + finding.Severity + "\x00" + finding.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, finding)
	}
	return out
}

func missingImplementationFinding(name string, ep schemaEndpoint) AuthoringFinding {
	router := "route"
	if name == authoringConsumerMeshery {
		router = "Gorilla route"
	} else if name == authoringConsumerCloud {
		router = "Echo route"
	}
	return AuthoringFinding{
		Kind:     "missing-implementation",
		Severity: authoringSeverityError,
		Message:  fmt.Sprintf("No %s registered for %s %s", router, ep.Method, ep.Path),
		Evidence: map[string]string{},
	}
}

func hasMissingAuthoringConsumer(consumers []AuthoringConsumer) bool {
	for _, c := range consumers {
		if c.Status == AuthoringVerdictMissingImplementation {
			return true
		}
	}
	return false
}

func nearbyRouteHints(ep schemaEndpoint, mesheryEndpoints, cloudEndpoints []consumerEndpoint) []AuthoringFinding {
	var hints []AuthoringFinding
	add := func(consumerName string, candidates []consumerEndpoint) {
		for _, c := range candidates {
			if c.Method != ep.Method && c.Method != "ANY" {
				continue
			}
			if !nearbyPath(ep.Path, c.Path) {
				continue
			}
			hints = append(hints, AuthoringFinding{
				Kind:     "path-method",
				Severity: authoringSeverityInfo,
				Message:  fmt.Sprintf("Nearby: %s %s", c.Method, c.Path),
				Evidence: map[string]string{
					"consumer":      consumerName,
					"candidatePath": c.Path,
					"handler":       c.HandlerName,
				},
			})
			if len(hints) >= 3 {
				return
			}
		}
	}
	add(authoringConsumerMeshery, mesheryEndpoints)
	if len(hints) < 3 {
		add(authoringConsumerCloud, cloudEndpoints)
	}
	sortAuthoringFindings(hints)
	if len(hints) > 3 {
		hints = hints[:3]
	}
	return hints
}

func nearbyPath(a, b string) bool {
	aTokens := pathTokens(a)
	bTokens := pathTokens(b)
	if absInt(len(aTokens)-len(bTokens)) > 1 {
		return false
	}
	mismatches := 0
	max := len(aTokens)
	if len(bTokens) > max {
		max = len(bTokens)
	}
	for i := 0; i < max; i++ {
		if i >= len(aTokens) || i >= len(bTokens) {
			mismatches++
			continue
		}
		if pathTokenEquivalent(aTokens[i], bTokens[i]) {
			continue
		}
		mismatches++
	}
	return mismatches <= 1
}

func pathTokens(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func pathTokenEquivalent(a, b string) bool {
	if a == b {
		return true
	}
	if strings.HasPrefix(a, "{") && strings.HasPrefix(b, "{") {
		return true
	}
	return strings.TrimSuffix(a, "s") == strings.TrimSuffix(b, "s")
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func summarizeAuthoringEndpoints(endpoints []AuthoringEndpoint) AuthoringSummary {
	s := AuthoringSummary{Endpoints: len(endpoints)}
	for _, ep := range endpoints {
		switch ep.Verdict {
		case AuthoringVerdictReady:
			s.Ready++
		case AuthoringVerdictContractMismatch:
			s.ContractMismatch++
		case AuthoringVerdictMissingImplementation:
			s.MissingImplementation++
		case AuthoringVerdictXInternalDrift:
			s.XInternalDrift++
		case AuthoringVerdictSecurityDrift:
			s.SecurityDrift++
		case AuthoringVerdictInsufficientEvidence:
			s.InsufficientEvidence++
		case AuthoringVerdictAdvisoryOnly:
			s.AdvisoryOnly++
		case AuthoringVerdictScopeMismatch:
			s.ScopeMismatch++
		}
	}
	return s
}

func sortAuthoringFindings(findings []AuthoringFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Message < findings[j].Message
	})
}

func normalizeAuthoringResult(result *ConsumerAuthoringResult) {
	if result == nil {
		return
	}
	if result.Inputs.ConsumerScope == nil {
		result.Inputs.ConsumerScope = []string{}
	}
	if result.Inputs.APIFiles == nil {
		result.Inputs.APIFiles = []AuthoringInputFile{}
	}
	if result.Endpoints == nil {
		result.Endpoints = []AuthoringEndpoint{}
	}
	for i := range result.Endpoints {
		if result.Endpoints[i].Tags == nil {
			result.Endpoints[i].Tags = []string{}
		}
		if result.Endpoints[i].XInternal == nil {
			result.Endpoints[i].XInternal = []string{}
		}
		if result.Endpoints[i].PerConsumer == nil {
			result.Endpoints[i].PerConsumer = []AuthoringConsumer{}
		}
		if result.Endpoints[i].Hints == nil {
			result.Endpoints[i].Hints = []AuthoringFinding{}
		}
		for j := range result.Endpoints[i].PerConsumer {
			if result.Endpoints[i].PerConsumer[j].Findings == nil {
				result.Endpoints[i].PerConsumer[j].Findings = []AuthoringFinding{}
			}
		}
	}
}

func ConsumerAuthoringHasFailure(result *ConsumerAuthoringResult, failOn []string) bool {
	if result == nil || len(failOn) == 0 {
		return false
	}
	want := make(map[string]bool, len(failOn))
	for _, item := range failOn {
		item = strings.TrimSpace(item)
		if item != "" {
			want[item] = true
		}
	}
	for _, ep := range result.Endpoints {
		if want[ep.Verdict] {
			return true
		}
	}
	return false
}
