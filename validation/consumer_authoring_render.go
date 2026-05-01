package validation

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/rodaine/table"
)

func RenderConsumerAuthoringJSON(out io.Writer, result *ConsumerAuthoringResult) error {
	normalizeAuthoringResult(result)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func RenderConsumerAuthoringMarkdown(out io.Writer, result *ConsumerAuthoringResult) error {
	if result == nil {
		return nil
	}
	fmt.Fprintln(out, "# Consumer-Informed Authoring Check")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "**Schema root:** `%s`\n", result.Inputs.SchemaRoot)
	scope := strings.Join(result.Inputs.ConsumerScope, ", ")
	if scope == "" {
		scope = "(none)"
	}
	fmt.Fprintf(out, "**Consumer scope:** %s\n", scope)
	fmt.Fprintf(out, "**Inputs:** %d file(s)\n\n", len(result.Inputs.APIFiles))

	fmt.Fprintln(out, "| File | Version | Construct |")
	fmt.Fprintln(out, "|---|---|---|")
	for _, input := range result.Inputs.APIFiles {
		fmt.Fprintf(out, "| `%s` | %s | %s |\n", input.Path, input.Version, input.Construct)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Summary")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "| Verdict | Count |")
	fmt.Fprintln(out, "|---|---|")
	fmt.Fprintf(out, "| ready | %d |\n", result.Summary.Ready)
	fmt.Fprintf(out, "| contract-mismatch | %d |\n", result.Summary.ContractMismatch)
	fmt.Fprintf(out, "| missing-implementation | %d |\n", result.Summary.MissingImplementation)
	fmt.Fprintf(out, "| x-internal-drift | %d |\n", result.Summary.XInternalDrift)
	fmt.Fprintf(out, "| security-drift | %d |\n", result.Summary.SecurityDrift)
	fmt.Fprintf(out, "| insufficient-evidence | %d |\n", result.Summary.InsufficientEvidence)
	fmt.Fprintf(out, "| advisory-only | %d |\n", result.Summary.AdvisoryOnly)
	fmt.Fprintf(out, "| scope-mismatch | %d |\n", result.Summary.ScopeMismatch)

	byFile := endpointsBySourceFile(result.Endpoints)
	for _, input := range result.Inputs.APIFiles {
		endpoints := byFile[input.Path]
		if len(endpoints) == 0 {
			continue
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "## `%s`\n", input.Path)
		for _, ep := range endpoints {
			fmt.Fprintln(out)
			fmt.Fprintf(out, "### `%s %s` - **%s**\n\n", ep.Method, ep.Path, ep.Verdict)
			fmt.Fprintf(out, "`operationId: %s`", ep.OperationID)
			if len(ep.Tags) > 0 {
				fmt.Fprintf(out, " ; tags: `%s`", strings.Join(ep.Tags, "`, `"))
			}
			if len(ep.XInternal) > 0 {
				fmt.Fprintf(out, " ; x-internal: `%s`", strings.Join(ep.XInternal, "`, `"))
			}
			fmt.Fprintln(out)
			for _, consumer := range ep.PerConsumer {
				fmt.Fprintln(out)
				fmt.Fprintf(out, "#### %s\n\n", consumer.Name)
				if consumer.HandlerName != "" {
					fmt.Fprintf(out, "- Handler: `%s::%s`", consumer.HandlerFile, consumer.HandlerName)
					if consumer.RouterFile != "" {
						fmt.Fprintf(out, " (`%s:%d`)", consumer.RouterFile, consumer.RouterLine)
					}
					fmt.Fprintln(out)
				} else {
					fmt.Fprintf(out, "- **%s**\n", consumer.Status)
				}
				if len(consumer.Findings) == 0 {
					fmt.Fprintln(out, "\n(no findings)")
					continue
				}
				fmt.Fprintln(out)
				fmt.Fprintln(out, "| Kind | Severity | Message |")
				fmt.Fprintln(out, "|---|---|---|")
				for _, finding := range consumer.Findings {
					fmt.Fprintf(out, "| %s | %s | %s |\n", finding.Kind, finding.Severity, markdownEscape(finding.Message))
				}
			}
			if len(ep.Hints) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "#### Hints")
				for _, hint := range ep.Hints {
					fmt.Fprintf(out, "- %s\n", hint.Message)
				}
			}
		}
	}
	if resultHasRawEvidence(result) {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "---")
		fmt.Fprintln(out, "*Some handlers write raw response bytes; the audit cannot verify those payloads against the declared schema. Confirm manually or migrate to a typed encoder.*")
	}
	return nil
}

// RenderConsumerAuthoringCLI prints the endpoint-centric authoring audit view.
// The output is intentionally complete for each supplied api.yml so authors can
// review alignment and drift without opening the JSON payload.
func RenderConsumerAuthoringCLI(out io.Writer, result *ConsumerAuthoringResult) error {
	if result == nil {
		return nil
	}
	byFile := endpointsBySourceFile(result.Endpoints)
	for inputIndex, input := range result.Inputs.APIFiles {
		endpoints := byFile[input.Path]
		fmt.Fprintf(out, "API Audit: %s\n\n", input.Path)
		if input.LoadError != "" {
			fmt.Fprintf(out, "Error: %s\n\n", input.LoadError)
		}

		fmt.Fprintln(out, "Summary:")
		fmt.Fprintln(out)
		renderAuthoringSummaryTable(out, summarizeAuthoringFile(endpoints))
		fmt.Fprintln(out)

		for i, ep := range endpoints {
			if i > 0 {
				fmt.Fprintln(out)
			}
			renderAuthoringEndpointDivider(out)
			renderAuthoringEndpoint(out, ep)
		}

		if len(endpoints) == 0 {
			fmt.Fprintln(out, "(no endpoints found)")
		}
		if input.Deprecated {
			fmt.Fprintln(out)
			fmt.Fprintln(out, "Deprecated: true")
		}
		if inputIndex < len(result.Inputs.APIFiles)-1 {
			fmt.Fprintln(out)
		}
	}
	return nil
}

type authoringSummaryLine struct {
	Total   int
	Meshery int
	Cloud   int
}

type authoringFileSummary struct {
	Endpoints             authoringSummaryLine
	MissingImplementation authoringSummaryLine
	XAnnotationDrift      authoringSummaryLine
	AuthDrift             authoringSummaryLine
	RequestBodyDrift      authoringSummaryLine
	ResponseBodyDrift     authoringSummaryLine
}

func summarizeAuthoringFile(endpoints []AuthoringEndpoint) authoringFileSummary {
	var s authoringFileSummary
	s.Endpoints.Total = len(endpoints)
	for _, ep := range endpoints {
		if xInternalAllows(ep.XInternal, authoringConsumerMeshery) {
			s.Endpoints.Meshery++
		}
		if xInternalAllows(ep.XInternal, authoringConsumerCloud) {
			s.Endpoints.Cloud++
		}

		var hasMissing, hasX, hasAuth, hasReq, hasResp bool
		for _, c := range ep.PerConsumer {
			if c.Status == AuthoringVerdictMissingImplementation {
				addConsumerSummary(&s.MissingImplementation, c.Name)
				hasMissing = true
			}
			if consumerHasFinding(c, "x-internal", authoringSeverityError) {
				addConsumerSummary(&s.XAnnotationDrift, c.Name)
				hasX = true
			}
			if consumerHasFinding(c, "security", authoringSeverityError) {
				addConsumerSummary(&s.AuthDrift, c.Name)
				hasAuth = true
			}
			if consumerHasFinding(c, "request-body", authoringSeverityError) {
				addConsumerSummary(&s.RequestBodyDrift, c.Name)
				hasReq = true
			}
			if consumerHasFinding(c, "response-body", authoringSeverityError) {
				addConsumerSummary(&s.ResponseBodyDrift, c.Name)
				hasResp = true
			}
		}
		if hasMissing {
			s.MissingImplementation.Total++
		}
		if hasX {
			s.XAnnotationDrift.Total++
		}
		if hasAuth {
			s.AuthDrift.Total++
		}
		if hasReq {
			s.RequestBodyDrift.Total++
		}
		if hasResp {
			s.ResponseBodyDrift.Total++
		}
	}
	return s
}

func addConsumerSummary(line *authoringSummaryLine, consumer string) {
	switch consumer {
	case authoringConsumerMeshery:
		line.Meshery++
	case authoringConsumerCloud:
		line.Cloud++
	}
}

func renderAuthoringSummaryTable(out io.Writer, s authoringFileSummary) {
	rows := []struct {
		Category string
		Values   authoringSummaryLine
	}{
		{"Endpoints", s.Endpoints},
		{"Missing Implementation", s.MissingImplementation},
		{"x-annotation Drift", s.XAnnotationDrift},
		{"Auth Drift", s.AuthDrift},
		{"Request Body Drift", s.RequestBodyDrift},
		{"Response Body Drift", s.ResponseBodyDrift},
	}
	t := table.New("Category", "Total", "Meshery", "Cloud")
	t.WithWriter(out)
	for _, row := range rows {
		t.AddRow(row.Category, row.Values.Total, row.Values.Meshery, row.Values.Cloud)
	}
	t.Print()
}

func renderAuthoringEndpointDivider(out io.Writer) {
	fmt.Fprintln(out, "------------------------------------------------------------------------------")
}

func renderAuthoringEndpoint(out io.Writer, ep AuthoringEndpoint) {
	fmt.Fprintf(out, "Method: %s\n", ep.Method)
	fmt.Fprintf(out, "Endpoint: %s\n", ep.Path)
	fmt.Fprintf(out, "Status: %s\n\n", authoringEndpointStatusLine(ep))
	fmt.Fprintf(out, "x-internal: %s\n", formatXInternalList(ep.XInternal))

	for i, c := range ep.PerConsumer {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out)
		renderAuthoringConsumer(out, ep, c)
	}
}

func renderAuthoringConsumer(out io.Writer, ep AuthoringEndpoint, c AuthoringConsumer) {
	if c.Status == AuthoringVerdictMissingImplementation {
		fmt.Fprintf(out, "%s: Missing Implementation\n", authoringConsumerDisplayName(c.Name))
		return
	}

	fmt.Fprintf(out, "%s:\n\n", authoringConsumerDisplayName(c.Name))
	fmt.Fprintf(out, "Route: %s\n", formatLocation(c.RouterFile, c.RouterLine))
	fmt.Fprintf(out, "Handler: %s\n", formatHandlerLocation(c))

	if c.Status != AuthoringVerdictXInternalDrift {
		renderBodyDrift(out, "Request Body Drift", c.Findings, "request-body", c.RequestType)
		renderBodyDrift(out, "Response Body Drift", c.Findings, "response-body", c.ResponseType)
	}
	renderFindingsByKind(out, "x-annotation Drift", c.Findings, "x-internal")
	renderAuthDrift(out, ep, c)
}

func cliConsumerFor(ep AuthoringEndpoint, consumerName string) *AuthoringConsumer {
	for i := range ep.PerConsumer {
		if ep.PerConsumer[i].Name == consumerName {
			return &ep.PerConsumer[i]
		}
	}
	return nil
}

func authoringEndpointStatusLine(ep AuthoringEndpoint) string {
	if len(ep.PerConsumer) == 0 {
		return authoringStatusDisplay(ep.Verdict)
	}
	parts := make([]string, 0, len(ep.PerConsumer))
	for _, c := range ep.PerConsumer {
		parts = append(parts, fmt.Sprintf("%s %s", authoringConsumerDisplayName(c.Name), authoringStatusDisplay(c.Status)))
	}
	return strings.Join(parts, ", ")
}

func authoringStatusDisplay(status string) string {
	switch status {
	case AuthoringVerdictReady:
		return "Aligned"
	case AuthoringVerdictAdvisoryOnly:
		return "Aligned (advisory)"
	case AuthoringVerdictContractMismatch, AuthoringVerdictXInternalDrift, AuthoringVerdictSecurityDrift:
		return "Drift"
	case AuthoringVerdictMissingImplementation:
		return "Missing Implementation"
	case AuthoringVerdictInsufficientEvidence:
		return "Not Audited"
	case AuthoringVerdictScopeMismatch:
		return "Out of Scope"
	case AuthoringVerdictSchemaIncomplete:
		return "Schema Incomplete"
	default:
		if status == "" {
			return "Not Audited"
		}
		return status
	}
}

func authoringConsumerDisplayName(name string) string {
	switch name {
	case authoringConsumerMeshery:
		return "Meshery"
	case authoringConsumerCloud:
		return "Cloud"
	default:
		return name
	}
}

func formatXInternalList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[" + strings.Join(values, ", ") + "]"
}

func formatLocation(file string, line int) string {
	if file == "" {
		return "-"
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

func formatHandlerLocation(c AuthoringConsumer) string {
	if c.HandlerName == "" {
		return "-"
	}
	loc := formatLocation(c.HandlerFile, c.HandlerLine)
	if loc == "-" {
		return c.HandlerName
	}
	return fmt.Sprintf("%s %s", c.HandlerName, loc)
}

func renderBodyDrift(out io.Writer, title string, findings []AuthoringFinding, kind, typeRef string) {
	messages := findingMessages(findings, kind, authoringSeverityError)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s:\n", title)
	if len(messages) == 0 {
		fmt.Fprintln(out, "Schema vs Implementation: Aligned")
		return
	}
	renderBodyDriftMessages(out, messages, typeRef)
}

func renderFindingsByKind(out io.Writer, title string, findings []AuthoringFinding, kind string) {
	messages := findingMessages(findings, kind, authoringSeverityError)
	if len(messages) == 0 {
		return
	}
	renderMessages(out, title, messages)
}

func renderFindings(out io.Writer, title string, findings []AuthoringFinding) {
	messages := make([]string, 0, len(findings))
	for _, finding := range findings {
		if finding.Message != "" {
			messages = append(messages, finding.Message)
		}
	}
	if len(messages) == 0 {
		return
	}
	renderMessages(out, title, messages)
}

func renderAuthDrift(out io.Writer, _ AuthoringEndpoint, c AuthoringConsumer) {
	messages := findingMessages(c.Findings, "security", authoringSeverityError)
	if len(messages) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Authentication Drift:")
	for _, msg := range messages {
		fmt.Fprintf(out, "%s\n", msg)
	}
}

type bodyFieldDriftGroup struct {
	Prefix     string
	Extra      []string
	Missing    []string
	Mismatched []string
	Other      []string
}

func renderBodyDriftMessages(out io.Writer, messages []string, typeRef string) {
	groups := groupBodyFieldDrift(messages)
	for i, group := range groups {
		if i > 0 {
			fmt.Fprintln(out)
		}
		renderBodyFieldDriftGroup(out, group, typeRef)
	}
}

func groupBodyFieldDrift(messages []string) []bodyFieldDriftGroup {
	ordered := make([]string, 0, len(messages))
	byPrefix := make(map[string]*bodyFieldDriftGroup, len(messages))
	add := func(prefix string) *bodyFieldDriftGroup {
		if prefix == "" {
			prefix = "Implementation"
		}
		group := byPrefix[prefix]
		if group == nil {
			group = &bodyFieldDriftGroup{Prefix: prefix}
			byPrefix[prefix] = group
			ordered = append(ordered, prefix)
		}
		return group
	}
	for _, msg := range messages {
		prefix, field := parseFieldDriftMessage(msg, " has extra field ")
		if field != "" {
			add(prefix).Extra = append(add(prefix).Extra, field)
			continue
		}
		prefix, field = parseFieldDriftMessage(msg, " missing field ")
		if field != "" {
			add(prefix).Missing = append(add(prefix).Missing, field)
			continue
		}
		prefix, field = parseTypeMismatchMessage(msg)
		if field != "" {
			add(prefix).Mismatched = append(add(prefix).Mismatched, field)
			continue
		}
		add("").Other = append(add("").Other, msg)
	}

	out := make([]bodyFieldDriftGroup, 0, len(ordered))
	for _, prefix := range ordered {
		group := byPrefix[prefix]
		sort.Strings(group.Extra)
		sort.Strings(group.Missing)
		sort.Strings(group.Mismatched)
		sort.Strings(group.Other)
		out = append(out, *group)
	}
	return out
}

func renderBodyFieldDriftGroup(out io.Writer, group bodyFieldDriftGroup, typeRef string) {
	wrote := false
	if len(group.Extra) > 0 {
		fmt.Fprintf(out, "%s has:\n", group.Prefix)
		fmt.Fprintln(out, "  extra field")
		writeWrappedFieldList(out, group.Extra, "    ")
		wrote = true
	}
	if len(group.Missing) > 0 {
		if wrote {
			fmt.Fprintln(out)
		}
		if shouldCollapseMissingForManualReview(group, typeRef) {
			fmt.Fprintf(out, "%s uses %s; inspect manually for inherited or wrapper fields.\n", group.Prefix, compactAuthoringTypeRef(typeRef))
			wrote = true
		} else {
			fmt.Fprintf(out, "%s missing field:\n", group.Prefix)
			writeWrappedFieldList(out, group.Missing, "  ")
			wrote = true
		}
	}
	if len(group.Mismatched) > 0 {
		if wrote {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s type mismatch:\n", group.Prefix)
		writeWrappedFieldList(out, group.Mismatched, "  ")
		wrote = true
	}
	if len(group.Other) > 0 {
		if wrote {
			fmt.Fprintln(out)
		}
		for _, msg := range group.Other {
			fmt.Fprintf(out, "%s\n", msg)
		}
	}
}

func shouldCollapseMissingForManualReview(group bodyFieldDriftGroup, typeRef string) bool {
	const noisyMissingFieldThreshold = 5
	return len(group.Missing) >= noisyMissingFieldThreshold && strings.TrimSpace(typeRef) != ""
}

func compactAuthoringTypeRef(typeRef string) string {
	typeRef = strings.TrimSpace(typeRef)
	if typeRef == "" {
		return "the implementation type"
	}
	lastSlash := strings.LastIndex(typeRef, "/")
	if lastSlash < 0 {
		return typeRef
	}
	suffix := typeRef[lastSlash+1:]
	lastDot := strings.LastIndex(suffix, ".")
	if lastDot < 0 {
		return suffix
	}
	return suffix
}

func writeWrappedFieldList(out io.Writer, fields []string, indent string) {
	const maxLineLen = 110
	var lineFields []string
	lineLen := len(indent)
	for _, field := range fields {
		addLen := len(field)
		if len(lineFields) > 0 {
			addLen += len(", ")
		}
		if len(lineFields) > 0 && lineLen+addLen > maxLineLen {
			fmt.Fprintf(out, "%s%s,\n", indent, strings.Join(lineFields, ", "))
			lineFields = []string{field}
			lineLen = len(indent) + len(field)
			continue
		}
		lineFields = append(lineFields, field)
		lineLen += addLen
	}
	if len(lineFields) > 0 {
		fmt.Fprintf(out, "%s%s\n", indent, strings.Join(lineFields, ", "))
	}
}

func parseFieldDriftMessage(msg, marker string) (string, string) {
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return "", ""
	}
	prefix := msg[:idx]
	field := msg[idx+len(marker):]
	if field == "" {
		return "", ""
	}
	return prefix, field
}

func parseTypeMismatchMessage(msg string) (string, string) {
	marker := " field "
	idx := strings.Index(msg, marker)
	if idx < 0 || !strings.Contains(msg[idx+len(marker):], " type mismatch ") {
		return "", ""
	}
	prefix := msg[:idx]
	field := msg[idx+len(marker):]
	field = strings.Replace(field, " type mismatch ", " ", 1)
	return prefix, field
}

func renderMessages(out io.Writer, title string, messages []string) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s:\n", title)
	for _, msg := range messages {
		fmt.Fprintf(out, "%s\n", msg)
	}
}

func findingMessages(findings []AuthoringFinding, kind, severity string) []string {
	var messages []string
	for _, finding := range findings {
		if finding.Kind != kind {
			continue
		}
		if severity != "" && finding.Severity != severity {
			continue
		}
		if finding.Message != "" {
			messages = append(messages, finding.Message)
		}
	}
	sort.Strings(messages)
	return messages
}

func consumerHasFinding(c AuthoringConsumer, kind, severity string) bool {
	return len(findingMessages(c.Findings, kind, severity)) > 0
}

// cliRowFor returns the display label and compact detail for one endpoint+consumer pair.
// Returns ("", "") for ready or suppressed verdicts.
func cliRowFor(ep AuthoringEndpoint, c *AuthoringConsumer, consumerName string) (label, detail string) {
	if c == nil {
		return "", ""
	}
	switch c.Status {
	case AuthoringVerdictMissingImplementation:
		return "not found", ""
	case AuthoringVerdictContractMismatch:
		return "drift", cliCompactDiff(c.Findings)
	case AuthoringVerdictSecurityDrift:
		return "security", cliSecurityNote(ep, c)
	case AuthoringVerdictXInternalDrift:
		return "x-internal", cliXInternalNote(ep, consumerName)
	default:
		return "", ""
	}
}

// cliCompactDiff extracts req/resp field diffs from findings into compact notation.
// Example: "req: -name, +owner  resp: -updatedAt"
func cliCompactDiff(findings []AuthoringFinding) string {
	var reqParts, respParts []string
	for _, f := range findings {
		tok := cliFieldToken(f.Message)
		if tok == "" {
			continue
		}
		switch f.Kind {
		case "request-body":
			reqParts = append(reqParts, tok)
		case "response-body":
			respParts = append(respParts, tok)
		}
	}
	var parts []string
	if len(reqParts) > 0 {
		sort.Strings(reqParts)
		parts = append(parts, "req: "+strings.Join(reqParts, ", "))
	}
	if len(respParts) > 0 {
		sort.Strings(respParts)
		parts = append(parts, "resp: "+strings.Join(respParts, ", "))
	}
	return strings.Join(parts, "  ")
}

// cliFieldToken converts a finding message to a compact field notation.
// "-name" = missing in handler, "+name" = extra in handler, "name:A≠B" = type mismatch.
func cliFieldToken(msg string) string {
	name := quotedFieldName(msg)
	if name == "" {
		return ""
	}
	switch {
	case strings.Contains(msg, " missing field "):
		return "-" + name
	case strings.Contains(msg, " has extra field "):
		return "+" + name
	case strings.Contains(msg, " type mismatch "):
		schema := textBetween(msg, "(schema ", ",")
		consumer := textBetween(msg, ", consumer ", ")")
		if schema != "" && consumer != "" {
			return name + ":" + schema + "≠" + consumer
		}
		return "~" + name
	}
	return ""
}

// cliSecurityNote produces "schema auth · route public" or the reverse.
func cliSecurityNote(ep AuthoringEndpoint, c *AuthoringConsumer) string {
	schemaLabel := "auth"
	if ep.Public {
		schemaLabel = "public"
	}
	routeLabel := "?"
	if c != nil && c.AnonymousAccess != "" {
		switch strings.ToUpper(c.AnonymousAccess) {
		case "TRUE":
			routeLabel = "public"
		case "FALSE":
			routeLabel = "auth"
		default:
			routeLabel = strings.ToLower(c.AnonymousAccess)
		}
	}
	return fmt.Sprintf("schema %s · route %s", schemaLabel, routeLabel)
}

// cliXInternalNote produces "schema cloud-only · meshery has stray route".
func cliXInternalNote(ep AuthoringEndpoint, consumerName string) string {
	return fmt.Sprintf("schema %s · %s has stray route", cliXInternalScope(ep.XInternal), consumerName)
}

func cliXInternalScope(xInternal []string) string {
	if len(xInternal) == 0 {
		return "all"
	}
	if len(xInternal) == 1 {
		return xInternal[0] + "-only"
	}
	return strings.Join(xInternal, "+")
}

// cliCountsForScope returns a summary string like "5 ok  1 not found  1 drift"
// for the given consumer. Suppressed verdicts (advisory, insufficient-evidence, etc.)
// are counted as ok.
func cliCountsForScope(consumerName string, endpoints []AuthoringEndpoint) string {
	counts := map[string]int{}
	for _, ep := range endpoints {
		c := cliConsumerFor(ep, consumerName)
		if c == nil {
			continue
		}
		switch c.Status {
		case AuthoringVerdictMissingImplementation:
			counts["not found"]++
		case AuthoringVerdictContractMismatch:
			counts["drift"]++
		case AuthoringVerdictSecurityDrift:
			counts["security"]++
		case AuthoringVerdictXInternalDrift:
			counts["x-internal"]++
		default:
			counts["ok"]++
		}
	}
	order := []string{"ok", "not found", "drift", "security", "x-internal"}
	var parts []string
	for _, k := range order {
		v := counts[k]
		if v == 0 && k != "ok" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", v, k))
	}
	return strings.Join(parts, "  ")
}

// --- Shared helpers (used by markdown renderer and above) ---

func endpointsBySourceFile(endpoints []AuthoringEndpoint) map[string][]AuthoringEndpoint {
	byFile := make(map[string][]AuthoringEndpoint)
	for _, ep := range endpoints {
		byFile[ep.SourceFile] = append(byFile[ep.SourceFile], ep)
	}
	for file := range byFile {
		sort.Slice(byFile[file], func(i, j int) bool {
			if byFile[file][i].Path != byFile[file][j].Path {
				return byFile[file][i].Path < byFile[file][j].Path
			}
			return byFile[file][i].Method < byFile[file][j].Method
		})
	}
	return byFile
}

func markdownEscape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", "<br>")
}

func resultHasRawEvidence(result *ConsumerAuthoringResult) bool {
	if result == nil {
		return false
	}
	for _, ep := range result.Endpoints {
		for _, consumer := range ep.PerConsumer {
			if consumer.WritesRaw {
				return true
			}
		}
	}
	return false
}

func quotedFieldName(msg string) string {
	start := strings.Index(msg, "\"")
	if start < 0 {
		return ""
	}
	rest := msg[start+1:]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func textBetween(s, prefix, suffix string) string {
	start := strings.Index(s, prefix)
	if start < 0 {
		return ""
	}
	rest := s[start+len(prefix):]
	end := strings.Index(rest, suffix)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
