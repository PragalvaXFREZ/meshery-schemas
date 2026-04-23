package validation

import (
	"regexp"
	"strings"
	"unicode"
)

// Casing validation helpers — ported from build/validate-schemas.js lines 238–381.
//
// Phase 4.D — deferred pruning of `knownLowercaseSuffixViolations`.
//
// Per Agent 4.D of docs/identifier-naming-migration.md, once every resource
// is migrated to the canonical camelCase-on-the-wire contract, individual
// entries in the `knownLowercaseSuffixViolations` map below become dead
// code — their listed property names no longer exist on any published
// schema. Phase 3 is still in flight (per-resource versioned migrations),
// so each of these entries may still occur in a legacy construct that has
// not yet been version-bumped. Accordingly, this file intentionally
// retains the full map rather than pruning on a best-effort basis.
//
// The pruning rule: when the last per-resource offender for a given entry
// is removed (i.e., after the matching Phase 3.<Resource> migration lands
// and its legacy construct is deleted by Phase 4.A), the corresponding
// entry in the map can be deleted in a follow-up PR. Until then, each
// entry documents a real drift class still reachable through at least one
// legacy resource — removing it prematurely would silently disable the
// Rule 6 camelCase-suffix diagnostic for that class and allow a reviewer
// to land a regression.
//
// This is the "noted and deferred" resolution of Phase 4.D: the rule
// surface is left untouched; the follow-up prune tracks Phase 3 completion
// on a per-entry cadence rather than in a single sweep.
//
// Concretely, as of this commit only `userid` is still referenced by
// live schema YAML (v1alpha1/core/api.yml and v1beta1/core/api.yml
// define a shared `userid` query parameter); the remaining entries
// cover resources whose Phase 3 migrations are either pending or whose
// legacy-version sunset (Phase 4.A) has not yet removed the offending
// file. Do not read the "zero live occurrences" state as permission to
// delete the entry — Phase 4.A is the sole authorised removal path.

var (
	camelCaseRE  = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	pascalCaseRE = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)
	kebabCaseRE  = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	dbTagPattern = regexp.MustCompile(`^(?:-|[a-z][a-z0-9]*(?:_[a-z0-9]+)*)$`)

	// screamingIDRE detects SCREAMING_CASE "ID" in a mixed-case string.
	// e.g., "getByID" or "orgID" — the correct form is "Id".
	screamingIDRE = regexp.MustCompile(`(?:[a-z0-9])ID(?:[A-Z0-9]|$)`)

	// operationIDRE validates lower camelCase verbNoun identifiers.
	operationIDRE = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[A-Z][a-z0-9]*)+$`)

	// screamingOpIDRE detects "ID" suffix in operationId.
	screamingOpIDRE = regexp.MustCompile(`[a-z]ID(?:[A-Z]|$)`)

	// pathParamRE extracts path parameter names from route paths.
	pathParamRE = regexp.MustCompile(`\{([^}]+)\}`)

	// lowercaseSuffixPattern matches compound words with all-lowercase known
	// suffixes that should be capitalized (e.g., "userid" → "userId").
	lowercaseSuffixPattern = regexp.MustCompile(`[a-z](id|ids|url|uri)$`)
)

// knownLowercaseSuffixViolations lists compound property names that end in
// a known suffix ("id", "url", "uri") but are incorrectly all-lowercase.
var knownLowercaseSuffixViolations = map[string]bool{
	"userid": true, "orgid": true, "teamid": true, "workspaceid": true,
	"modelid": true, "designid": true, "connectionid": true,
	"environmentid": true, "credentialid": true, "subscriptionid": true,
	"invitationid": true, "tokenid": true, "eventid": true, "keyid": true,
	"roleid": true, "badgeid": true, "planid": true, "schemaid": true,
	"registrantid": true, "componentid": true, "categoryid": true,
	"pageurl": true, "avatarurl": true, "snapshoturl": true,
	"callbackurl": true, "redirecturl": true,
}

// dbMirroredFields enumerates known snake_case property names that originated
// as DB column mirrors in pre-canonical-contract schemas.
//
// Under the canonical identifier-naming contract (see AGENTS.md § Casing
// rules at a glance and docs/identifier-naming-migration.md), wire tags are
// camelCase regardless of DB backing — so these names are no longer treated
// as an exception to Rule 6. They surface as Rule 6 violations and are
// routed through the same `--style-debt` severity path as every other
// legacy snake_case violation. The set remains defined because
// matcher.go still uses it to distinguish server-generated / DB-mirrored
// fields from request-side client input when diffing consumer Go types
// against schema shapes.
var dbMirroredFields = map[string]bool{
	"created_at": true, "updated_at": true, "deleted_at": true,
	"user_id": true, "org_id": true, "organization_id": true,
	"environment_id": true, "workspace_id": true, "team_id": true,
	"design_id": true, "credential_id": true, "connection_id": true,
	"system_id": true, "operation_id": true, "view_id": true,
	"general_id": true, "invite_id": true, "content_id": true,
	"badge_id": true, "plan_id": true, "access_expires_at": true,
	"avatar_url": true, "accepted_terms_at": true,
	"page_size": true, "total_count": true,
}

// serverGeneratedFields are fields that should never be required in write
// payloads (POST/PUT/PATCH requestBody).
var serverGeneratedFields = map[string]bool{
	"id": true, "created_at": true, "updated_at": true, "deleted_at": true,
}

// IsCamelCase returns true if s starts lowercase with no underscores/hyphens.
func IsCamelCase(s string) bool {
	return camelCaseRE.MatchString(s)
}

// IsPascalCase returns true if s starts uppercase with no underscores/hyphens.
func IsPascalCase(s string) bool {
	return pascalCaseRE.MatchString(s)
}

// IsKebabCase returns true if s is lowercase with hyphens only.
func IsKebabCase(s string) bool {
	return kebabCaseRE.MatchString(s)
}

// IsValidDBTag returns true if the value matches the db tag pattern.
func IsValidDBTag(s string) bool {
	return dbTagPattern.MatchString(s)
}

// HasUnderscore returns true if the string contains an underscore.
func HasUnderscore(s string) bool {
	return strings.Contains(s, "_")
}

// HasScreamingIDToken returns true if s contains SCREAMING_CASE "ID".
func HasScreamingIDToken(s string) bool {
	return screamingIDRE.MatchString(s)
}

// HasLowercaseSuffix returns true if s is a known lowercase-suffix violation.
func HasLowercaseSuffix(s string) bool {
	return knownLowercaseSuffixViolations[s]
}

// ToCamelCase converts a property name to its expected camelCase form.
func ToCamelCase(s string) string {
	// Replace non-alphanumeric separators followed by a letter/digit with uppercase.
	var result strings.Builder
	capitalizeNext := false
	for i, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			capitalizeNext = true
			continue
		}
		if capitalizeNext {
			result.WriteRune(unicode.ToUpper(r))
			capitalizeNext = false
		} else if i == 0 {
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}

	// Replace screaming "ID" with "Id" only at word boundaries (suffix or
	// followed by an uppercase letter), not inside words like "IDENTITY".
	out := result.String()
	out = regexp.MustCompile(`ID(?:[A-Z0-9]|$)`).ReplaceAllStringFunc(out, func(m string) string {
		if len(m) > 2 {
			return "Id" + m[2:]
		}
		return "Id"
	})
	return out
}

// GetCamelCaseSuggestion returns a camelCase suggestion for a name, or empty
// string if no valid suggestion can be generated.
func GetCamelCaseSuggestion(name string) string {
	suggestion := ToCamelCase(name)
	// Strip leading non-alpha and non-alnum characters.
	suggestion = strings.TrimLeftFunc(suggestion, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	if IsCamelCase(suggestion) {
		return suggestion
	}
	return ""
}

// CasingIssue describes a single casing violation on a name.
type CasingIssue struct {
	Description string
}

// GetCamelCaseIssues checks a wire identifier (schema property name,
// OpenAPI query/header parameter name, or any similar camelCase-expected
// token) for casing violations.
//
// Under the canonical identifier-naming contract (AGENTS.md § Casing rules
// at a glance, docs/identifier-naming-migration.md §1), wire names are
// camelCase regardless of DB backing — the snake_case DB column name lives
// only in `x-oapi-codegen-extra-tags.db`, not on the wire. Accordingly this
// checker is unconditional: there is no DB-mirroring exception. The
// legacy-DB-mirrored field set (dbMirroredFields) remains defined for use
// by matcher.go's consumer-type diff, but it is no longer an exception.
//
// The returned issue descriptions are context-agnostic — each caller
// (Rule 6 for schema/entity properties, Rule 9 for query/header
// parameters, etc.) is responsible for adding any context-specific
// guidance on top of the generic message. Severity is determined at the
// caller via classifyStyleIssue / the --style-debt / --strict-consistency
// flags.
func GetCamelCaseIssues(name string) []CasingIssue {
	var issues []CasingIssue

	if HasUnderscore(name) {
		issues = append(issues, CasingIssue{
			Description: "uses snake_case (must be camelCase)",
		})
	}
	if len(name) > 0 && unicode.IsUpper(rune(name[0])) {
		issues = append(issues, CasingIssue{
			Description: "starts with uppercase (must be camelCase, not PascalCase)",
		})
	}
	if HasScreamingIDToken(name) {
		issues = append(issues, CasingIssue{
			Description: `uses "ID" token (must be "Id")`,
		})
	}
	if HasLowercaseSuffix(name) {
		lc := strings.ToLower(name)
		for _, suffix := range []string{"ids", "id", "url", "uri"} {
			if strings.HasSuffix(lc, suffix) {
				corrected := name[:len(name)-len(suffix)] +
					strings.ToUpper(suffix[:1]) + suffix[1:]
				issues = append(issues, CasingIssue{
					Description: `has all-lowercase suffix "` + suffix + `" (must be "` + corrected + `")`,
				})
				break
			}
		}
	}
	if len(name) > 0 && unicode.IsDigit(rune(name[0])) {
		issues = append(issues, CasingIssue{
			Description: "starts with a digit",
		})
	}

	if len(issues) == 0 && !IsCamelCase(name) {
		issues = append(issues, CasingIssue{
			Description: "is not valid camelCase",
		})
	}

	return issues
}

// IsValidOperationID checks if an operationId is lower camelCase verbNoun.
func IsValidOperationID(opID string) bool {
	return operationIDRE.MatchString(opID)
}

// HasScreamingOperationIDSuffix checks if an operationId ends with "ID".
func HasScreamingOperationIDSuffix(opID string) bool {
	return screamingOpIDRE.MatchString(opID) || strings.HasSuffix(opID, "ID")
}

// SuggestOperationID converts a screaming "ID" suffix to "Id".
func SuggestOperationID(opID string) string {
	re := regexp.MustCompile(`ID(?:[A-Z]|$)`)
	return re.ReplaceAllStringFunc(opID, func(m string) string {
		if len(m) > 2 {
			return "Id" + m[2:]
		}
		return "Id"
	})
}

// IsBadPathParam returns true if a path parameter name uses incorrect casing.
func IsBadPathParam(param string) bool {
	if len(param) > 0 && unicode.IsUpper(rune(param[0])) {
		return true
	}
	if strings.HasSuffix(param, "ID") {
		return true
	}
	if param == "id" {
		return false
	}
	if strings.HasSuffix(param, "id") && !strings.HasSuffix(param, "Id") {
		return true
	}
	if strings.Contains(param, "_") {
		return true
	}
	return false
}

// SuggestPathParam generates the correct camelCase form for a path parameter.
func SuggestPathParam(param string) string {
	if param == "id" {
		return param
	}
	// Normalize separators.
	normalized := ToCamelCase(param)
	// Ensure "id"/"ID" suffix becomes "Id".
	if strings.HasSuffix(strings.ToLower(normalized), "id") &&
		!strings.HasSuffix(normalized, "Id") {
		normalized = normalized[:len(normalized)-2] + "Id"
	}
	return normalized
}
