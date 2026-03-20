#!/usr/bin/env node
/**
 * validate-schemas.js — Schema Design Validation
 *
 * DESCRIPTION:
 *   Validates that all v1beta1+ entity schemas follow the design rules documented in
 *   AGENTS.md.
 *
 *   Rule 1 — Entity schemas (<construct>.yaml) must have `additionalProperties: false`
 *             at the top level.
 *
 *   Rule 2 — POST/PUT requestBody schemas must not contain server-generated fields
 *             (id, created_at, updated_at, deleted_at) in their `required` array.
 *             This indicates the full entity schema is being used as a request body
 *             instead of a dedicated *Payload schema.
 *
 *   Rule 3 — `operationId` values must start with a lowercase letter (lower camelCase).
 *             PascalCase operationIds (e.g. GetConnections, CreateTeam) violate the
 *             naming convention. All schemas have been updated; new additions must
 *             follow lower camelCase verbNoun (e.g. getConnections, createTeam).
 *
 *   Rule 4 — Path parameters must use camelCase with the "Id" suffix, never
 *             SCREAMING_CASE (e.g. {orgID}) or snake_case (e.g. {org_id}).
 *
 *   Rule 5 — DELETE operations must not have a requestBody. Bulk deletes must use a
 *             POST sub-resource (e.g. POST /api/designs/delete) because REST semantics
 *             do not define a request body for DELETE and many clients strip it silently.
 *
 * USAGE:
 *   node build/validate-schemas.js          # exits 0 if clean, 1 if violations found
 *   node build/validate-schemas.js --warn   # always exits 0, only prints warnings
 *
 * DEPENDENCIES:
 *   js-yaml (already a project dependency)
 */

"use strict";

const fs = require("fs");
const path = require("path");
const yaml = require("js-yaml");

const ROOT = path.resolve(__dirname, "..");
const CONSTRUCTS_DIR = path.join(ROOT, "schemas", "constructs");

// Fields that are always server-generated and should never be required in write payloads.
const SERVER_GENERATED_FIELDS = new Set(["id", "created_at", "updated_at", "deleted_at"]);

// Only validate these versions (alpha schemas predate the pattern).
const VALIDATED_VERSIONS = ["v1beta1", "v1beta2-draft"];

const warnOnly = process.argv.includes("--warn");
const violations = [];

function warn(file, message) {
  violations.push({ file: path.relative(ROOT, file), message });
}

// ─── Rule 1: entity schemas must have additionalProperties: false ─────────────

function validateEntitySchema(filePath) {
  let doc;
  try {
    doc = yaml.load(fs.readFileSync(filePath, "utf-8"));
  } catch (e) {
    return; // unparseable — not our concern here
  }

  if (!doc || typeof doc !== "object") return;
  if (doc.type !== "object") return; // skip non-object schemas (enums, etc.)

  if (doc.additionalProperties !== false) {
    warn(
      filePath,
      `Missing \`additionalProperties: false\` at top level. ` +
        `Entity schemas must set this to prevent unknown fields in generated structs.`,
    );
  }
}

// ─── Rule 2: POST/PUT requestBody must not use the full entity schema ─────────

function resolveLocalRef(ref, components) {
  // Only handle local component refs: "#/components/schemas/Foo"
  const match = ref && ref.match(/^#\/components\/schemas\/(.+)$/);
  if (!match) return null;
  return components?.schemas?.[match[1]] ?? null;
}

function getRequiredFields(schema, components) {
  if (!schema) return [];
  // Follow a top-level $ref to a local component
  if (schema.$ref) {
    const resolved = resolveLocalRef(schema.$ref, components);
    return resolved ? getRequiredFields(resolved, components) : [];
  }
  return Array.isArray(schema.required) ? schema.required : [];
}

function validateApiSpec(filePath, doc) {
  if (!doc?.paths) return;
  const components = doc.components ?? {};

  for (const [routePath, pathItem] of Object.entries(doc.paths)) {
    for (const method of ["post", "put", "patch"]) {
      const op = pathItem[method];
      if (!op?.requestBody) continue;

      // Collect all content schemas for this requestBody
      const contentMap = op.requestBody.content ?? {};
      for (const [mediaType, mediaObj] of Object.entries(contentMap)) {
        if (!mediaObj?.schema) continue;

        const required = getRequiredFields(mediaObj.schema, components);
        const offending = required.filter((f) => SERVER_GENERATED_FIELDS.has(f));

        if (offending.length > 0) {
          const schemaRef = mediaObj.schema.$ref ?? "(inline)";
          warn(
            filePath,
            `${method.toUpperCase()} ${routePath} — requestBody schema ${schemaRef} ` +
              `has server-generated field(s) in \`required\`: [${offending.join(", ")}]. ` +
              `Use a dedicated *Payload schema that omits server-generated required fields. ` +
              `See AGENTS.md § "The Dual-Schema Pattern".`,
          );
        }
      }
    }
  }
}

// ─── Rule 3: operationId must be lower camelCase ──────────────────────────────

// Enforce lower camelCase verbNoun identifiers such as getPatterns.
// This rejects PascalCase, underscores, punctuation, and single-word lowercase IDs.
const OPERATION_ID_RE = /^[a-z][a-z0-9]*(?:[A-Z][a-z0-9]*)+$/;

function validateOperationIds(filePath, doc) {
  if (!doc?.paths) return;

  for (const [routePath, pathItem] of Object.entries(doc.paths)) {
    for (const method of ["get", "post", "put", "patch", "delete"]) {
      const op = pathItem[method];
      if (!op?.operationId) continue;

      if (!OPERATION_ID_RE.test(op.operationId)) {
        warn(
          filePath,
          `${method.toUpperCase()} ${routePath} — operationId "${op.operationId}" ` +
            `must use lower camelCase verbNoun without underscores or other separators (e.g. "getPatterns"). ` +
            `See AGENTS.md § "Naming conventions".`,
        );
      }
    }
  }
}

// ─── Rule 4: path parameters must be camelCase with Id suffix ─────────────────

// Matches all path parameters like {somethingId}; actual validity checks happen in
// isBadPathParam, which flags forms like {somethingID} and {something_id}.
const PATH_PARAM_RE = /\{([^}]+)\}/g;

function isBadPathParam(param) {
  // Path params should use lower camelCase.
  if (/^[A-Z]/.test(param)) return true;
  // Flag SCREAMING_CASE suffix: ends with ID (two uppercase letters)
  if (param.endsWith("ID")) return true;
  // Keep plain legacy {id} allowed, but flag lowercase id suffixes like orgid;
  // canonical form for multiword names is orgId.
  if (param === "id") return false;
  if (/id$/.test(param) && !param.endsWith("Id")) return true;
  // Flag snake_case: contains underscore
  if (/_/.test(param)) return true;
  return false;
}

function suggestPathParam(param) {
  if (param === "id") return param;

  const normalized = param
    .replace(/[_-]+([a-zA-Z0-9])/g, (_, c) => c.toUpperCase())
    .replace(/^([A-Z])/, (match) => match.toLowerCase());

  return normalized.replace(/(?:ID|id)$/, "Id");
}

function validatePathParams(filePath, doc) {
  if (!doc?.paths) return;

  for (const routePath of Object.keys(doc.paths)) {
    let match;
    PATH_PARAM_RE.lastIndex = 0;
    while ((match = PATH_PARAM_RE.exec(routePath)) !== null) {
      const param = match[1];
      if (isBadPathParam(param)) {
        const suggestion = suggestPathParam(param);
        warn(
          filePath,
          `Path "${routePath}" — parameter {${param}} uses incorrect casing. ` +
            `Use camelCase with "Id" suffix: {${suggestion}}. ` +
            `See AGENTS.md § "Naming conventions".`,
        );
      }
    }
  }
}

// ─── Rule 5: DELETE must not have a requestBody ───────────────────────────────

function validateDeleteNoBody(filePath, doc) {
  if (!doc?.paths) return;

  for (const [routePath, pathItem] of Object.entries(doc.paths)) {
    const op = pathItem["delete"];
    if (!op) continue;

    if (op.requestBody) {
      warn(
        filePath,
        `DELETE ${routePath} — DELETE operations must not have a requestBody. ` +
          `Use a POST sub-resource for bulk deletes (e.g. POST ${routePath}/delete). ` +
          `See AGENTS.md § "HTTP API Design Principles".`,
      );
    }
  }
}

// ─── Walk constructs directory ────────────────────────────────────────────────

function shouldValidateVersion(version) {
  return VALIDATED_VERSIONS.some((v) => version === v || version.startsWith(v));
}

function walk(dir) {
  if (!fs.existsSync(dir)) return;

  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;

    const version = entry.name;
    if (!shouldValidateVersion(version)) continue;

    const versionDir = path.join(dir, version);

    for (const construct of fs.readdirSync(versionDir, { withFileTypes: true })) {
      if (!construct.isDirectory()) continue;

      const constructDir = path.join(versionDir, construct.name);

      // Rule 1: check all top-level *.yaml files (entity schemas, not api.yml)
      for (const file of fs.readdirSync(constructDir)) {
        if (
          file.endsWith(".yaml") &&
          file !== "api.yml" &&
          !file.includes("_template") &&
          !file.includes("_page")
        ) {
          validateEntitySchema(path.join(constructDir, file));
        }
      }

      // Rules 2–5: check api.yml
      const apiYml = path.join(constructDir, "api.yml");
      if (fs.existsSync(apiYml)) {
        let doc;
        try {
          doc = yaml.load(fs.readFileSync(apiYml, "utf-8"));
        } catch (e) {
          doc = null;
        }
        if (doc) {
          validateApiSpec(apiYml, doc);
          validateOperationIds(apiYml, doc);
          validatePathParams(apiYml, doc);
          validateDeleteNoBody(apiYml, doc);
        }
      }
    }
  }
}

walk(CONSTRUCTS_DIR);

// ─── Report ───────────────────────────────────────────────────────────────────

if (violations.length === 0) {
  console.log("✓ validate-schemas: no violations found.");
  process.exit(0);
}

console.error(`\nvalidate-schemas: ${violations.length} violation(s) found:\n`);
for (const { file, message } of violations) {
  console.error(`  ${file}\n    → ${message}\n`);
}

if (warnOnly) {
  process.exit(0);
} else {
  process.exit(1);
}
