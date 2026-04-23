// Package validation provides build-time schema design auditing and runtime
// document validation for the meshery/schemas repository.
//
// This is a dependency-leaf package: it imports kin-openapi, yaml.v3, and the
// standard library only. It must NOT import github.com/meshery/meshkit or
// github.com/meshery/schemas/models. This constraint is architectural — it
// allows meshkit/schema and model helper methods to import this package
// without creating cycles.
//
// Three concerns live here:
//
//   - Schema Auditing: validates that OpenAPI schema files follow project
//     conventions (naming, dual-schema pattern, codegen annotations, etc.).
//     Invoked at build time via cmd/validate-schemas.
//
//   - Document Validation: validates JSON documents against OpenAPI schemas
//     at runtime using kin-openapi. Used by Meshery server and CLI.
//
//   - Consumer Auditing: walks consumer repos (meshery/meshery,
//     layer5io/meshery-cloud, layer5labs/meshery-extensions) and diffs
//     their registered endpoints against the schemas endpoint index.
//     Three parsers are registered: consumer_gorilla (Go Gorilla router in
//     meshery/meshery), consumer_echo (Go Echo router in meshery-cloud), and
//     consumer_ts (TypeScript RTK Query endpoints across all three repos).
//     The TS consumer is regex-based — full semantic TS analysis would
//     require the TypeScript compiler, and the migration plan calls for
//     simpler heuristics — and it surfaces wire-format drift such as
//     camelCase → SCREAMING case-flips (`orgID: queryArg.orgId`),
//     snake_case body wrappers (`pattern_data`, `k8s_manifest`), and
//     snake_case params keys outside the pagination envelope.
//     Invoked at review/maintenance time via cmd/consumer-audit.
package validation
