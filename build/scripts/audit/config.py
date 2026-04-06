"""Repo configuration, sheet layout, and shared constants."""

from typing import List, Optional, Tuple

# ---------------------------------------------------------------------------
# Paths relative to repos
# ---------------------------------------------------------------------------

# Path relative to meshery/meshery repo root
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
PROVIDER_BYTES_SENTINEL = "<provider-[]byte>"

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
