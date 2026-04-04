#!/usr/bin/env python3
"""
Meshery API Schema Audit

Compares data sources within meshery/meshery and meshery/schemas to produce
a comprehensive audit of every API endpoint:

  1. server/router/server.go    → registered API endpoints
  2. _openapi_build/merged_openapi.yml → authoritative OpenAPI spec (from this repo)
  3. server/handlers/*.go       → schema-driven check (import analysis)

For each endpoint the script computes:
  - Coverage      — Overlap / Server Underlap / Schema Underlap
  - Status        — Active / Deprecated / Unimplemented / Cloud-only
  - Schema-Backed — Is the endpoint defined in the OpenAPI spec?
  - Schema Completeness — Full / Partial / Stub / N/A
  - Schema-Driven — Does the handler import+use meshery/schemas types?

Optionally writes results to a Google Sheet. Credentials come from
environment variables — never hardcoded.

Intended to be run via Make targets from the meshery-schemas repo root:

  make api-audit            # Dry-run audit — prints summary only
  make api-audit-update     # Audit and write results to Google Sheet
  make api-audit-setup      # Install Python dependencies only

For advanced usage or CI, the script can be invoked directly:

  python build/scripts/api-audit.py --meshery-repo ../meshery --dry-run --verbose
  python build/scripts/api-audit.py --meshery-repo ../meshery --sheet-id $SHEET_ID
"""

import argparse
import json
import os
import re
import subprocess
import sys

from collections import defaultdict
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

try:
    import yaml
except ImportError:
    sys.exit("Missing dependency: pip install pyyaml")



# ---------------------------------------------------------------------------
# Paths relative to repos
# ---------------------------------------------------------------------------

# Paths relative to meshery/meshery repo root
ROUTER_FILE = "server/router/server.go"
HANDLERS_DIR = "server/handlers"

# Path relative to meshery-schemas repo root (produced by bundle-openapi)
DEFAULT_SPEC_PATH = "_openapi_build/merged_openapi.yml"


# ---------------------------------------------------------------------------
# Repo configuration (replaces hardcoded path constants for multi-repo support)
# ---------------------------------------------------------------------------

class RepoConfig:
    """Per-repo path and dialect settings for the audit pipeline."""

    def __init__(
        self,
        name: str,
        router_file: str,
        handlers_dir: str,
        go_mod: str = "go.mod",
        provider_file: Optional[str] = None,
        router_dialect: str = "gorilla",
    ):
        self.name = name
        self.router_file = router_file
        self.handlers_dir = handlers_dir
        self.go_mod = go_mod
        self.provider_file = provider_file
        self.router_dialect = router_dialect  # "gorilla" | "echo"


MESHERY_CONFIG = RepoConfig(
    name="meshery",
    router_file="server/router/server.go",
    handlers_dir="server/handlers",
    provider_file="server/models/providers.go",
    router_dialect="gorilla",
)

MESHERY_CLOUD_CONFIG = RepoConfig(
    name="meshery-cloud",
    router_file="server/router/router.go",
    handlers_dir="server/handlers",
    provider_file=None,
    router_dialect="echo",
)

# ---------------------------------------------------------------------------
# Sheet configuration
# ---------------------------------------------------------------------------
SHEET_COLUMNS = [
    "Category",
    "Sub-Category",
    "Endpoints",
    "Methods",
    "Endpoint Status",
    "x-annotated",
    "Schema-Backed (Meshery Server)",
    "Schema-Backed (Meshery Cloud)",
    "Schema Completeness (Meshery Server)",
    "Schema Completeness (Meshery Cloud)",
    "Schema Driven (Meshery Server)",
    "Schema Driven (Meshery Cloud)",
    "Notes",
    "Change Log",
]
COL_CATEGORY = 0
COL_SUBCATEGORY = 1
COL_ENDPOINTS = 2
COL_METHODS = 3
COL_STATUS = 4
COL_X_ANNOTATED = 5
COL_BACKED_MS = 6
COL_BACKED_MC = 7
COL_COMPLETENESS_MS = 8
COL_COMPLETENESS_MC = 9
COL_DRIVEN_MS = 10
COL_DRIVEN_MC = 11
COL_NOTES = 12
COL_CHANGELOG = 13

AUDIT_WORKSHEET_INDEX = 4

SUMMARY_TABLE_ROWS: List[Tuple[str, str]] = [
    ("endpoints_spec", "Spec endpoints"),
    ("implemented", "  \u251c Implemented (router match)"),
    ("unimplemented", "  \u2514 Unimplemented (no router)"),
    ("endpoints_router", "Router endpoints"),
    ("schema_backed", "  \u251c Schema-backed"),
    ("complete", "  \u2502   \u251c Complete"),
    ("incomplete", "  \u2502   \u251c Incomplete"),
    ("not_audited", "  \u2502   \u2514 Not audited"),
    ("not_schema_backed", "  \u251c Not in spec"),
    ("no_schema", "  \u2514 No schema"),
    ("x_internal_tagged", "x-internal tagged"),
    ("schema_driven", "Schema-driven"),
    ("not_schema_driven", "Not schema-driven"),
]

SUMMARY_KEYS = [key for key, _label in SUMMARY_TABLE_ROWS]

PLATFORM_LABELS = {
    "meshery": "Meshery",
    "cloud": "Cloud",
}

HTTP_METHODS = frozenset({"get", "post", "put", "delete", "patch", "options", "head"})

# Methods that typically carry a request body
BODY_METHODS = frozenset({"POST", "PUT", "PATCH"})

# Sentinel value for handler response types that are []byte passthrough
# from the Provider interface (opaque JSON forwarded without typed struct).
_PROVIDER_BYTES_SENTINEL = "<provider-[]byte>"

MIDDLEWARE_NAMES = frozenset({
    "ProviderMiddleware", "AuthMiddleware", "SessionInjectorMiddleware",
    "KubernetesMiddleware", "K8sFSMMiddleware", "GraphqlMiddleware",
    "NoCacheMiddleware",
})

# Fallback category rules for router-only endpoints that have no OpenAPI tags.
# Spec-backed endpoints derive their category from x-tagGroups / tags instead.
CATEGORY_FALLBACK: List[Tuple[str, str, str]] = [
    ("/api/system/graphql", "Meshery Server and Components", "Meshery Operator"),
    ("/api/system/database", "Meshery Server and Components", "Database"),
    ("/api/system/adapter", "Meshery Server and Components", "Adapters"),
    ("/api/system/adapters", "Meshery Server and Components", "Adapters"),
    ("/api/system/availableAdapters", "Meshery Server and Components", "Adapters"),
    ("/api/system/meshsync", "Meshery Server and Components", "Meshsync"),
    ("/api/system", "Meshery Server and Components", "System"),
    ("/api/extension", "Meshery Server and Components", "System"),
    ("/api/meshmodels", "Capabilities Registry", "Entities"),
    ("/api/meshmodel", "Capabilities Registry", "Model Lifecycle"),
    ("/api/pattern", "Configuration", "Patterns"),
    ("/api/patterns", "Configuration", "Patterns"),
    ("/api/filter", "Configuration", "Filters"),
    ("/api/content/design", "Configuration", "Patterns"),
    ("/api/content/filter", "Configuration", "Filters"),
    ("/api/perf", "Benchmarking and Validation", "Performance (SMP)"),
    ("/api/mesh", "Benchmarking and Validation", "Performance (SMP)"),
    ("/api/smi", "Benchmarking and Validation", "Conformance (SMI)"),
    ("/api/user/performance", "Benchmarking and Validation", "Performance (SMP)"),
    ("/api/user/prefs/perf", "Benchmarking and Validation", "Performance (SMP)"),
    ("/api/telemetry/metrics/grafana", "Telemetry", "Grafana API"),
    ("/api/grafana", "Telemetry", "Grafana API"),
    ("/api/telemetry/metrics", "Telemetry", "Prometheus API"),
    ("/api/prometheus", "Telemetry", "Prometheus API"),
    ("/api/identity", "Identity", "User"),
    ("/api/user", "Identity", "User"),
    ("/api/token", "Identity", "User"),
    ("/api/provider", "Identity", "Providers, Extensions"),
    ("/api/providers", "Identity", "Providers, Extensions"),
    ("/api/schema", "Meshery Server and Components", "System"),
    ("/provider", "Identity", "Providers, Extensions"),
    ("/auth", "Identity", "User"),
    ("/user/login", "Identity", "User"),
    ("/user/logout", "Identity", "User"),
    ("/swagger.yaml", "Meshery Server and Components", "System"),
    ("/docs", "Meshery Server and Components", "System"),
    ("/healthz", "Meshery Server and Components", "System"),
    ("/error", "Other", ""),
]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def normalize_path(path: str) -> str:
    """Replace path parameter tokens with positional {p1}, {p2}, … for matching.

    Handles both OpenAPI/Gorilla-Mux style ({paramName}) and Echo/Express
    style (:paramName) so routes from either framework match spec paths.
    """
    counter = [0]

    def _repl(_m):
        counter[0] += 1
        return f"{{p{counter[0]}}}"

    # Normalise Echo-style :param segments first, then OpenAPI {param} style.
    path = re.sub(r"/:([^/]+)", lambda m: "/{" + m.group(1) + "}", path)
    return re.sub(r"\{[^}]+\}", _repl, path)


def _is_api_route(path: str) -> bool:
    """Return True for paths that belong to the /api namespace.

    Non-API routes (UI, static assets, health checks, documentation) are
    excluded from primary API coverage metrics because their absence from
    the OpenAPI spec is intentional and not an audit gap.
    """
    return path.startswith("/api/") or path == "/api"


def categorize(
    path: str,
    spec_categories: Optional[Dict[str, Tuple[str, str]]] = None,
) -> Tuple[str, str]:
    """Return (category, subcategory) for a given endpoint path.

    First checks *spec_categories* (derived from OpenAPI tags / x-tagGroups).
    Falls back to CATEGORY_FALLBACK prefix rules for router-only endpoints.
    """
    norm = normalize_path(path)
    if spec_categories and norm in spec_categories:
        return spec_categories[norm]
    for prefix, cat, sub in CATEGORY_FALLBACK:
        if path.startswith(prefix):
            return cat, sub
    return "Other", ""


def endpoint_sort_key(endpoint: Dict[str, Any]) -> Tuple[str, str, str, str]:
    """Return a deterministic sort key for sheet output."""
    return (
        endpoint["category"],
        endpoint["subcategory"],
        endpoint["path"],
        endpoint["methods"],
    )


# ---------------------------------------------------------------------------
# 1. Router parser — server/router/server.go
# ---------------------------------------------------------------------------

def parse_router(
    repo: Path, config: Optional["RepoConfig"] = None
) -> List[Dict[str, Any]]:
    """Parse route registrations from the repo's router file.

    Uses *config.router_dialect* to pick the right parser:
    - "gorilla" (default) — parses gMux.Handle/HandleFunc/PathPrefix calls
    - "echo"              — parses Echo group and method calls (e.GET/POST/…)
    """
    router_file_rel = config.router_file if config else ROUTER_FILE
    router_file = repo / router_file_rel
    if not router_file.exists():
        print(f"ERROR: {router_file} not found", file=sys.stderr)
        return []

    content = router_file.read_text(errors="replace")

    dialect = config.router_dialect if config else "gorilla"
    if dialect == "echo":
        return _parse_echo_routes(content)

    lines = content.splitlines()

    # Accumulate multi-line gMux statements
    statements: List[str] = []
    current = ""
    paren_depth = 0
    in_stmt = False
    current_commented = False

    for line in lines:
        stripped = line.strip()
        if re.match(r"^\s*(//\s*)?gMux\.(Handle|HandleFunc|PathPrefix)", line):
            if current and in_stmt:
                statements.append(current)
            current = stripped
            in_stmt = True
            current_commented = stripped.startswith("//")
            paren_depth = current.count("(") - current.count(")")
            if paren_depth <= 0 and not stripped.rstrip().endswith("."):
                statements.append(current)
                current, in_stmt, paren_depth, current_commented = "", False, 0, False
            continue
        if in_stmt:
            continuation = stripped
            if current_commented:
                continuation = re.sub(r"^//\s*", "", continuation)
            current += " " + continuation
            paren_depth += continuation.count("(") - continuation.count(")")
            if paren_depth <= 0 and not continuation.rstrip().endswith("."):
                statements.append(current)
                current, in_stmt, paren_depth, current_commented = "", False, 0, False

    if current:
        statements.append(current)

    routes = []
    for stmt in statements:
        route = _parse_route(stmt)
        if route:
            routes.append(route)
    return routes


def _parse_route(stmt: str) -> Optional[Dict[str, Any]]:
    """Parse a single gMux statement into a route dict."""
    commented = stmt.lstrip().startswith("//")
    clean = re.sub(r"^//\s*", "", stmt.strip()) if commented else stmt.strip()

    path_m = re.search(
        r'gMux\.(Handle|HandleFunc|PathPrefix)\s*\(\s*"([^"]+)"', clean
    )
    if not path_m:
        return None

    path = path_m.group(2)
    methods_m = re.search(r"\.\s*Methods\(\s*(.+?)\s*\)", clean)
    methods = re.findall(r'"([A-Z]+)"', methods_m.group(1)) if methods_m else ["ALL"]
    handler = _extract_handler(clean)

    return {
        "path": path,
        "methods": sorted(methods),
        "handler": handler,
        "commented": commented,
    }


def _extract_handler(line: str) -> str:
    """Extract handler function name from a route registration line.

    Inline anonymous functions are detected first so that middleware or
    helper calls inside the lambda body (h.SomeMethod) are not mistaken
    for the route's named handler.
    """
    # Inline anonymous function — must check before h.* scanning
    if "func(" in line or "func (" in line:
        return "<inline>"

    # Exported methods on Handler receiver (h.FuncName)
    refs = re.findall(r"h\.([A-Z]\w+)", line)
    actual = [r for r in refs if r not in MIDDLEWARE_NAMES]
    if actual:
        return actual[-1]

    # Any h.funcName (including unexported)
    refs = re.findall(r"h\.([A-Za-z]\w+)", line)
    actual = [r for r in refs if r not in MIDDLEWARE_NAMES]
    if actual:
        return actual[-1]

    return "<unknown>"


def _parse_echo_routes(content: str) -> List[Dict[str, Any]]:
    """Parse route registrations from an Echo-framework router file.

    Handles:
      - s.e.METHOD("/path", handler, middlewares...)
      - group.METHOD("/path", handler, middlewares...)
    where group is defined as  var = (...).Group("/prefix")

    echo.WrapHandler(http.HandlerFunc(s.h.Name)) → handler name "Name"
    s.h.Name or s.someHandler.Name              → handler name "Name"
    func( ...                                   → "<inline>"
    """
    # 1. Build group base-path map: variable name → absolute path prefix
    group_bases: Dict[str, str] = {}
    for m in re.finditer(
        r'(\w+)\s*:?=\s*(?:\w+\.)*(?:e|echo|s\.e)\.Group\("([^"]+)"\)',
        content,
    ):
        group_bases[m.group(1)] = m.group(2)

    routes: List[Dict[str, Any]] = []
    http_methods = {"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}

    # 2. Match method calls: (receiver).(METHOD)("path", handler_expr, ...)
    for m in re.finditer(
        r'(\w+|s\.e)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS|Any)\s*\(\s*"([^"]+)"'
        r'\s*,\s*([^)]+)',
        content,
    ):
        receiver, method_raw, path_suffix, rest = (
            m.group(1), m.group(2), m.group(3), m.group(4)
        )
        method = method_raw.upper() if method_raw != "Any" else "ALL"
        methods = [method] if method != "ALL" else ["ALL"]

        # Reconstruct full path
        base = group_bases.get(receiver, "")
        if receiver in ("s.e", "e"):
            base = ""  # direct on server — path_suffix is absolute
        full_path = (base.rstrip("/") + "/" + path_suffix.lstrip("/")).rstrip("/") or "/"

        # Extract handler name from rest of the call
        handler = _extract_echo_handler(rest)

        commented = False  # Echo router doesn't use commented routes

        routes.append({
            "path": full_path,
            "methods": sorted(methods) if methods != ["ALL"] else ["ALL"],
            "handler": handler,
            "commented": commented,
        })

    return routes


def _extract_echo_handler(expr: str) -> str:
    """Extract handler name from the second argument of an Echo route call."""
    expr = expr.strip()
    # Inline function
    if "func(" in expr or "func (" in expr:
        return "<inline>"
    # echo.WrapHandler(http.HandlerFunc(s.h.Name))
    m = re.search(r"http\.HandlerFunc\((?:\w+\.)*(\w+)\)", expr)
    if m:
        return m.group(1)
    # s.h.Name or s.academyHandler.Name or s.someHandler.Name
    m = re.search(r"s\.\w+\.([A-Z]\w+)", expr)
    if m:
        return m.group(1)
    # Plain method reference
    m = re.search(r"\.([A-Z]\w+)\b", expr)
    if m:
        return m.group(1)
    return "<unknown>"


# ---------------------------------------------------------------------------
# 2. OpenAPI parser — docs/data/openapi.yml
# ---------------------------------------------------------------------------

def _has_meaningful_schema(schema: Optional[dict]) -> bool:
    """Check if a schema object has real structure beyond a bare type."""
    if not schema or not isinstance(schema, dict):
        return False
    if "$ref" in schema:
        return True
    if any(k in schema for k in ("allOf", "oneOf", "anyOf")):
        return True
    if schema.get("type") == "array" and "items" in schema:
        return True
    if schema.get("type") == "object" and "properties" in schema:
        return True
    return False


def _get_content_schema(content: Any) -> Optional[dict]:
    """Extract schema from a content map (application/json, etc.)."""
    if not isinstance(content, dict):
        return None
    for _media_type, media_obj in content.items():
        if isinstance(media_obj, dict) and "schema" in media_obj:
            return media_obj["schema"]
    return None


def _describe_schema(schema: Optional[dict], label: str) -> List[str]:
    """Return human-readable findings about a schema object."""
    if not schema or not isinstance(schema, dict):
        return [f"{label}: no schema defined"]

    if "$ref" in schema:
        ref = schema["$ref"].rsplit("/", 1)[-1]
        return [f"{label}: references {ref}"]

    if any(k in schema for k in ("allOf", "oneOf", "anyOf")):
        combo = next(k for k in ("allOf", "oneOf", "anyOf") if k in schema)
        count = len(schema[combo]) if isinstance(schema[combo], list) else "?"
        return [f"{label}: {combo} with {count} sub-schemas"]

    s_type = schema.get("type", "untyped")

    if s_type == "array":
        items = schema.get("items", {})
        if isinstance(items, dict) and "$ref" in items:
            ref = items["$ref"].rsplit("/", 1)[-1]
            return [f"{label}: array of {ref}"]
        return [f"{label}: array (inline items)"]

    if s_type == "object":
        props = schema.get("properties", {})
        required = schema.get("required", [])
        if not props:
            return [f"{label}: object with no properties defined"]
        prop_names = sorted(props.keys())
        n = len(prop_names)
        preview = ", ".join(prop_names[:6])
        if n > 6:
            preview += f", ... ({n} total)"
        missing_desc = [
            k for k, v in props.items()
            if isinstance(v, dict) and not v.get("description")
        ]
        findings = [f"{label}: object with properties [{preview}]"]
        if not required:
            findings.append(f"{label}: no 'required' fields specified")
        if missing_desc:
            names = ", ".join(missing_desc[:5])
            if len(missing_desc) > 5:
                names += f", ... ({len(missing_desc)} total)"
            findings.append(
                f"{label}: properties missing description: {names}"
            )
        return findings

    # bare type (string, integer, boolean, etc.) with no structure
    return [f"{label}: bare type '{s_type}' with no properties or $ref"]


def _assess_completeness(operation: dict, method: str) -> Tuple[str, List[str]]:
    """Assess schema completeness for a single OpenAPI operation.

    Returns (completeness, detail_notes) where detail_notes lists every
    specific finding (missing fields, bare types, property gaps, etc.).
    """
    notes: List[str] = []
    expects_body = method.upper() in BODY_METHODS

    # --- Operation-level checks ---
    if not operation.get("operationId"):
        notes.append("missing operationId")
    if not operation.get("summary") and not operation.get("description"):
        notes.append("missing summary/description")

    # Parameters
    params = operation.get("parameters", [])
    if isinstance(params, list):
        no_desc = [
            p.get("name", "?") for p in params
            if isinstance(p, dict) and not p.get("description")
        ]
        if no_desc:
            notes.append(
                f"parameters missing description: {', '.join(no_desc[:5])}"
            )

    # --- Request side ---
    request_meaningful = False
    req_body = operation.get("requestBody", {})
    if isinstance(req_body, dict) and req_body:
        if "$ref" in req_body:
            request_meaningful = True
            ref = req_body["$ref"].rsplit("/", 1)[-1]
            notes.append(f"requestBody: references {ref}")
        else:
            req_content = req_body.get("content", {})
            req_schema = _get_content_schema(req_content)
            request_meaningful = _has_meaningful_schema(req_schema)
            notes.extend(_describe_schema(req_schema, "requestBody"))
    elif expects_body:
        notes.append("requestBody: not defined (method expects a body)")

    # --- Response side ---
    response_meaningful = False
    responses = operation.get("responses", {})
    defined_codes: Set[str] = set()

    if isinstance(responses, dict):
        for code, resp in responses.items():
            defined_codes.add(str(code))
            if not isinstance(resp, dict):
                continue

            if str(code).startswith("2"):
                if "$ref" in resp:
                    response_meaningful = True
                    ref = resp["$ref"].rsplit("/", 1)[-1]
                    notes.append(f"response {code}: references {ref}")
                else:
                    resp_content = resp.get("content", {})
                    resp_schema = _get_content_schema(resp_content)
                    if _has_meaningful_schema(resp_schema):
                        response_meaningful = True
                    notes.extend(
                        _describe_schema(resp_schema, f"response {code}")
                    )

    if not any(str(c).startswith("2") for c in defined_codes):
        notes.append("no 2xx success response defined")

    # Missing common error responses
    common_errors = {"400", "401", "404", "500"}
    missing_errors = sorted(common_errors - defined_codes)
    if missing_errors:
        notes.append(f"missing error responses: {', '.join(missing_errors)}")

    # --- Classify ---
    if expects_body:
        if request_meaningful and response_meaningful:
            return "Full", notes
        if request_meaningful or response_meaningful:
            return "Partial", notes
        return "Stub", notes
    else:
        # GET, DELETE, HEAD, OPTIONS — only response matters
        if response_meaningful:
            return "Full", notes
        return "Stub", notes


def _build_tag_category_map(doc: dict) -> Dict[str, Tuple[str, str]]:
    """Build a mapping from OpenAPI tag name to (category, subcategory).

    Uses x-tagGroups for the category (group name) and the tag's
    x-displayName for the subcategory.  Falls back to the raw tag name
    when x-displayName is absent.
    """
    # tag name → x-displayName
    tag_display: Dict[str, str] = {}
    for tag_def in doc.get("tags", []):
        if isinstance(tag_def, dict) and "name" in tag_def:
            display = tag_def.get("x-displayName", tag_def["name"])
            tag_display[tag_def["name"]] = display

    # tag name → (group_name, display_name)
    tag_to_category: Dict[str, Tuple[str, str]] = {}
    for group in doc.get("x-tagGroups", []):
        if not isinstance(group, dict):
            continue
        group_name = group.get("name", "Other")
        for tag_name in group.get("tags", []):
            display = tag_display.get(tag_name, tag_name)
            tag_to_category[tag_name] = (group_name, display)

    # Tags not listed in any group still get a category from their name
    for tag_name, display in tag_display.items():
        if tag_name not in tag_to_category:
            tag_to_category[tag_name] = (display, display)

    return tag_to_category


def parse_openapi(spec_file: Path) -> dict:
    """Parse the authoritative bundled OpenAPI spec.

    Returns a dict with:
      all_paths:        {norm_path: {METHOD, ...}}
      completeness:     {(norm_path, METHOD): "Full"/"Partial"/"Stub"}
      x_internal:       {(norm_path, METHOD): ["cloud"] or []}
      original_paths:   {norm_path: original_path}
      compl_notes:      {(norm_path, METHOD): [detail_notes]}
      path_categories:  {norm_path: (category, subcategory)}
      operations:       {(norm_path, METHOD): operation_dict}
    """
    empty = {
        "all_paths": {},
        "completeness": {},
        "x_internal": {},
        "original_paths": {},
        "compl_notes": {},
        "path_categories": {},
        "operations": {},
    }
    if not spec_file.exists():
        print(f"ERROR: {spec_file} not found", file=sys.stderr)
        return empty

    with open(spec_file, encoding="utf-8") as f:
        doc = yaml.safe_load(f)

    # Build tag → (category, subcategory) from x-tagGroups + tags
    tag_to_category = _build_tag_category_map(doc)

    all_paths: Dict[str, Set[str]] = {}
    completeness: Dict[Tuple[str, str], str] = {}
    x_internal: Dict[Tuple[str, str], List[str]] = {}
    original_paths: Dict[str, str] = {}
    compl_notes: Dict[Tuple[str, str], List[str]] = {}
    path_categories: Dict[str, Tuple[str, str]] = {}
    operations: Dict[Tuple[str, str], dict] = {}

    for path, methods_obj in doc.get("paths", {}).items():
        if not isinstance(methods_obj, dict):
            continue
        for method, details in methods_obj.items():
            if method.lower() not in HTTP_METHODS:
                continue
            if not isinstance(details, dict):
                continue

            norm = normalize_path(path)
            m_upper = method.upper()
            all_paths.setdefault(norm, set()).add(m_upper)

            # Track original path for spec-only endpoints
            if norm not in original_paths:
                original_paths[norm] = path

            # Derive category from the operation's first tag
            if norm not in path_categories:
                op_tags = details.get("tags", [])
                if isinstance(op_tags, list):
                    for tag_name in op_tags:
                        if tag_name in tag_to_category:
                            path_categories[norm] = tag_to_category[tag_name]
                            break

            # x-internal tag
            xi = details.get("x-internal", [])
            if not isinstance(xi, list):
                xi = [xi] if xi else []
            x_internal[(norm, m_upper)] = xi

            # Schema completeness (legacy — kept for fallback)
            comp, cnotes = _assess_completeness(details, method)
            completeness[(norm, m_upper)] = comp
            compl_notes[(norm, m_upper)] = cnotes

            # Store raw operation for cross-check
            operations[(norm, m_upper)] = details

    return {
        "all_paths": all_paths,
        "completeness": completeness,
        "x_internal": x_internal,
        "original_paths": original_paths,
        "compl_notes": compl_notes,
        "path_categories": path_categories,
        "operations": operations,
    }


# ---------------------------------------------------------------------------
# Go AST analyzer — subprocess bridge
# ---------------------------------------------------------------------------

def run_go_analyzer(
    repo: Path,
    config: Optional["RepoConfig"] = None,
) -> Dict[str, Any]:
    """Run the Go AST helper and return its parsed JSON output.

    The helper (build/scripts/analyze_handlers/main.go) uses go/ast to extract:
      - Per-handler schema import usage and request/response types
      - Transitive type aliases from local models to schema packages
      - JSON struct field names from handlers, models, and schemas models

    Falls back to an empty result (triggering regex fallback in main) when Go
    is not available or the helper fails.
    """
    schemas_root = Path(__file__).resolve().parents[2]
    schemas_models = schemas_root / "models"
    handlers_dir = config.handlers_dir if config else HANDLERS_DIR

    cmd = [
        "go", "run", "./build/scripts/analyze_handlers/",
        "--repo", str(repo),
        "--handlers-dir", handlers_dir,
        "--models-dir", "server/models",
        "--schema-module", "github.com/meshery/schemas",
    ]
    if schemas_models.exists():
        cmd += ["--schemas-models", str(schemas_models)]

    try:
        out = subprocess.check_output(
            cmd,
            cwd=str(schemas_root),
            text=True,
            stderr=subprocess.PIPE,
        )
        return json.loads(out)
    except FileNotFoundError:
        print(
            "WARNING: 'go' not found — Go AST analyzer unavailable; "
            "analysis will be empty — handler classification unavailable",
            file=sys.stderr,
        )
    except subprocess.CalledProcessError as exc:
        print(
            f"WARNING: Go analyzer exited with code {exc.returncode} — "
            "analysis will be empty — handler classification unavailable\n"
            f"  stderr: {exc.stderr.strip()[:200]}",
            file=sys.stderr,
        )
    except (json.JSONDecodeError, ValueError) as exc:
        print(
            f"WARNING: Go analyzer output could not be parsed ({exc}) — "
            "analysis will be empty — handler classification unavailable",
            file=sys.stderr,
        )
    return {"handlers": {}, "type_aliases": {}, "struct_fields": {}}


def _upgrade_schema_map(
    schema_map: Dict[str, Tuple[str, str]],
    handler_io_map: Dict[str, Dict[str, Optional[str]]],
    type_aliases: Dict[str, str],
    schema_module: str = "github.com/meshery/schemas",
) -> Dict[str, Tuple[str, str]]:
    """Upgrade FALSE entries in schema_map using transitive type alias lookups.

    When a handler's I/O type (request or response) resolves through a local
    alias to a schema type, it should be classified TRUE/Partial, not FALSE.
    This repairs the alias-blind gap in the direct-import check.

    Example:
        GetConnections → resp_type "*connections.ConnectionPage"
        type_aliases   → {"ConnectionPage": ".../models/v1beta1/connection"}
        Result         → ("TRUE", "alias: ConnectionPage → models/v1beta1/connection")
    """
    if not type_aliases or not handler_io_map:
        return schema_map

    result = dict(schema_map)
    for handler, (status, _reason) in result.items():
        if status != "FALSE":
            continue
        io = handler_io_map.get(handler, {})
        for t in [io.get("request_type"), io.get("response_type")]:
            if not t:
                continue
            bare = t.lstrip("*[]").rsplit(".", 1)[-1]
            imp = type_aliases.get(bare)
            if not imp:
                continue
            rel = imp.replace(schema_module + "/", "")
            if "models/v" in rel:
                result[handler] = ("TRUE", f"alias: {bare} → {rel}")
                break
            if "models/core" in rel:
                result[handler] = ("Partial", f"alias: {bare} → models/core")
                # don't break — a versioned import on the other type would win

    return result


# ---------------------------------------------------------------------------
# 4. Handler I/O Type Extractor
# ---------------------------------------------------------------------------


# ---------------------------------------------------------------------------
# 6. Spec Schema Field Extractor
# ---------------------------------------------------------------------------

def _collect_property_names(schema: Any) -> Set[str]:
    """Recursively collect property names from an OpenAPI schema."""
    if not isinstance(schema, dict):
        return set()

    props = set()

    if "properties" in schema and isinstance(schema["properties"], dict):
        props.update(schema["properties"].keys())

    # Walk allOf / oneOf / anyOf
    for combo in ("allOf", "oneOf", "anyOf"):
        if combo in schema and isinstance(schema[combo], list):
            for sub in schema[combo]:
                props.update(_collect_property_names(sub))

    # Array items
    if schema.get("type") == "array" and isinstance(
        schema.get("items"), dict
    ):
        props.update(_collect_property_names(schema["items"]))

    return props


def extract_spec_schema_fields(
    operation: dict, method: str
) -> Dict[str, Set[str]]:
    """Extract property name sets from an OpenAPI operation.

    Returns {"request_fields": set, "response_fields": set}.
    """
    req_fields: Set[str] = set()
    resp_fields: Set[str] = set()

    # --- Request body ---
    req_body = operation.get("requestBody", {})
    if isinstance(req_body, dict) and req_body:
        content = req_body.get("content", {})
        if isinstance(content, dict):
            for _mt, media_obj in content.items():
                if isinstance(media_obj, dict) and "schema" in media_obj:
                    req_fields = _collect_property_names(media_obj["schema"])
                    break

    # --- Response (first 2xx) ---
    responses = operation.get("responses", {})
    if isinstance(responses, dict):
        for code, resp in responses.items():
            if not str(code).startswith("2") or not isinstance(resp, dict):
                continue
            content = resp.get("content", {})
            if isinstance(content, dict):
                for _mt, media_obj in content.items():
                    if isinstance(media_obj, dict) and "schema" in media_obj:
                        resp_fields = _collect_property_names(
                            media_obj["schema"]
                        )
                        break
            break  # use first 2xx only

    return {"request_fields": req_fields, "response_fields": resp_fields}


# ---------------------------------------------------------------------------
# 7. Cross-Check Completeness
# ---------------------------------------------------------------------------

def _field_overlap(
    go_fields: Optional[Set[str]], spec_fields: Set[str]
) -> Tuple[Optional[float], Set[str], Set[str], Set[str]]:
    """Compute overlap between Go struct fields and spec properties.

    Returns (ratio_or_None, common, only_go, only_spec).
    """
    if go_fields is None or not spec_fields or not go_fields:
        return None, set(), set(), set()

    common = go_fields & spec_fields
    only_go = go_fields - spec_fields
    only_spec = spec_fields - go_fields
    ratio = len(common) / max(len(go_fields), len(spec_fields))
    return ratio, common, only_go, only_spec


def _format_field_set(fields: Set[str], limit: int = 6) -> str:
    """Format a set of field names as a compact string."""
    if not fields:
        return ""
    items = sorted(fields)
    preview = ", ".join(items[:limit])
    if len(items) > limit:
        preview += f", ... ({len(items)} total)"
    return preview


def cross_check_completeness(
    handler_name: str,
    handler_io: Dict[str, Optional[str]],
    go_fields_map: Dict[str, Set[str]],
    spec_fields: Dict[str, Set[str]],
    method: str,
) -> Tuple[str, List[str]]:
    """Cross-check handler's Go types against spec schemas.

    Returns (completeness, structured_notes) where structured_notes is a
    list of lines with clear section markers for later formatting.

    Lines are prefixed with markers:
      [REQ]  — request-side finding
      [RESP] — response-side finding
      [INFO] — general info
    """
    notes: List[str] = []
    expects_body = method.upper() in BODY_METHODS

    req_type = handler_io.get("request_type")
    resp_type = handler_io.get("response_type")
    is_resp_passthrough = resp_type == _PROVIDER_BYTES_SENTINEL

    # Strip pointer/slice prefix for type lookup
    def _bare_type(t: Optional[str]) -> Optional[str]:
        if t is None or t == _PROVIDER_BYTES_SENTINEL:
            return None
        return t.lstrip("*[]").split(".")[-1]

    req_type_short = _bare_type(req_type)
    resp_type_short = _bare_type(resp_type)

    req_go_fields = (
        go_fields_map.get(req_type_short) if req_type_short else None
    )
    resp_go_fields = (
        go_fields_map.get(resp_type_short) if resp_type_short else None
    )

    spec_req = spec_fields.get("request_fields", set())
    spec_resp = spec_fields.get("response_fields", set())
    has_spec = bool(spec_req or spec_resp)

    if not has_spec:
        return "Not-audited", ["[INFO] No spec schema to compare against"]

    # --- Request side ---
    req_ratio, req_common, req_only_go, req_only_spec = _field_overlap(
        req_go_fields, spec_req
    )

    if expects_body:
        if req_type:
            notes.append(f"[REQ] Handler type: {req_type}")
            if req_ratio is not None:
                pct = int(req_ratio * 100)
                notes.append(
                    f"[REQ] Field match: {pct}% "
                    f"({len(req_common)}/{max(len(req_go_fields or set()), len(spec_req))})"
                )
                if req_common:
                    notes.append(
                        f"[REQ] Matching: {_format_field_set(req_common)}"
                    )
                if req_only_go:
                    notes.append(
                        f"[REQ] In handler only: "
                        f"{_format_field_set(req_only_go, 5)}"
                    )
                if req_only_spec:
                    notes.append(
                        f"[REQ] In spec only: "
                        f"{_format_field_set(req_only_spec, 5)}"
                    )
            else:
                notes.append(
                    "[REQ] Struct fields not found for comparison"
                )
        else:
            notes.append("[REQ] Could not extract request type from handler")

    # --- Response side ---
    resp_ratio, resp_common, resp_only_go, resp_only_spec = _field_overlap(
        resp_go_fields, spec_resp
    )

    if is_resp_passthrough:
        notes.append(
            "[RESP] Provider returns raw []byte — "
            "response type is opaque, cannot cross-check"
        )
    elif resp_type:
        notes.append(f"[RESP] Handler type: {resp_type}")
        if resp_ratio is not None:
            pct = int(resp_ratio * 100)
            notes.append(
                f"[RESP] Field match: {pct}% "
                f"({len(resp_common)}/{max(len(resp_go_fields or set()), len(spec_resp))})"
            )
            if resp_common:
                notes.append(
                    f"[RESP] Matching: {_format_field_set(resp_common)}"
                )
            if resp_only_go:
                notes.append(
                    f"[RESP] In handler only: "
                    f"{_format_field_set(resp_only_go, 5)}"
                )
            if resp_only_spec:
                notes.append(
                    f"[RESP] In spec only: "
                    f"{_format_field_set(resp_only_spec, 5)}"
                )
        else:
            notes.append(
                "[RESP] Struct fields not found for comparison"
            )
    else:
        if spec_resp:
            notes.append(
                "[RESP] Could not extract response type from handler"
            )
        else:
            notes.append("[RESP] No response body in spec (expected)")

    # --- Classify ---
    GOOD_THRESHOLD = 0.70

    if expects_body:
        req_good = req_ratio is not None and req_ratio >= GOOD_THRESHOLD
        resp_good = resp_ratio is not None and resp_ratio >= GOOD_THRESHOLD
        req_any = req_ratio is not None and req_ratio > 0
        resp_any = resp_ratio is not None and resp_ratio > 0

        if req_ratio is None and resp_ratio is None:
            if req_type or resp_type:
                return "Stub", notes
            return "Not-audited", notes

        if req_good and resp_good:
            return "Full", notes
        if req_good or resp_good or (req_any and resp_any):
            return "Partial", notes
        return "Stub", notes
    else:
        if resp_ratio is None:
            if is_resp_passthrough or (resp_type and resp_type != _PROVIDER_BYTES_SENTINEL):
                return "Stub", notes
            return "Not-audited", notes

        if resp_ratio >= GOOD_THRESHOLD:
            return "Full", notes
        if resp_ratio > 0:
            return "Partial", notes
        return "Stub", notes


# ---------------------------------------------------------------------------
# 8. Actionable Notes Builder
# ---------------------------------------------------------------------------

def _build_actionable_notes(
    *,
    coverage: str,
    status: str,
    is_commented: bool,
    compl_notes: List[str],
    completeness: str = "",
    driven: str = "",
    repo_source: str = "",
) -> str:
    """Build a structured, actionable summary for the Notes column.

    Only includes information that cannot be derived from other columns.
    Columns already convey: x-annotated scope, backed status, completeness
    level, and driven status — so notes focus on *specific gaps*.

    Sections (only included when relevant):
      ACTION                  — what to do (add spec, implement handler, remove route,
                                migrate handler to use meshery/schemas types)
      Completeness - Meshery Server/Cloud    — field-level gap details (omitted when Full)
    """
    sections: List[str] = []

    _PLATFORM_LABEL: Dict[str, str] = {
        "meshery": " - Meshery Server",
        "cloud": " - Meshery Cloud",
    }
    _plat_label = _PLATFORM_LABEL.get(repo_source, "")

    # ── ACTIONABLE ITEMS ──────────────────────────────────────────────
    if is_commented:
        sections.append(
            "[ACTION] Route is commented out — "
            "consider removal from router and spec"
        )

    if coverage == "Server Underlap":
        sections.append(
            "[ACTION] Not in OpenAPI spec — add spec definition"
        )
    elif coverage == "Schema Underlap":
        if status == "Unimplemented":
            sections.append(
                "[ACTION] In spec but no server route — "
                "implement handler or remove from spec"
            )

    if driven == "FALSE" and coverage == "Overlap":
        sections.append(
            f"[ACTION{_plat_label}] Not schema-driven — "
            "handler does not import meshery/schemas types"
        )

    # ── COMPLETENESS DETAILS (field-level + structural gaps) ─────────
    # Skip when completeness is Full — the column already says so; no gaps to list.
    if completeness == "Full":
        return "\n\n".join(sections) if sections else ""

    req_lines = [n for n in compl_notes if n.startswith("[REQ]")]
    resp_lines = [n for n in compl_notes if n.startswith("[RESP]")]
    info_lines = [n for n in compl_notes if n.startswith("[INFO]")]
    legacy_lines = [
        n for n in compl_notes
        if not n.startswith(("[REQ]", "[RESP]", "[INFO]"))
    ]

    compl_parts: List[str] = []
    if info_lines:
        for line in info_lines:
            compl_parts.append(line.replace("[INFO] ", ""))
    if req_lines:
        compl_parts.append("Request:")
        for line in req_lines:
            compl_parts.append("  " + line.replace("[REQ] ", ""))
    if resp_lines:
        compl_parts.append("Response:")
        for line in resp_lines:
            compl_parts.append("  " + line.replace("[RESP] ", ""))
    if legacy_lines:
        compl_parts.extend(legacy_lines)

    if compl_parts:
        sections.append(
            f"[Completeness{_plat_label}]\n" + "\n".join(compl_parts)
        )

    return "\n\n".join(sections) if sections else ""


def _derive_sheet_status_and_annotation(
    status: str,
    x_annotated: str,
    repo_source: str = "",
) -> Tuple[str, str]:
    """Split combined internal status into sheet-friendly fields.

    *repo_source* ("meshery" | "cloud") is appended as a suffix so the
    Endpoint Status column indicates where the route lives:
      Active - Meshery Server
      Active - Meshery Cloud
      Unimplemented - Meshery Server
      Unimplemented - Meshery Cloud
    Omitting repo_source produces the legacy bare labels.
    """
    if status == "Deprecated":
        if repo_source == "meshery":
            sheet_status = "Deprecated - Meshery Server"
        elif repo_source == "cloud":
            sheet_status = "Deprecated - Meshery Cloud"
        else:
            sheet_status = "Deprecated - Both"
    elif status in {"Unimplemented", "Cloud-only"}:
        if x_annotated == "Cloud-only":
            sheet_status = "Unimplemented - Meshery Cloud"
        elif x_annotated == "Meshery":
            sheet_status = "Unimplemented - Meshery Server"
        else:
            sheet_status = "Unimplemented - Both"
    else:
        if repo_source == "meshery":
            sheet_status = "Active - Meshery Server"
        elif repo_source == "cloud":
            sheet_status = "Active - Meshery Cloud"
        else:
            sheet_status = "Active - Both"

    return sheet_status, x_annotated


# ---------------------------------------------------------------------------
# 5. Classification — bidirectional walk (Router ∪ Spec)
# ---------------------------------------------------------------------------

def _dedup_notes(notes: List[str]) -> List[str]:
    """Return notes with duplicates removed, preserving insertion order."""
    seen: Set[str] = set()
    result = []
    for n in notes:
        if n not in seen:
            seen.add(n)
            result.append(n)
    return result


def _reduce_completeness(
    method_comps: List[str], notes: List[str]
) -> Tuple[str, List[str]]:
    """Reduce a list of per-method completeness values to a single status.

    Shared by the structural fallback path and the cross-check path so
    that both produce identical aggregation semantics.
    """
    unique = _dedup_notes(notes)
    if all(c == "Full" for c in method_comps):
        return "Full", unique
    if any(c == "Full" for c in method_comps) or any(
        c == "Partial" for c in method_comps
    ):
        return "Partial", unique
    if all(c == "Not-audited" for c in method_comps):
        return "Not-audited", unique
    return "Stub", unique


def _sheet_completeness_value(value: str) -> str:
    """Map internal completeness to sheet value."""
    if value in ("N/A", "No Schema"):
        return "No Schema"
    return value


def _sheet_driven_value(value: str) -> str:
    """Map internal driven value to sheet value."""
    if value in ("N/A", "No Schema"):
        return "FALSE"
    return value


def _aggregate_completeness(
    norm: str,
    methods: List[str],
    spec_data: dict,
) -> Tuple[str, List[str]]:
    """Aggregate structural spec completeness across methods for one endpoint."""
    comp_map = spec_data["completeness"]
    cnotes_map = spec_data["compl_notes"]
    all_paths = spec_data["all_paths"]
    spec_methods = all_paths.get(norm, set())

    if not spec_methods:
        return "No Schema", []

    check_methods = spec_methods if methods == ["ALL"] else [
        m for m in methods if m in spec_methods
    ]

    method_comps: List[str] = []
    agg_notes: List[str] = []
    for m in check_methods:
        method_comps.append(comp_map.get((norm, m), "Stub"))
        agg_notes.extend(cnotes_map.get((norm, m), []))

    if not method_comps:
        return "Stub", ["spec path exists but no method match"]

    return _reduce_completeness(method_comps, agg_notes)


def classify_endpoints(
    routes: List[Dict[str, Any]],
    spec_data: dict,
    schema_map: Dict[str, Tuple[str, str]],
    handler_io_map: Optional[Dict[str, Dict[str, Optional[str]]]] = None,
    go_fields_map: Optional[Dict[str, Set[str]]] = None,
    repo_source: str = "",
) -> List[Dict[str, Any]]:
    """Classify endpoints from both router and spec (bidirectional walk).

    When *handler_io_map* and *go_fields_map* are provided, schema
    completeness is determined by cross-checking handler Go types against
    spec schemas (field-level comparison).  Otherwise falls back to the
    legacy structural assessment.
    """
    all_paths = spec_data["all_paths"]
    x_internal_map = spec_data["x_internal"]
    original_paths = spec_data["original_paths"]
    path_categories = spec_data.get("path_categories", {})
    operations_map = spec_data.get("operations", {})

    endpoints: List[Dict[str, Any]] = []
    router_norm_paths: Set[str] = set()

    # ------------------------------------------------------------------
    # Pass 1: Router-sourced endpoints
    # ------------------------------------------------------------------
    grouped_routes: Dict[Tuple[str, str], List[Dict[str, Any]]] = defaultdict(list)
    for route in routes:
        methods_str = ", ".join(route["methods"])
        grouped_routes[(route["path"], methods_str)].append(route)

    for path, methods_str in sorted(grouped_routes):
        route_group = grouped_routes[(path, methods_str)]
        route = next((r for r in route_group if not r["commented"]), route_group[0])
        methods = route["methods"]
        is_commented = all(r["commented"] for r in route_group)

        norm = normalize_path(path)
        category, subcategory = categorize(path, path_categories)
        router_norm_paths.add(norm)

        spec_methods = all_paths.get(norm, set())

        # --- Coverage ---
        coverage = "Overlap" if spec_methods else "Server Underlap"

        # --- Cloud methods (needed for Status and Notes) ---
        cloud_methods: List[str] = []
        meshery_methods: List[str] = []
        if coverage != "Server Underlap":
            check_m = (
                sorted(spec_methods)
                if methods == ["ALL"]
                else [m for m in methods if m in spec_methods]
            )
            for m in check_m:
                xi = x_internal_map.get((norm, m), [])
                if "cloud" in xi:
                    cloud_methods.append(m)
                if "meshery" in xi:
                    meshery_methods.append(m)

        # --- Status ---
        if is_commented:
            status = "Deprecated"
        elif cloud_methods and len(cloud_methods) == len(spec_methods):
            status = "Active (Cloud-annotated)"
        else:
            status = "Active"

        if cloud_methods:
            x_annotated = "Cloud-only"
        elif meshery_methods:
            x_annotated = "Meshery"
        elif coverage == "Server Underlap":
            # Not in spec at all — x-internal annotation is inapplicable
            x_annotated = "No Schema"
        else:
            # In spec but no x-internal annotation (shared / no restriction)
            x_annotated = "None"

        sheet_status, sheet_x_annotated = _derive_sheet_status_and_annotation(
            status,
            x_annotated,
            repo_source=repo_source,
        )

        # --- Per-platform method filtering ---
        # ms_methods_list: methods Meshery Server owns (excludes x-internal: cloud)
        # mc_methods_list: methods Meshery Cloud owns (excludes x-internal: meshery)
        _is_cloud = repo_source == "cloud"
        if coverage == "Server Underlap":
            ms_methods_list: List[str] = []
            mc_methods_list: List[str] = []
        else:
            all_check = (
                sorted(spec_methods)
                if methods == ["ALL"]
                else [m for m in methods if m in spec_methods]
            )
            ms_methods_list = [
                m for m in all_check
                if "cloud" not in x_internal_map.get((norm, m), [])
            ]
            mc_methods_list = [
                m for m in all_check
                if "meshery" not in x_internal_map.get((norm, m), [])
            ]

        # Resolve which list belongs to the source platform vs the other
        this_methods = mc_methods_list if _is_cloud else ms_methods_list
        other_methods = ms_methods_list if _is_cloud else mc_methods_list

        # --- Schema-Backed (per platform) ---
        # No Schema = not in spec at all; FALSE = spec has path but methods
        # are owned by the other platform; TRUE = platform has relevant methods
        if coverage == "Server Underlap":
            backed_this = "No Schema"
            backed_other = "No Schema"
        else:
            backed_this = "TRUE" if this_methods else "FALSE"
            backed_other = "TRUE" if other_methods else "FALSE"

        # --- Schema Completeness (cross-check or legacy fallback) ---
        handler = route["handler"]
        use_crosscheck = (
            handler_io_map is not None
            and bool(go_fields_map)
            and handler in handler_io_map
            and spec_methods
        )

        # -- Source platform's completeness (cross-check when available) --
        if backed_this in ("FALSE", "No Schema"):
            completeness_this = "No Schema"
            compl_notes_this: List[str] = []
        elif use_crosscheck and this_methods:
            method_comps: List[str] = []
            agg_notes: List[str] = []
            for m in this_methods:
                op = operations_map.get((norm, m))
                if op:
                    sf = extract_spec_schema_fields(op, m)
                    comp, cnotes = cross_check_completeness(
                        handler, handler_io_map[handler],
                        go_fields_map, sf, m,
                    )
                    method_comps.append(comp)
                    agg_notes.extend(cnotes)
                else:
                    method_comps.append("Not-audited")
            completeness_this, compl_notes_this = _reduce_completeness(
                method_comps, agg_notes
            )
        elif this_methods:
            completeness_this, compl_notes_this = _aggregate_completeness(
                norm, this_methods, spec_data
            )
        else:
            completeness_this = "No Schema"
            compl_notes_this = []
        completeness_this = _sheet_completeness_value(completeness_this)

        # -- Other platform's completeness (structural only, no cross-check) --
        if backed_other in ("FALSE", "No Schema"):
            completeness_other = "No Schema"
            compl_notes_other: List[str] = []
        elif other_methods:
            completeness_other, compl_notes_other = _aggregate_completeness(
                norm, other_methods, spec_data
            )
            completeness_other = _sheet_completeness_value(completeness_other)
        else:
            completeness_other = "No Schema"
            compl_notes_other = []

        # --- Schema-Driven ---
        if handler in ("<inline>", "<unknown>"):
            driven, driven_reason = "FALSE", f"handler: {handler}"
        else:
            driven, driven_reason = schema_map.get(
                handler, ("FALSE", "handler not mapped")
            )
        driven = _sheet_driven_value(driven)

        # --- Notes (actionable summary) ---
        notes = _build_actionable_notes(
            coverage=coverage,
            status=status,
            is_commented=is_commented,
            compl_notes=compl_notes_this,
            completeness=completeness_this,
            driven=driven,
            repo_source=repo_source,
        )

        # Assign to correct platform columns
        if _is_cloud:
            backed_ms = backed_other
            backed_mc = backed_this
            completeness_ms = completeness_other
            completeness_mc = completeness_this
            driven_ms = "FALSE"
            driven_mc = driven
        else:
            backed_ms = backed_this
            backed_mc = backed_other
            completeness_ms = completeness_this
            completeness_mc = completeness_other
            driven_ms = driven
            driven_mc = "FALSE"

        endpoints.append({
            "category": category,
            "subcategory": subcategory,
            "path": path,
            "methods": methods_str,
            "coverage": coverage,
            "status": status,
            "sheet_status": sheet_status,
            "x_annotated": sheet_x_annotated,
            "backed_ms": backed_ms,
            "backed_mc": backed_mc,
            "completeness_ms": completeness_ms,
            "completeness_mc": completeness_mc,
            "driven_ms": driven_ms,
            "driven_mc": driven_mc,
            "notes": notes,
            "repo_source": repo_source,
            "meshery_present": repo_source == "meshery",
            "cloud_present": repo_source == "cloud",
        })

    # ------------------------------------------------------------------
    # Pass 2: Spec-only endpoints (Schema Underlap)
    # ------------------------------------------------------------------
    for norm_path, spec_methods in sorted(all_paths.items()):
        if norm_path in router_norm_paths:
            continue

        original = original_paths.get(norm_path, norm_path)
        methods_sorted = sorted(spec_methods)
        category, subcategory = categorize(original, path_categories)

        # Determine x-internal across all methods for this path
        all_cloud = True
        any_cloud = False
        any_meshery = False
        for m in methods_sorted:
            xi = x_internal_map.get((norm_path, m), [])
            if "cloud" in xi:
                any_cloud = True
            else:
                all_cloud = False
            if "meshery" in xi:
                any_meshery = True

        # --- Coverage ---
        coverage = "Schema Underlap"

        # --- Status ---
        if all_cloud:
            status = "Cloud-only"
        else:
            status = "Unimplemented"

        if any_cloud:
            x_annotated = "Cloud-only"
        elif any_meshery:
            x_annotated = "Meshery"
        else:
            # In spec but no x-internal annotation (shared / no restriction)
            x_annotated = "None"

        # Spec-only endpoints have no router source; omit suffix so the status
        # reads "Unimplemented" rather than "Unimplemented - Meshery Server".
        sheet_status, sheet_x_annotated = _derive_sheet_status_and_annotation(
            status,
            x_annotated,
        )

        # --- Per-platform method filtering ---
        ms_methods = [
            m for m in methods_sorted
            if "cloud" not in x_internal_map.get((norm_path, m), [])
        ]
        mc_methods = [
            m for m in methods_sorted
            if "meshery" not in x_internal_map.get((norm_path, m), [])
        ]

        # --- Schema-Backed (per platform) ---
        # TRUE  = platform owns methods in the spec for this path
        # FALSE = spec has the path but all methods are owned by the other platform
        # No Schema = should not occur in Pass 2 (path is always in spec)
        if x_annotated == "Cloud-only":
            backed_ms = "FALSE"
            backed_mc = "TRUE"
        elif x_annotated == "Meshery":
            backed_ms = "TRUE"
            backed_mc = "FALSE"
        else:
            # shared (None) — both platforms own it
            backed_ms = "TRUE"
            backed_mc = "TRUE"

        # --- Schema Completeness (per platform, independent) ---
        compl_notes_ms: List[str] = []
        compl_notes_mc: List[str] = []

        if backed_ms in ("FALSE", "No Schema"):
            completeness_ms = "No Schema"
        elif ms_methods:
            completeness_ms, compl_notes_ms = _aggregate_completeness(
                norm_path, ms_methods, spec_data
            )
            completeness_ms = _sheet_completeness_value(completeness_ms)
        else:
            completeness_ms = "No Schema"

        if backed_mc in ("FALSE", "No Schema"):
            completeness_mc = "No Schema"
        elif mc_methods:
            completeness_mc, compl_notes_mc = _aggregate_completeness(
                norm_path, mc_methods, spec_data
            )
            completeness_mc = _sheet_completeness_value(completeness_mc)
        else:
            completeness_mc = "No Schema"

        # --- Notes (per-platform gaps, actionable only) ---
        note_sections: List[str] = []

        if status == "Unimplemented":
            note_sections.append(
                "[ACTION] In spec but no server route — "
                "implement handler or remove from spec"
            )

        if compl_notes_ms and completeness_ms != "Full":
            note_sections.append(
                "[Completeness - Meshery Server]\n" + "\n".join(compl_notes_ms)
            )
        if compl_notes_mc and completeness_mc != "Full":
            note_sections.append(
                "[Completeness - Meshery Cloud]\n" + "\n".join(compl_notes_mc)
            )

        notes = "\n\n".join(note_sections) if note_sections else ""

        endpoints.append({
            "category": category,
            "subcategory": subcategory,
            "path": original,
            "methods": ", ".join(methods_sorted),
            "coverage": coverage,
            "status": status,
            "sheet_status": sheet_status,
            "x_annotated": sheet_x_annotated,
            "backed_ms": backed_ms,
            "backed_mc": backed_mc,
            "completeness_ms": completeness_ms,
            "completeness_mc": completeness_mc,
            "driven_ms": "FALSE",
            "driven_mc": "FALSE",
            "notes": notes,
            "repo_source": "",  # spec-only — no router source
            "meshery_present": False,
            "cloud_present": False,
        })

    return sorted(endpoints, key=endpoint_sort_key)


# ---------------------------------------------------------------------------
# Per-repo analysis helper
# ---------------------------------------------------------------------------

def _setup_repo_analysis(
    repo_root: Path,
    repo_config: "RepoConfig",
) -> Tuple[
    List[Dict[str, Any]],
    Dict[str, Tuple[str, str]],
    Dict[str, Dict[str, Optional[str]]],
    Dict[str, Set[str]],
    Dict[str, int],
]:
    """Parse routes and run handler analysis for one repo.

    Returns (routes, schema_map, handler_io_map, go_fields_map).
    """
    routes = parse_router(repo_root, config=repo_config)
    analysis = run_go_analyzer(repo_root, config=repo_config)

    go_fields_map: Dict[str, Set[str]] = {
        name: set(fields) for name, fields in analysis["struct_fields"].items()
    }
    handler_io_map: Dict[str, Dict[str, Optional[str]]] = {
        name: {
            "request_type": info["request_type"],
            "response_type": info["response_type"],
        }
        for name, info in analysis["handlers"].items()
    }

    schema_map_direct: Dict[str, Tuple[str, str]] = {
        name: (info["schema_import_usage"], info["schema_reason"])
        for name, info in analysis["handlers"].items()
    }

    schema_map = _upgrade_schema_map(
        schema_map_direct,
        handler_io_map,
        analysis.get("type_aliases", {}),
    )

    n_driven = sum(1 for s, _ in schema_map.values() if s == "TRUE")
    n_io = sum(
        1 for io in handler_io_map.values()
        if io.get("request_type") or io.get("response_type")
    )
    analysis_stats = {
        "handlers": len(analysis["handlers"]),
        "struct_types": len(analysis["struct_fields"]),
        "schema_aliases": len(analysis["type_aliases"]),
        "extractable_io_handlers": n_io,
        "direct_schema_imports": n_driven,
    }

    return routes, schema_map, handler_io_map, go_fields_map, analysis_stats


# ---------------------------------------------------------------------------
# Combined endpoint merger (used when both repos are analysed in one run)
# ---------------------------------------------------------------------------

_COV_RANK: Dict[str, int] = {"Overlap": 3, "Server Underlap": 2, "Schema Underlap": 1}


def merge_endpoint_lists(
    meshery_eps: List[Dict[str, Any]],
    cloud_eps: List[Dict[str, Any]],
) -> List[Dict[str, Any]]:
    """Produce a single unified endpoint list from meshery + cloud analysis.

    - Endpoints unique to one repo keep their "Active - Meshery Server" /
      "Active - Meshery Cloud" sheet_status as set by classify_endpoints.
    - Endpoints present in BOTH repos are merged into one row; the
      sheet_status is upgraded to "Active - Both" when both are active.
    - Spec-only endpoints that appear in both spec-only passes are
      deduplicated (same path, keep one).
    """
    meshery_by_norm: Dict[str, Dict[str, Any]] = {}
    for ep in meshery_eps:
        norm = normalize_path(ep["path"])
        # Prefer router-backed (Pass 1) over spec-only (Pass 2)
        if norm not in meshery_by_norm or (
            _COV_RANK.get(ep["coverage"], 0) > _COV_RANK.get(meshery_by_norm[norm]["coverage"], 0)
        ):
            meshery_by_norm[norm] = ep

    cloud_by_norm: Dict[str, Dict[str, Any]] = {}
    for ep in cloud_eps:
        norm = normalize_path(ep["path"])
        if norm not in cloud_by_norm or (
            _COV_RANK.get(ep["coverage"], 0) > _COV_RANK.get(cloud_by_norm[norm]["coverage"], 0)
        ):
            cloud_by_norm[norm] = ep

    merged: List[Dict[str, Any]] = []

    for norm in sorted(set(meshery_by_norm) | set(cloud_by_norm)):
        m_ep = meshery_by_norm.get(norm)
        c_ep = cloud_by_norm.get(norm)

        if m_ep and not c_ep:
            merged.append(m_ep)
            continue
        if c_ep and not m_ep:
            merged.append(c_ep)
            continue

        # Both repos have this path — merge.
        # Primary = the one with higher coverage rank (router-backed wins over spec-only).
        if _COV_RANK.get(m_ep["coverage"], 0) >= _COV_RANK.get(c_ep["coverage"], 0):
            primary, secondary = m_ep, c_ep
        else:
            primary, secondary = c_ep, m_ep

        ep = dict(primary)

        # Combine methods from both repos
        m_methods = {m.strip() for m in m_ep["methods"].split(",") if m.strip()}
        c_methods = {m.strip() for m in c_ep["methods"].split(",") if m.strip()}
        ep["methods"] = ", ".join(sorted(m_methods | c_methods))

        # Combined sheet_status — reflect which repos are active
        m_active = m_ep["status"] in {"Active", "Active (Cloud-annotated)"}
        c_active = c_ep["status"] in {"Active", "Active (Cloud-annotated)"}

        if m_active and c_active:
            ep["sheet_status"] = "Active - Both"
        elif m_active:
            ep["sheet_status"] = "Active - Meshery Server"
        elif c_active:
            ep["sheet_status"] = "Active - Meshery Cloud"
        elif m_ep["status"] == "Deprecated" or c_ep["status"] == "Deprecated":
            ep["sheet_status"] = "Deprecated - Both"
        else:
            ep["sheet_status"] = "Unimplemented - Both"

        # Per-platform columns: prefer the platform-specific value, fall back
        # to the other only when the primary platform has "No Schema".
        def _prefer(a: str, b: str) -> str:
            return a if a != "No Schema" else b

        ep["backed_ms"] = _prefer(m_ep["backed_ms"], c_ep["backed_ms"])
        ep["backed_mc"] = _prefer(c_ep["backed_mc"], m_ep["backed_mc"])
        ep["completeness_ms"] = _prefer(m_ep["completeness_ms"], c_ep["completeness_ms"])
        ep["completeness_mc"] = _prefer(c_ep["completeness_mc"], m_ep["completeness_mc"])
        ep["driven_ms"] = _prefer(m_ep["driven_ms"], c_ep["driven_ms"])
        ep["driven_mc"] = _prefer(c_ep["driven_mc"], m_ep["driven_mc"])
        ep["meshery_present"] = bool(m_ep.get("meshery_present"))
        ep["cloud_present"] = bool(c_ep.get("cloud_present"))

        # Combine notes from both repos, keeping platforms separate.
        # When both entries are spec-only (no router in either repo), they
        # produce identical notes — avoid double-wrapping by using one copy.
        m_notes = m_ep.get("notes", "")
        c_notes = c_ep.get("notes", "")
        both_spec_only = (
            m_ep.get("repo_source", "") == ""
            and c_ep.get("repo_source", "") == ""
        )
        if both_spec_only:
            ep["notes"] = m_notes or c_notes
        elif m_notes and c_notes:
            ep["notes"] = f"[Meshery Server]\n{m_notes}\n\n[Meshery Cloud]\n{c_notes}"
        elif m_notes:
            ep["notes"] = m_notes
        elif c_notes:
            ep["notes"] = c_notes

        ep["repo_source"] = "both"
        merged.append(ep)

    return sorted(merged, key=endpoint_sort_key)


# ---------------------------------------------------------------------------
# Google Sheet — credentials from environment only
# ---------------------------------------------------------------------------

_GOOGLE_SCOPES = [
    "https://www.googleapis.com/auth/spreadsheets",
    "https://www.googleapis.com/auth/drive",
]


def _load_google_service_account_creds():
    """Load Google service account credentials from environment variables.

    Checks GOOGLE_CREDENTIALS_JSON (inline JSON, for CI) first, then
    GOOGLE_APPLICATION_CREDENTIALS (file path, for local dev).
    Returns a Credentials object, or None if neither is set.
    """
    try:
        from google.oauth2.service_account import Credentials
    except ImportError:
        sys.exit("Missing packages. Run: pip install google-auth")

    creds_json = os.environ.get("GOOGLE_CREDENTIALS_JSON")
    if creds_json:
        return Credentials.from_service_account_info(
            json.loads(creds_json), scopes=_GOOGLE_SCOPES
        )

    creds_file = os.environ.get("GOOGLE_APPLICATION_CREDENTIALS")
    if creds_file and os.path.exists(creds_file):
        return Credentials.from_service_account_file(
            creds_file, scopes=_GOOGLE_SCOPES
        )

    return None


def _get_sheet_client():
    """Authenticate with Google Sheets using env-var credentials."""
    try:
        import gspread
    except ImportError:
        sys.exit("Missing packages. Run: pip install gspread google-auth")

    creds = _load_google_service_account_creds()
    if creds is None:
        return None
    return gspread.authorize(creds)


def _has_sheet_credentials_configured() -> bool:
    """Return True when sheet credentials are configured via env vars."""
    if os.environ.get("GOOGLE_CREDENTIALS_JSON"):
        return True
    creds_file = os.environ.get("GOOGLE_APPLICATION_CREDENTIALS")
    return bool(creds_file and os.path.exists(creds_file))


def _col_letter(idx: int) -> str:
    """Convert 0-based column index to sheet column letter (A, B, ... Z, AA, ...)."""
    result = ""
    while True:
        result = chr(65 + idx % 26) + result
        idx = idx // 26 - 1
        if idx < 0:
            break
    return result


MAGENTA_TEXT_RGB = {"red": 1.0, "green": 0.0, "blue": 1.0}
BLACK_TEXT_RGB = {"red": 0.0, "green": 0.0, "blue": 0.0}


def _build_sheet_index(current_rows: List[List[str]]) -> Dict[str, List[Tuple[int, Set[str]]]]:
    """Index worksheet rows by normalized endpoint path and methods."""
    sheet_index: Dict[str, List[Tuple[int, Set[str]]]] = defaultdict(list)
    for idx, row in enumerate(current_rows):
        if idx == 0:
            continue
        ep = row[COL_ENDPOINTS].strip() if len(row) > COL_ENDPOINTS else ""
        if not ep:
            continue
        if not ep.startswith("/"):
            ep = "/" + ep
        norm = normalize_path(ep)
        raw_methods = row[COL_METHODS].strip() if len(row) > COL_METHODS else ""
        mset = {
            m.strip().upper()
            for m in raw_methods.replace(";", ",").split(",")
            if m.strip()
        }
        sheet_index[norm].append((idx, mset))
    return sheet_index


def _find_matching_row(
    sheet_index: Dict[str, List[Tuple[int, Set[str]]]],
    path: str,
    methods: str,
    matched_rows: Set[int],
) -> Optional[int]:
    """Find the worksheet row for a given endpoint."""
    norm = normalize_path(path)
    endpoint_methods = {m.strip().upper() for m in methods.split(",") if m.strip()}

    for idx, sheet_methods in sheet_index.get(norm, []):
        if idx in matched_rows:
            continue
        if (
            "ALL" in endpoint_methods
            or "ALL" in sheet_methods
            or endpoint_methods & sheet_methods
            or not sheet_methods
            or not endpoint_methods
        ):
            return idx
    return None


def _batch_set_text_color(
    spreadsheet,
    worksheet_id: int,
    targets: List[Tuple[int, int]],
    color: Dict[str, float],
    errors: List[str],
    label: str,
) -> int:
    """Apply *color* to each (row_num, col_num) cell in *targets* (1-based)."""
    if not targets:
        return 0
    requests = [
        {
            "repeatCell": {
                "range": {
                    "sheetId": worksheet_id,
                    "startRowIndex": row_num - 1,
                    "endRowIndex": row_num,
                    "startColumnIndex": col_num - 1,
                    "endColumnIndex": col_num,
                },
                "cell": {
                    "userEnteredFormat": {"textFormat": {"foregroundColor": color}}
                },
                "fields": "userEnteredFormat.textFormat.foregroundColor",
            }
        }
        for row_num, col_num in targets
    ]
    try:
        spreadsheet.batch_update({"requests": requests})
        return len(targets)
    except Exception as exc:
        errors.append(f"{label.upper()} TEXT COLOR ERROR: {exc}")
        return 0


def _reset_worksheet_text_color(
    spreadsheet,
    worksheet_id: int,
    n_rows: int,
    errors: List[str],
) -> bool:
    """Reset all data rows (row 2 onward) to black text in one API call.

    Replaces the previous approach of reading every cell's effectiveFormat via
    a raw HTTP request to detect magenta cells before resetting them. A single
    repeatCell covering the entire data range is faster, simpler, and removes
    the AuthorizedSession / raw-HTTP dependency.
    """
    if n_rows <= 1:
        return False
    request = {
        "repeatCell": {
            "range": {
                "sheetId": worksheet_id,
                "startRowIndex": 1,  # skip header row
                "endRowIndex": n_rows,
                "startColumnIndex": 0,
                "endColumnIndex": len(SHEET_COLUMNS),
            },
            "cell": {
                "userEnteredFormat": {"textFormat": {"foregroundColor": BLACK_TEXT_RGB}}
            },
            "fields": "userEnteredFormat.textFormat.foregroundColor",
        }
    }
    try:
        spreadsheet.batch_update({"requests": [request]})
        return True
    except Exception as exc:
        errors.append(f"RESET TEXT COLOR ERROR: {exc}")
        return False


# Columns the script compares and updates on matched rows.
# (column_index, endpoint_dict_key, human_label)
_UPDATABLE_COLUMNS = [
    (COL_STATUS, "sheet_status", "endpoint status"),
    (COL_X_ANNOTATED, "x_annotated", "x-annotated"),
    (COL_BACKED_MS, "backed_ms", "backed (server)"),
    (COL_BACKED_MC, "backed_mc", "backed (cloud)"),
    (COL_COMPLETENESS_MS, "completeness_ms", "completeness (server)"),
    (COL_COMPLETENESS_MC, "completeness_mc", "completeness (cloud)"),
    (COL_DRIVEN_MS, "driven_ms", "driven (server)"),
    (COL_DRIVEN_MC, "driven_mc", "driven (cloud)"),
    (COL_NOTES, "notes", "notes"),
]


def _method_sort_key(method: str) -> Tuple[int, str]:
    order = {
        "ALL": 0,
        "GET": 1,
        "POST": 2,
        "PUT": 3,
        "PATCH": 4,
        "DELETE": 5,
        "OPTIONS": 6,
        "HEAD": 7,
    }
    return order.get(method, 99), method


def _merge_methods(method_values: List[str]) -> str:
    merged: Set[str] = set()
    for raw in method_values:
        for method in raw.replace(";", ",").split(","):
            method = method.strip().upper()
            if method:
                merged.add(method)
    return ", ".join(sorted(merged, key=_method_sort_key))


def _reduce_backed(values: List[str]) -> str:
    uniq = {v for v in values if v}
    if not uniq:
        return ""
    if "TRUE" in uniq:
        return "TRUE"
    if "FALSE" in uniq:
        return "FALSE"
    return "No Schema"


def _reduce_driven(values: List[str]) -> str:
    uniq = {v for v in values if v}
    if not uniq:
        return ""
    if "Partial" in uniq:
        return "Partial"
    if "TRUE" in uniq and "FALSE" in uniq:
        return "Partial"
    if "TRUE" in uniq:
        return "TRUE"
    if "FALSE" in uniq:
        return "FALSE"
    return "FALSE"


def _reduce_path_completeness(values: List[str]) -> str:
    uniq = {v for v in values if v}
    if not uniq:
        return ""
    if uniq == {"Full"}:
        return "Full"
    if uniq == {"No Schema"}:
        return "No Schema"
    if uniq == {"Not-audited"}:
        return "Not-audited"
    if "Partial" in uniq:
        return "Partial"
    if "Full" in uniq and ("Stub" in uniq or "No Schema" in uniq or "Not-audited" in uniq):
        return "Partial"
    if "Full" in uniq:
        return "Full"
    if "Stub" in uniq:
        return "Stub"
    if "Not-audited" in uniq:
        return "Not-audited"
    return "No Schema"


def _merge_notes_for_sheet(group: List[Dict[str, Any]]) -> str:
    merged_notes: List[str] = []
    seen: Set[str] = set()
    for ep in sorted(group, key=endpoint_sort_key):
        note = ep.get("notes", "").strip()
        if not note:
            continue
        entry = f"[{ep['methods']}]\n{note}" if len(group) > 1 else note
        if entry not in seen:
            seen.add(entry)
            merged_notes.append(entry)
    return "\n\n".join(merged_notes)


def _aggregate_endpoints_for_sheet(endpoints: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Collapse endpoint rows to one worksheet candidate per normalized path."""
    grouped: Dict[str, List[Dict[str, Any]]] = defaultdict(list)
    for ep in endpoints:
        grouped[normalize_path(ep["path"])].append(ep)

    aggregated: List[Dict[str, Any]] = []
    for norm_path, group in grouped.items():
        if len(group) == 1:
            aggregated.append(dict(group[0]))
            continue

        primary = max(group, key=lambda ep: (_COV_RANK.get(ep["coverage"], 0), endpoint_sort_key(ep)))
        merged = dict(primary)
        merged["path"] = primary["path"]
        merged["methods"] = _merge_methods([ep["methods"] for ep in group])
        merged["backed_ms"] = _reduce_backed([ep["backed_ms"] for ep in group])
        merged["backed_mc"] = _reduce_backed([ep["backed_mc"] for ep in group])
        merged["completeness_ms"] = _reduce_path_completeness([ep["completeness_ms"] for ep in group])
        merged["completeness_mc"] = _reduce_path_completeness([ep["completeness_mc"] for ep in group])
        merged["driven_ms"] = _reduce_driven([ep["driven_ms"] for ep in group])
        merged["driven_mc"] = _reduce_driven([ep["driven_mc"] for ep in group])
        merged["meshery_present"] = any(bool(ep.get("meshery_present")) for ep in group)
        merged["cloud_present"] = any(bool(ep.get("cloud_present")) for ep in group)
        merged["notes"] = _merge_notes_for_sheet(group)
        aggregated.append(merged)

    return sorted(aggregated, key=endpoint_sort_key)


def update_sheet(
    endpoints: List[Dict[str, Any]],
    sheet_id: str,
    dry_run: bool = False,
    prefetched: Optional[Tuple[Any, Any, List[List[str]]]] = None,
) -> Dict[str, Any]:
    """Diff computed endpoints against the sheet and apply updates.

    - Matches rows by normalized endpoint path + method overlap.
    - Updates Coverage, Status, Schema-Backed, Schema Completeness,
      Schema-Driven, and Notes columns when they differ.
    - Inserts new rows into matching category groups when possible.
    - Stamps the Change Log column on modified rows.

    *prefetched* — optional (sheet, ws, rows) tuple from sheet_diff_analysis.
    When supplied the sheet is not re-opened and rows are not re-fetched,
    saving one full API round-trip.
    """
    if prefetched is not None:
        sheet, ws, current_rows = prefetched
    else:
        gc = _get_sheet_client()
        if not gc:
            print(
                "ERROR: No credentials found.\n"
                "  Set GOOGLE_CREDENTIALS_JSON (inline JSON for CI) or\n"
                "  GOOGLE_APPLICATION_CREDENTIALS (file path for local dev).",
                file=sys.stderr,
            )
            sys.exit(1)
        sheet = gc.open_by_key(sheet_id)
        ws = sheet.get_worksheet(AUDIT_WORKSHEET_INDEX)
        current_rows = ws.get_all_values()

    changes: List[str] = []
    errors: List[str] = []
    sheet_index = _build_sheet_index(current_rows)
    batch_updates: List[Dict[str, Any]] = []
    new_rows_info: List[Tuple[List[str], str, str]] = []
    highlight_specs: List[Tuple[str, str, Set[int]]] = []
    matched_rows: Set[int] = set()
    today = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    sheet_endpoints = _aggregate_endpoints_for_sheet(endpoints)

    for ep in sheet_endpoints:
        matched_idx = _find_matching_row(sheet_index, ep["path"], ep["methods"], matched_rows)

        if matched_idx is not None:
            matched_rows.add(matched_idx)
            row = current_rows[matched_idx]
            while len(row) < len(SHEET_COLUMNS):
                row.append("")

            row_changed = False
            changed_cols: Set[int] = set()
            for col_idx, field, label in _UPDATABLE_COLUMNS:
                old_val = row[col_idx].strip() if len(row) > col_idx else ""
                new_val = ep[field]
                if old_val != new_val:
                    cl = _col_letter(col_idx)
                    changes.append(
                        f"UPDATE row {matched_idx + 1} [{ep['path']}] "
                        f"{label}: '{old_val}' -> '{new_val}'"
                    )
                    batch_updates.append({
                        "range": f"{cl}{matched_idx + 1}",
                        "values": [[new_val]],
                    })
                    row_changed = True
                    changed_cols.add(col_idx + 1)

            if row_changed:
                cl = _col_letter(COL_CHANGELOG)
                batch_updates.append({
                    "range": f"{cl}{matched_idx + 1}",
                    "values": [[today]],
                })
                changed_cols.add(COL_CHANGELOG + 1)
                highlight_specs.append((ep["path"], ep["methods"], changed_cols))
        else:
            new_row = [
                ep["category"],
                ep["subcategory"],
                ep["path"],
                ep["methods"],
                ep["sheet_status"],
                ep["x_annotated"],
                ep["backed_ms"],
                ep["backed_mc"],
                ep["completeness_ms"],
                ep["completeness_mc"],
                ep["driven_ms"],
                ep["driven_mc"],
                ep["notes"],
                today,
            ]
            changes.append(
                f"NEW ROW: {ep['path']} [{ep['methods']}] "
                f"status={ep['sheet_status']} "
                f"x-annotated={ep['x_annotated']} "
                f"backed_ms={ep['backed_ms']} backed_mc={ep['backed_mc']} "
                f"completeness_ms={ep['completeness_ms']} completeness_mc={ep['completeness_mc']} "
                f"driven_ms={ep['driven_ms']} driven_mc={ep['driven_mc']}"
            )
            new_rows_info.append((new_row, ep["category"], ep["subcategory"]))
            highlight_specs.append(
                (
                    ep["path"],
                    ep["methods"],
                    set(range(1, len(SHEET_COLUMNS) + 1)),
                )
            )

    new_rows_info.sort(
        key=lambda item: endpoint_sort_key({
            "category": item[1],
            "subcategory": item[2],
            "path": item[0][COL_ENDPOINTS],
            "methods": item[0][COL_METHODS],
        })
    )

    # --- Apply changes (reset text color only when there is something to update) ---
    has_changes = bool(batch_updates or new_rows_info)
    reset_applied = False
    if not dry_run and has_changes:
        reset_applied = _reset_worksheet_text_color(
            sheet, ws.id, len(current_rows), errors
        )

    batch_update_count = 0
    if not dry_run and batch_updates:
        try:
            ws.batch_update(batch_updates, value_input_option="RAW")
            batch_update_count = len(batch_updates)
        except Exception as exc:
            errors.append(f"BATCH UPDATE ERROR: {exc}")

    # --- Insert new rows ---
    inserted_rows = 0
    appended_rows = 0
    if not dry_run and new_rows_info:
        inserted_rows, appended_rows = _insert_rows_by_group(
            ws, new_rows_info, errors
        )

    highlighted_cells = 0
    if not dry_run and highlight_specs:
        refreshed_rows = ws.get_all_values()
        refreshed_index = _build_sheet_index(refreshed_rows)
        # Accumulate cols per row index so that multiple audit endpoints
        # (e.g. GET and POST) that map to the same sheet row don't block
        # each other via the exclusion set and generate false HIGHLIGHT ERRORs.
        row_cols: Dict[int, Set[int]] = {}

        for path, methods, cols in highlight_specs:
            # Pass an empty set so the same row can be matched for different
            # method entries; dedup is handled by row_cols below.
            row_idx = _find_matching_row(refreshed_index, path, methods, set())
            if row_idx is None:
                changes.append(f"HIGHLIGHT ERROR: unable to resolve row for {path} [{methods}]")
                continue
            row_cols.setdefault(row_idx, set()).update(cols)

        resolved_targets: List[Tuple[int, int]] = [
            (row_idx + 1, col_num)
            for row_idx, col_set in row_cols.items()
            for col_num in sorted(col_set)
        ]
        highlighted_cells = _batch_set_text_color(
            sheet,
            ws.id,
            resolved_targets,
            MAGENTA_TEXT_RGB,
            errors,
            "highlight",
        )

    return {
        "worksheet_title": ws.title,
        "changes": changes,
        "errors": errors,
        "updated_rows": sum(1 for c in changes if c.startswith("UPDATE")),
        "new_rows": sum(1 for c in changes if c.startswith("NEW ROW")),
        "batch_update_cells": batch_update_count,
        "inserted_rows": inserted_rows,
        "appended_rows": appended_rows,
        "highlighted_cells": highlighted_cells,
        "reset_applied": reset_applied,
    }


def _insert_rows_by_group(
    ws,
    new_rows_info: List[Tuple[List[str], str, str]],
    errors: List[str],
) -> Tuple[int, int]:
    """Insert new rows into the correct category/sub-category block.

    Groups insertions by target position and processes from bottom to top
    so that earlier inserts don't shift indices for later ones.
    """
    try:
        all_rows = ws.get_all_values()
    except Exception as exc:
        errors.append(f"INSERT ERROR (read failed): {exc}")
        return 0, 0

    # Build index: last row for each (category, subcategory) and category
    group_last_row: Dict[Tuple[str, str], int] = {}
    cat_last_row: Dict[str, int] = {}
    last_cat = ""
    last_sub = ""

    for idx, row in enumerate(all_rows):
        if idx == 0:
            continue

        cat = row[COL_CATEGORY].strip() if len(row) > COL_CATEGORY else ""
        sub = row[COL_SUBCATEGORY].strip() if len(row) > COL_SUBCATEGORY else ""

        if cat:
            last_cat = cat
        else:
            cat = last_cat

        if sub:
            last_sub = sub
        else:
            sub = last_sub

        # Only track rows that carry actual endpoint data so that blank
        # separator rows between groups don't become the "insert after"
        # target (which would leave a blank line between rows).
        has_endpoint = bool(
            row[COL_ENDPOINTS].strip() if len(row) > COL_ENDPOINTS else ""
        )
        if cat and has_endpoint:
            group_last_row[(cat, sub)] = idx
            cat_last_row[cat] = idx

    # Classify each new row: targeted insert or append
    inserts: List[Tuple[int, List[str]]] = []
    append_rows: List[List[str]] = []

    for row_data, cat, sub in new_rows_info:
        target = group_last_row.get((cat, sub))
        if target is None:
            target = cat_last_row.get(cat)

        if target is not None:
            inserts.append((target, row_data))
        else:
            append_rows.append(row_data)

    # Insert from bottom to top to preserve indices
    inserts.sort(key=lambda item: item[0], reverse=True)
    inserted_rows = 0

    for insert_after, row_data in inserts:
        try:
            ws.insert_row(row_data, insert_after + 2, value_input_option="RAW")
            inserted_rows += 1
        except Exception as exc:
            errors.append(f"INSERT ERROR at row {insert_after + 2}: {exc}")

    appended_rows = 0
    if append_rows:
        try:
            ws.append_rows(append_rows, value_input_option="RAW")
            appended_rows = len(append_rows)
        except Exception as exc:
            errors.append(f"APPEND ROWS ERROR: {exc}")

    return inserted_rows, appended_rows


# ---------------------------------------------------------------------------
# Summary & Insights
# ---------------------------------------------------------------------------

def _print_table(
    title: str,
    headers: List[str],
    rows: List[List[Any]],
    numeric_cols: Optional[Set[int]] = None,
) -> None:
    """Print a formatted ASCII table with box-drawing characters."""
    if not rows:
        if title:
            print(f"\n{title}")
        print("  (no data)")
        return

    n_cols = len(headers)
    numeric_cols = numeric_cols or set()

    widths = [len(str(h)) for h in headers]
    for row in rows:
        for i, cell in enumerate(row):
            widths[i] = max(widths[i], len(str(cell)))
    widths = [w + 2 for w in widths]

    def fmt_cell(val, col):
        s = str(val)
        w = widths[col]
        return s.rjust(w - 1) + " " if col in numeric_cols else " " + s.ljust(w - 1)

    top = "\u250c" + "\u252c".join("\u2500" * w for w in widths) + "\u2510"
    mid = "\u251c" + "\u253c".join("\u2500" * w for w in widths) + "\u2524"
    bot = "\u2514" + "\u2534".join("\u2500" * w for w in widths) + "\u2518"
    fmt_row = lambda cells: "\u2502" + "\u2502".join(
        fmt_cell(cells[i], i) for i in range(n_cols)
    ) + "\u2502"

    if title:
        print(f"\n{title}")
    print(top)
    print(fmt_row(headers))
    print(mid)
    for row in rows:
        print(fmt_row(row))
    print(bot)


def _empty_summary_counts() -> Dict[str, int]:
    return {key: 0 for key in SUMMARY_KEYS}


def _is_tagged(value: str) -> bool:
    return value in {"Cloud-only", "Meshery"}


def _is_complete_value(value: str) -> bool:
    return value == "Full"


def _is_incomplete_value(value: str) -> bool:
    return value in {"Partial", "Stub", "No Schema", "Not-audited"}


def _is_driven_value(value: str) -> bool:
    return value in {"TRUE", "Partial"}


def collect_endpoint_summary(
    endpoints: List[Dict[str, Any]],
    spec_data: Dict[str, Any],
) -> Dict[str, Any]:
    """Collect final CLI summary counts from spec and computed endpoint rows.

    Summary semantics:
      - Endpoints (Spec) comes from the bundled spec only.
      - Endpoints (Router) and all other metrics come from route-aware analysis.
    """
    summary = {
        "total": _empty_summary_counts(),
        "meshery": _empty_summary_counts(),
        "cloud": _empty_summary_counts(),
    }

    operations = spec_data.get("operations", {})
    x_internal = spec_data.get("x_internal", {})
    all_paths = spec_data.get("all_paths")
    if all_paths:
        unique_paths = set(all_paths)
    else:
        unique_paths = {path for path, _method in operations}

    tagged_paths: Set[str] = set()
    meshery_tagged_paths: Set[str] = set()
    cloud_tagged_paths: Set[str] = set()
    for op_key in operations:
        path, _method = op_key
        xi_vals = set(x_internal.get(op_key, []))
        if xi_vals & {"cloud", "meshery"}:
            tagged_paths.add(path)
        if "meshery" in xi_vals:
            meshery_tagged_paths.add(path)
        if "cloud" in xi_vals:
            cloud_tagged_paths.add(path)

    summary["total"]["endpoints_spec"] = len(unique_paths)
    summary["total"]["x_internal_tagged"] = len(tagged_paths)
    # not_tagged removed from summary table — x_internal_tagged is sufficient
    summary["meshery"]["x_internal_tagged"] = len(meshery_tagged_paths)
    summary["cloud"]["x_internal_tagged"] = len(cloud_tagged_paths)

    meshery_matches_cloud_tagged: List[str] = []

    for ep in endpoints:
        total = summary["total"]
        cov = ep.get("coverage", "")

        if ep.get("meshery_present") or ep.get("cloud_present"):
            total["endpoints_router"] += 1
        if cov == "Overlap":
            total["implemented"] += 1
        elif cov == "Schema Underlap":
            total["unimplemented"] += 1

        _ms_present = ep.get("meshery_present")
        _mc_present = ep.get("cloud_present")

        # --- backed: priority TRUE > FALSE > No Schema (mutually exclusive per endpoint) ---
        _ms_backed = ep.get("backed_ms") if _ms_present else None
        _mc_backed = ep.get("backed_mc") if _mc_present else None
        if "TRUE" in (_ms_backed, _mc_backed):
            total["schema_backed"] += 1
        elif "FALSE" in (_ms_backed, _mc_backed):
            total["not_schema_backed"] += 1
        # else: no_schema — derived at the end

        # --- completeness: priority Complete > Not-audited > Incomplete (within backed) ---
        _ms_compl = ep.get("completeness_ms") if _ms_present else None
        _mc_compl = ep.get("completeness_mc") if _mc_present else None
        if _is_complete_value(_ms_compl or "") or _is_complete_value(_mc_compl or ""):
            total["complete"] += 1
        elif _ms_compl == "Not-audited" or _mc_compl == "Not-audited":
            total["not_audited"] += 1

        if (_ms_present and _is_driven_value(ep.get("driven_ms", ""))) or (
            _mc_present and _is_driven_value(ep.get("driven_mc", ""))
        ):
            total["schema_driven"] += 1

        if ep.get("meshery_present"):
            meshery = summary["meshery"]
            meshery["endpoints_router"] += 1
            if cov == "Overlap":
                meshery["implemented"] += 1
            if ep.get("backed_ms") == "TRUE":
                meshery["schema_backed"] += 1
            elif ep.get("backed_ms") == "FALSE":
                meshery["not_schema_backed"] += 1
            else:
                meshery["no_schema"] += 1
            if _is_complete_value(ep.get("completeness_ms", "")):
                meshery["complete"] += 1
            elif ep.get("completeness_ms") == "Not-audited":
                meshery["not_audited"] += 1
            if _is_driven_value(ep.get("driven_ms", "")):
                meshery["schema_driven"] += 1
            if ep.get("x_annotated", "") == "Cloud-only":
                meshery_matches_cloud_tagged.append(
                    f"{ep.get('path', '')} [{ep.get('methods', '')}]"
                )

        if ep.get("cloud_present"):
            cloud = summary["cloud"]
            cloud["endpoints_router"] += 1
            if cov == "Overlap":
                cloud["implemented"] += 1
            if ep.get("backed_mc") == "TRUE":
                cloud["schema_backed"] += 1
            elif ep.get("backed_mc") == "FALSE":
                cloud["not_schema_backed"] += 1
            else:
                cloud["no_schema"] += 1
            if _is_complete_value(ep.get("completeness_mc", "")):
                cloud["complete"] += 1
            elif ep.get("completeness_mc") == "Not-audited":
                cloud["not_audited"] += 1
            if _is_driven_value(ep.get("driven_mc", "")):
                cloud["schema_driven"] += 1

    # Derive remaining counts
    for key in ("total", "meshery", "cloud"):
        group = summary[key]
        group["no_schema"] = (
            group["endpoints_router"]
            - group["schema_backed"]
            - group["not_schema_backed"]
        )
        group["incomplete"] = (
            group["schema_backed"]
            - group["complete"]
            - group["not_audited"]
        )
        group["not_schema_driven"] = (
            group["endpoints_router"]
            - group["schema_driven"]
        )

    summary["notes"] = []
    if meshery_matches_cloud_tagged:
        summary["notes"].append(
            f"{len(meshery_matches_cloud_tagged)} Meshery router endpoints match spec endpoints tagged as cloud."
        )
        summary["notes"].extend(meshery_matches_cloud_tagged)

    return summary





def render_audit_summary_table(
    summary: Dict[str, Dict[str, int]],
    include_meshery: bool,
    include_cloud: bool,
) -> None:
    """Render the final audit summary table."""
    headers = ["Category", "Total", "Meshery", "Cloud"]
    numeric_cols = {1, 2, 3}

    # Keys where per-platform breakdown doesn't apply
    _TOTAL_ONLY_KEYS = {"endpoints_spec", "unimplemented"}

    rows: List[List[Any]] = []
    labels = dict(SUMMARY_TABLE_ROWS)
    for key in SUMMARY_KEYS:
        if key in _TOTAL_ONLY_KEYS:
            meshery_value: Any = "-"
            cloud_value: Any = "-"
        else:
            meshery_value = summary["meshery"].get(key, 0) if include_meshery else "-"
            cloud_value = summary["cloud"].get(key, 0) if include_cloud else "-"
        row: List[Any] = [
            labels[key],
            summary["total"].get(key, 0),
            meshery_value,
            cloud_value,
        ]
        rows.append(row)

    print("\nAPI Audit Summary")
    print("  Total = unique endpoints (deduped); Meshery and Cloud are independent per-repo counts.")
    print("  Meshery + Cloud may exceed Total for rows where an endpoint appears in both routers.")
    _print_table(
        "",
        headers,
        rows,
        numeric_cols=numeric_cols,
    )
    notes = summary.get("notes", [])
    if notes:
        print("\nNotes:")
        for note in notes:
            print(note)


def prefetch_sheet_snapshot(
    sheet_id: str,
) -> Tuple[Optional[Tuple[Any, Any, List[List[str]]]], Optional[str]]:
    """Open the audit worksheet once for comparison and update reuse."""
    gc = _get_sheet_client()
    if not gc:
        return None, "Google Sheet comparison unavailable; credentials could not be loaded."

    try:
        sheet = gc.open_by_key(sheet_id)
        ws = sheet.get_worksheet(AUDIT_WORKSHEET_INDEX)
        rows = ws.get_all_values()
        return (sheet, ws, rows), None
    except Exception as exc:
        return None, f"Google Sheet comparison unavailable: {exc}"


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(
        description=(
            "Audit Meshery API endpoints for schema coverage, completeness, "
            "and schema-driven status.  Reads the authoritative bundled "
            "OpenAPI spec from meshery-schemas and server source from "
            "meshery/meshery."
        )
    )
    parser.add_argument(
        "--meshery-repo",
        default=os.environ.get("MESHERY_REPO", ""),
        help=(
            "Path to the meshery/meshery repo root "
            "(default: $MESHERY_REPO env var)"
        ),
    )
    parser.add_argument(
        "--spec",
        default=os.environ.get("OPENAPI_SPEC_PATH", ""),
        help=(
            "Path to the bundled OpenAPI spec. "
            "Defaults to _openapi_build/merged_openapi.yml in this repo."
        ),
    )
    parser.add_argument(
        "--sheet-id",
        default=os.environ.get("SHEET_ID"),
        help="Google Sheet ID (default: $SHEET_ID env var)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print diff without writing to the sheet",
    )
    parser.add_argument(
        "--cloud-repo",
        default=os.environ.get("CLOUD_REPO", ""),
        help=(
            "Path to the meshery-cloud repo root (default: $CLOUD_REPO env var). "
            "Uses the Echo router parser."
        ),
    )
    parser.add_argument(
        "--verbose", "-v",
        action="store_true",
        help="Print per-endpoint details",
    )
    args = parser.parse_args()
    schemas_root = Path(__file__).resolve().parents[2]

    if args.spec:
        spec_file = Path(args.spec).resolve()
    else:
        spec_file = schemas_root / DEFAULT_SPEC_PATH

    if not spec_file.exists():
        print(
            f"ERROR: {spec_file} not found.\n"
            "Run 'make bundle-openapi' first to generate the bundled spec,\n"
            "or pass --spec to point to an existing spec file.",
            file=sys.stderr,
        )
        sys.exit(1)

    spec_data = parse_openapi(spec_file)

    # Sheet-update mode requires a sheet ID and credentials.
    # Dry-run mode never writes to the sheet.
    if args.sheet_id and not args.dry_run:
        missing: List[str] = []
        if not args.meshery_repo and not args.cloud_repo:
            missing.append("MESHERY_REPO / --meshery-repo (or --cloud-repo)")
        if not _has_sheet_credentials_configured():
            missing.append(
                "Google credentials "
                "(GOOGLE_CREDENTIALS_JSON or GOOGLE_APPLICATION_CREDENTIALS)"
            )
        if missing:
            print(
                "ERROR: sheet-update mode requires:\n"
                "  - " + "\n  - ".join(missing),
                file=sys.stderr,
            )
            sys.exit(1)

    if not (args.meshery_repo or args.cloud_repo):
        print(
            "ERROR: at least one of MESHERY_REPO or CLOUD_REPO must be set.\n"
            "  make api-audit MESHERY_REPO=../meshery\n"
            "  make api-audit CLOUD_REPO=../meshery-cloud\n"
            "  make api-audit MESHERY_REPO=../meshery CLOUD_REPO=../meshery-cloud",
            file=sys.stderr,
        )
        sys.exit(1)

    print("Preparing API audit...")

    combined_mode = bool(args.meshery_repo and args.cloud_repo)
    include_meshery = bool(args.meshery_repo)
    include_cloud = bool(args.cloud_repo)

    if combined_mode:
        # ── Combined mode: analyse both repos in one pass, merge results ──────
        meshery_root = Path(args.meshery_repo).resolve()
        cloud_root = Path(args.cloud_repo).resolve()

        for label, root, cfg in [
            ("Meshery Server", meshery_root, MESHERY_CONFIG),
            ("Meshery Cloud", cloud_root, MESHERY_CLOUD_CONFIG),
        ]:
            if not (root / cfg.router_file).exists():
                print(
                    f"ERROR: {cfg.router_file} not found in {root}\n"
                    f"Check --meshery-repo / --cloud-repo paths.",
                    file=sys.stderr,
                )
                sys.exit(1)

        print("Analyzing Meshery...")
        m_routes, m_schema_map, m_handler_io, m_go_fields, _m_stats = _setup_repo_analysis(
            meshery_root, MESHERY_CONFIG
        )
        meshery_eps = classify_endpoints(
            m_routes, spec_data, m_schema_map,
            handler_io_map=m_handler_io,
            go_fields_map=m_go_fields,
            repo_source="meshery",
        )

        print("Analyzing Meshery Cloud...")
        c_routes, c_schema_map, c_handler_io, c_go_fields, _c_stats = _setup_repo_analysis(
            cloud_root, MESHERY_CLOUD_CONFIG
        )
        cloud_eps = classify_endpoints(
            c_routes, spec_data, c_schema_map,
            handler_io_map=c_handler_io,
            go_fields_map=c_go_fields,
            repo_source="cloud",
        )

        endpoints = merge_endpoint_lists(meshery_eps, cloud_eps)

    else:
        # ── Single-repo mode ──────────────────────────────────────────────────
        if args.cloud_repo:
            repo_config = MESHERY_CLOUD_CONFIG
            repo_root = Path(args.cloud_repo).resolve()
            repo_source = "cloud"
        else:
            repo_config = MESHERY_CONFIG
            repo_root = Path(args.meshery_repo).resolve()
            repo_source = "meshery"

        if not (repo_root / repo_config.router_file).exists():
            print(
                f"ERROR: {repo_config.router_file} not found in {repo_root}\n"
                "Use --meshery-repo (or --cloud-repo) to point to the repo root.",
                file=sys.stderr,
            )
            sys.exit(1)

        print(
            "Analyzing Meshery Cloud..."
            if repo_source == "cloud"
            else "Analyzing Meshery..."
        )
        routes, schema_map, handler_io_map, go_fields_map, _repo_stats = _setup_repo_analysis(
            repo_root, repo_config
        )
        endpoints = classify_endpoints(
            routes, spec_data, schema_map,
            handler_io_map=handler_io_map,
            go_fields_map=go_fields_map,
            repo_source=repo_source,
        )

    summary = collect_endpoint_summary(endpoints, spec_data)
    notes: List[str] = []
    prefetched = None

    if args.dry_run or not args.sheet_id:
        render_audit_summary_table(
            summary,
            include_meshery=include_meshery,
            include_cloud=include_cloud,
        )
        if args.verbose:
            print("\nDetailed endpoint output:")
            for ep in endpoints:
                print(
                    f"  {ep['path']:55s} [{ep['methods']:20s}] "
                    f"st={ep['status']:14s} "
                    f"bk_ms={ep['backed_ms']:5s} bk_mc={ep['backed_mc']:5s} "
                    f"comp_ms={ep['completeness_ms']:7s} comp_mc={ep['completeness_mc']:7s} "
                    f"drv_ms={ep['driven_ms']:7s} drv_mc={ep['driven_mc']:7s}"
                )
        sys.exit(0)

    prefetched, prefetch_note = prefetch_sheet_snapshot(args.sheet_id)
    if prefetch_note:
        notes.append(prefetch_note)

    print("Updating Google Sheet...")
    sheet_result = update_sheet(
        endpoints,
        args.sheet_id,
        args.dry_run,
        prefetched=prefetched,
    )

    render_audit_summary_table(
        summary,
        include_meshery=include_meshery,
        include_cloud=include_cloud,
    )

    if sheet_result["errors"]:
        notes.extend(sheet_result["errors"])

    if notes:
        print("\nNotes:")
        for note in notes:
            print(note)

    if args.verbose:
        print("\nDetailed endpoint output:")
        for ep in endpoints:
            print(
                f"  {ep['path']:55s} [{ep['methods']:20s}] "
                f"cov={ep['coverage']:16s} st={ep['status']:14s} "
                f"bk_ms={ep['backed_ms']:5s} bk_mc={ep['backed_mc']:5s} "
                f"comp_ms={ep['completeness_ms']:7s} comp_mc={ep['completeness_mc']:7s} "
                f"drv_ms={ep['driven_ms']:7s} drv_mc={ep['driven_mc']:7s}"
            )
        if sheet_result["changes"]:
            print("\nDetailed sheet changes:")
            for ch in sheet_result["changes"]:
                print(f"  {ch}")

    if sheet_result["errors"]:
        print("\nGoogle Sheet update completed with errors")
    elif sheet_result["changes"]:
        print("\nGoogle Sheet updated successfully")
    else:
        print("\nGoogle Sheet already up to date")


if __name__ == "__main__":
    main()
