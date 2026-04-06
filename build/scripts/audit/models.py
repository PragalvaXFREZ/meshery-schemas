"""EndpointRecord dataclass and helpers shared across the audit pipeline."""

import re

from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, Tuple

from .config import CATEGORY_FALLBACK


# ---------------------------------------------------------------------------
# Normalized endpoint fact model
# ---------------------------------------------------------------------------

@dataclass
class EndpointRecord:
    """Normalized endpoint facts — computed once, formatted once.

    Raw facts are gathered during classification (router walk + spec walk).
    The formatting layer (format_record_for_sheet) converts these into
    sheet-ready display values exactly once.
    """
    # Identity
    path: str
    method: str
    category: str = ""
    subcategory: str = ""
    handler: str = ""

    # Spec facts
    in_spec: bool = False
    x_annotation: str = "None"  # "Cloud-only" | "Meshery" | "None"

    # Router facts
    exists_in_meshery_router: bool = False
    exists_in_cloud_router: bool = False
    is_commented: bool = False

    # Derived ownership (from annotation + spec presence)
    belongs_to_meshery: bool = False
    belongs_to_cloud: bool = False

    # Schema-backed (does spec define this endpoint for the platform?)
    meshery_schema_backed: bool = False
    cloud_schema_backed: bool = False

    # Schema completeness (raw internal values)
    # "Full" | "Partial" | "Stub" | "Not-audited" | None
    meshery_schema_completeness: Optional[str] = None
    cloud_schema_completeness: Optional[str] = None

    # Schema-driven (raw from schema_map lookup)
    # "TRUE" | "FALSE" | "Partial" | None
    meshery_schema_driven: Optional[str] = None
    cloud_schema_driven: Optional[str] = None

    # Internal fields (for notes and summary, not displayed directly)
    coverage: str = ""
    status: str = ""
    compl_notes: List[str] = field(default_factory=list)
    notes: str = ""
    repo_source: str = ""


def format_record_for_sheet(rec: EndpointRecord) -> Dict[str, Any]:
    """Convert an EndpointRecord into the dict expected by update_sheet().

    This is the single source of truth for all derived audit column values.
    Every display-facing value is computed here from raw facts.
    """
    # --- Endpoint Status ---
    if rec.is_commented:
        if rec.belongs_to_meshery and rec.belongs_to_cloud:
            sheet_status = "Deprecated - Both"
        elif rec.belongs_to_cloud:
            sheet_status = "Deprecated - Meshery Cloud"
        else:
            sheet_status = "Deprecated - Meshery Server"
    else:
        m_active = rec.exists_in_meshery_router and not rec.is_commented
        c_active = rec.exists_in_cloud_router and not rec.is_commented

        if rec.belongs_to_meshery and rec.belongs_to_cloud:
            # Shared endpoint
            if m_active and c_active:
                sheet_status = "Active - Both"
            elif m_active:
                sheet_status = "Active - Meshery Server, Unimplemented - Meshery Cloud"
            elif c_active:
                sheet_status = "Active - Meshery Cloud, Unimplemented - Meshery Server"
            else:
                sheet_status = "Unimplemented - Both"
        elif rec.belongs_to_cloud:
            sheet_status = "Active - Meshery Cloud" if c_active else "Unimplemented - Meshery Cloud"
        elif rec.belongs_to_meshery:
            sheet_status = "Active - Meshery Server" if m_active else "Unimplemented - Meshery Server"
        else:
            # Fallback: not owned by either (shouldn't happen normally)
            if m_active or c_active:
                sheet_status = "Active - Both"
            else:
                sheet_status = "Unimplemented - Both"

    # When no schema exists, show only the active consumer.
    if not rec.in_spec:
        if sheet_status == "Active - Meshery Server, Unimplemented - Meshery Cloud":
            sheet_status = "Active - Meshery Server"
        elif sheet_status == "Active - Meshery Cloud, Unimplemented - Meshery Server":
            sheet_status = "Active - Meshery Cloud"

    # --- x-annotated ---
    if not rec.in_spec:
        x_annotated = "No Schema"
    else:
        x_annotated = rec.x_annotation  # "Cloud-only" | "Meshery" | "None"

    # --- Helper: format per-platform columns ---
    def _fmt_backed(belongs: bool, schema_backed: bool) -> str:
        if not belongs:
            return "-"
        if not rec.in_spec:
            return "-"
        return "True" if schema_backed else "False"

    def _fmt_completeness(belongs: bool, raw: Optional[str], schema_backed: bool) -> str:
        if not belongs:
            return "-"
        if not rec.in_spec or not schema_backed:
            return "-"
        if raw == "Full":
            return "True"
        if raw == "Partial":
            return "Partial"
        # "Stub", "Not-audited", None → "False"
        return "False"

    def _fmt_driven(present: bool, raw: Optional[str]) -> str:
        if not present:
            return "-"
        if raw == "TRUE":
            return "True"
        if raw == "Partial":
            return "Partial"
        # "FALSE", None → "False"
        return "False"

    backed_ms = _fmt_backed(rec.belongs_to_meshery, rec.meshery_schema_backed)
    backed_mc = _fmt_backed(rec.belongs_to_cloud, rec.cloud_schema_backed)
    completeness_ms = _fmt_completeness(
        rec.belongs_to_meshery, rec.meshery_schema_completeness, rec.meshery_schema_backed,
    )
    completeness_mc = _fmt_completeness(
        rec.belongs_to_cloud, rec.cloud_schema_completeness, rec.cloud_schema_backed,
    )
    driven_ms = _fmt_driven(rec.exists_in_meshery_router, rec.meshery_schema_driven)
    driven_mc = _fmt_driven(rec.exists_in_cloud_router, rec.cloud_schema_driven)

    return {
        "category": rec.category,
        "subcategory": rec.subcategory,
        "path": rec.path,
        "methods": rec.method,
        "sheet_status": sheet_status,
        "x_annotated": x_annotated,
        "backed_ms": backed_ms,
        "backed_mc": backed_mc,
        "completeness_ms": completeness_ms,
        "completeness_mc": completeness_mc,
        "driven_ms": driven_ms,
        "driven_mc": driven_mc,
        "notes": rec.notes,
        "handler": rec.handler,
        "coverage": rec.coverage,
        "status": rec.status,
        "meshery_present": rec.exists_in_meshery_router,
        "cloud_present": rec.exists_in_cloud_router,
    }


# ---------------------------------------------------------------------------
# Path helpers
# ---------------------------------------------------------------------------

def normalize_path(path: str) -> str:
    """Replace path parameter tokens with positional {p1}, {p2}, … for matching.

    Handles both OpenAPI/Gorilla-Mux style ({paramName}) and Echo/Express
    style (:paramName) so routes from either framework match spec paths.

    Path segments are lowercased so that casing variants of the same path
    (e.g. /api/academy/Curricula vs /api/academy/curricula) resolve to the
    same key and their x-internal annotations are correctly applied.
    """
    counter = [0]

    def _repl(_m):
        counter[0] += 1
        return f"{{p{counter[0]}}}"

    # Lowercase before parameter replacement so static segments are
    # case-insensitive; parameter tokens get replaced anyway.
    path = path.lower()
    # Normalise Echo-style :param segments first, then OpenAPI {param} style.
    path = re.sub(r"/:([^/]+)", lambda m: "/{" + m.group(1) + "}", path)
    return re.sub(r"\{[^}]+\}", _repl, path)


def is_api_route(path: str) -> bool:
    """Return True for paths that belong to the /api namespace."""
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
        if norm.startswith(prefix):
            return cat, sub
    return "Other", ""


def endpoint_sort_key(endpoint) -> Tuple[str, str, str, str]:
    """Return a deterministic sort key for sheet output.

    Accepts either a dict or an EndpointRecord.
    """
    if isinstance(endpoint, EndpointRecord):
        return (endpoint.category, endpoint.subcategory, endpoint.path, endpoint.method)
    return (
        endpoint["category"],
        endpoint["subcategory"],
        endpoint["path"],
        endpoint.get("methods", ""),
    )


def derive_ownership(x_annotation: str) -> Tuple[bool, bool]:
    """Return (belongs_to_meshery, belongs_to_cloud) from an x-annotation value."""
    if x_annotation == "Cloud-only":
        return False, True
    if x_annotation == "Meshery":
        return True, False
    return True, True
