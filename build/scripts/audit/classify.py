"""Endpoint classification, cross-check completeness, and multi-repo merge."""

import re
import sys

from typing import Any, Dict, List, Optional, Set, Tuple

from .analyzer import go_type_lookup_key
from .config import BODY_METHODS, PROVIDER_BYTES_SENTINEL
from .models import (
    EndpointRecord,
    categorize,
    derive_ownership,
    endpoint_sort_key,
    is_api_route,
    normalize_path,
)
from .openapi import assess_completeness, extract_spec_schema_fields


# ---------------------------------------------------------------------------
# Cross-check completeness (Go struct fields vs spec properties)
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

    Returns (completeness, structured_notes).
    Lines are prefixed with markers: [REQ], [RESP], [INFO].
    """
    notes: List[str] = []

    req_type = handler_io.get("request_type")
    resp_type = handler_io.get("response_type")
    is_resp_passthrough = resp_type == PROVIDER_BYTES_SENTINEL

    req_type_key = go_type_lookup_key(req_type)
    resp_type_key = go_type_lookup_key(resp_type)

    req_go_fields = go_fields_map.get(req_type_key) if req_type_key else None
    resp_go_fields = go_fields_map.get(resp_type_key) if resp_type_key else None

    spec_req = spec_fields.get("request_fields", set())
    spec_resp = spec_fields.get("response_fields", set())
    has_spec = bool(spec_req or spec_resp)

    if not has_spec:
        return "Stub", ["[INFO] No spec schema defined for this endpoint"]

    expects_body = bool(spec_req) or (
        method.upper() in BODY_METHODS and bool(req_type)
    )

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
                    notes.append(f"[REQ] Matching: {_format_field_set(req_common)}")
                if req_only_go:
                    notes.append(f"[REQ] In handler only: {_format_field_set(req_only_go, 5)}")
                if req_only_spec:
                    notes.append(f"[REQ] In spec only: {_format_field_set(req_only_spec, 5)}")
            else:
                handler_file = handler_io.get("file", "")
                file_info = f" ({handler_file})" if handler_file else ""
                notes.append(
                    f"[REQ] Struct fields for {req_type_key} not found"
                    f" — inspect handler{file_info}"
                )
                print(
                    f"[AUDIT WARN] {handler_name}: req struct fields not found"
                    f" for type {req_type_key}{file_info}",
                    file=sys.stderr,
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
                notes.append(f"[RESP] Matching: {_format_field_set(resp_common)}")
            if resp_only_go:
                notes.append(f"[RESP] In handler only: {_format_field_set(resp_only_go, 5)}")
            if resp_only_spec:
                notes.append(f"[RESP] In spec only: {_format_field_set(resp_only_spec, 5)}")
        else:
                handler_file = handler_io.get("file", "")
                file_info = f" ({handler_file})" if handler_file else ""
                notes.append(
                    f"[RESP] Struct fields for {resp_type_key} not found"
                    f" — inspect handler{file_info}"
                )
                print(
                    f"[AUDIT WARN] {handler_name}: resp struct fields not found"
                    f" for type {resp_type_key}{file_info}",
                    file=sys.stderr,
                )
    else:
        if spec_resp:
            notes.append("[RESP] Could not extract response type from handler")
        else:
            notes.append(
                "[RESP] No response body in spec — "
                "spec may be incomplete or handler returns nothing"
            )

    # --- Classify ---
    GOOD_THRESHOLD = 0.70

    if expects_body:
        req_good = req_ratio is not None and req_ratio >= GOOD_THRESHOLD
        resp_good = resp_ratio is not None and resp_ratio >= GOOD_THRESHOLD
        req_any = req_ratio is not None and req_ratio > 0
        resp_any = resp_ratio is not None and resp_ratio > 0

        if req_ratio is None and resp_ratio is None:
            if req_type or resp_type:
                return "Audit Failed", notes
            return "Stub", notes

        if req_good and resp_good:
            return "Full", notes
        if req_good or resp_good or (req_any and resp_any):
            return "Partial", notes
        return "Stub", notes
    else:
        if resp_ratio is None:
            if is_resp_passthrough:
                return "Stub", notes
            if resp_type:
                return "Audit Failed", notes
            return "Stub", notes

        if resp_ratio >= GOOD_THRESHOLD:
            return "Full", notes
        if resp_ratio > 0:
            return "Partial", notes
        return "Stub", notes


# ---------------------------------------------------------------------------
# Actionable notes builder
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
    path: str = "",
) -> str:
    """Build a structured, actionable summary for the Notes column."""
    sections: List[str] = []

    if is_commented:
        sections.append(
            "[ACTION] Route is commented out — "
            "consider removal from router and spec"
        )
    elif coverage == "Server Underlap":
        if is_api_route(path):
            sections.append(
                "[ACTION] Not in OpenAPI spec — add spec definition"
            )

    if coverage == "Schema Underlap" and status == "Unimplemented":
        sections.append(
            "[ACTION] In spec but no server route — "
            "implement handler or remove from spec"
        )

    if driven == "FALSE" and coverage == "Overlap":
        sections.append(
            "[ACTION] Not schema-driven — "
            "handler does not import meshery/schemas types"
        )

    if completeness == "Full":
        return "\n\n".join(sections) if sections else ""

    _GAP_KEYWORDS = ("In spec only", "In handler only", "Could not extract",
                     "No response body", "Struct fields for",
                     "Provider returns raw")

    def _is_gap_line(line: str) -> bool:
        return any(kw in line for kw in _GAP_KEYWORDS)

    req_lines = [n for n in compl_notes
                 if n.startswith("[REQ]") and _is_gap_line(n)]
    resp_lines = [n for n in compl_notes
                  if n.startswith("[RESP]") and _is_gap_line(n)]
    info_lines = [n for n in compl_notes if n.startswith("[INFO]")]

    _INFORMATIONAL_PATTERN = re.compile(
        r"^(requestBody|response \d+): "
        r"(references \S+"
        r"|(?:allOf|oneOf|anyOf) with \d+ sub-schemas"
        r"|array of \S+"
        r"|object with properties \[)"
    )
    legacy_lines = [
        n for n in compl_notes
        if not n.startswith(("[REQ]", "[RESP]", "[INFO]"))
        and not _INFORMATIONAL_PATTERN.match(n)
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
        sections.append("[Completeness]\n" + "\n".join(compl_parts))

    return "\n\n".join(sections) if sections else ""


# ---------------------------------------------------------------------------
# Classification helpers
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
    """Reduce a list of per-method completeness values to a single status."""
    unique = _dedup_notes(notes)
    if all(c == "Full" for c in method_comps):
        return "Full", unique
    if any(c == "Full" for c in method_comps) or any(
        c == "Partial" for c in method_comps
    ):
        return "Partial", unique
    return "Stub", unique


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


# ---------------------------------------------------------------------------
# Main classification — bidirectional walk (Router ∪ Spec)
# ---------------------------------------------------------------------------

def classify_endpoints(
    routes: List[Dict[str, Any]],
    spec_data: dict,
    schema_map: Dict[str, Tuple[str, str]],
    handler_io_map: Optional[Dict[str, Dict[str, Optional[str]]]] = None,
    go_fields_map: Optional[Dict[str, Set[str]]] = None,
    repo_source: str = "",
) -> List[EndpointRecord]:
    """Classify endpoints from both router and spec (bidirectional walk).

    Routes are expected to be **per-verb** entries (one method per dict).
    Each (path, method) pair produces exactly one EndpointRecord with raw
    facts.  Formatting into sheet-ready values happens in
    format_record_for_sheet().
    """
    all_paths = spec_data["all_paths"]
    x_internal_map = spec_data["x_internal"]
    original_paths = spec_data["original_paths"]
    path_categories = spec_data.get("path_categories", {})
    operations_map = spec_data.get("operations", {})

    records: List[EndpointRecord] = []
    router_norm_keys: Set[Tuple[str, str]] = set()

    _is_cloud = repo_source == "cloud"

    # ------------------------------------------------------------------
    # Pass 1: Router-sourced endpoints
    # ------------------------------------------------------------------
    for route in routes:
        path = route["path"]
        method = route.get("method", route["methods"][0] if route["methods"] else "ALL")
        handler = route["handler"]
        is_commented = route.get("commented", False)

        norm = normalize_path(path)
        category, subcategory = categorize(path, path_categories)
        router_norm_keys.add((norm, method))

        spec_methods = all_paths.get(norm, set())
        method_in_spec = method in spec_methods or (method == "ALL" and spec_methods)

        coverage = "Overlap" if method_in_spec else "Server Underlap"

        xi = x_internal_map.get((norm, method), []) if method_in_spec else []
        is_cloud_annotated = "cloud" in xi
        is_meshery_annotated = "meshery" in xi

        if is_commented:
            status = "Deprecated"
        elif is_cloud_annotated:
            status = "Active (Cloud-annotated)"
        else:
            status = "Active"

        if is_cloud_annotated:
            x_annotation = "Cloud-only"
        elif is_meshery_annotated:
            x_annotation = "Meshery"
        else:
            x_annotation = "None"

        belongs_to_meshery, belongs_to_cloud = derive_ownership(x_annotation)

        exists_in_meshery = not _is_cloud
        exists_in_cloud = _is_cloud

        # Schema-backed should reflect actual implementation presence plus
        # spec presence. Ownership annotations remain separate signals.
        meshery_schema_backed = exists_in_meshery and method_in_spec
        cloud_schema_backed = exists_in_cloud and method_in_spec

        # --- Schema Completeness ---
        this_backed = meshery_schema_backed if not _is_cloud else cloud_schema_backed
        use_crosscheck = (
            handler_io_map is not None
            and bool(go_fields_map)
            and handler in handler_io_map
            and method_in_spec
        )

        if not this_backed:
            completeness_this: Optional[str] = None
            compl_notes_this: List[str] = []
        elif use_crosscheck:
            op = operations_map.get((norm, method))
            if op:
                sf = extract_spec_schema_fields(op, method)
                completeness_this, compl_notes_this = cross_check_completeness(
                    handler, handler_io_map[handler],
                    go_fields_map, sf, method,
                )
            else:
                completeness_this = "Stub"
                compl_notes_this = []
        elif method_in_spec:
            completeness_this, compl_notes_this = _aggregate_completeness(
                norm, [method], spec_data
            )
        else:
            completeness_this = None
            compl_notes_this = []

        other_backed = cloud_schema_backed if not _is_cloud else meshery_schema_backed
        if not other_backed:
            completeness_other: Optional[str] = None
        elif method_in_spec:
            completeness_other, _ = _aggregate_completeness(
                norm, [method], spec_data
            )
        else:
            completeness_other = None

        if _is_cloud:
            meshery_completeness = completeness_other
            cloud_completeness = completeness_this
        else:
            meshery_completeness = completeness_this
            cloud_completeness = completeness_other

        # --- Schema-Driven ---
        if handler in ("<inline>", "<unknown>"):
            driven_raw = "FALSE"
        else:
            driven_raw, _ = schema_map.get(handler, ("FALSE", "handler not mapped"))

        if _is_cloud:
            meshery_driven: Optional[str] = None
            cloud_driven: Optional[str] = driven_raw
        else:
            meshery_driven = driven_raw
            cloud_driven = None

        # --- Notes ---
        notes_completeness = completeness_this if completeness_this else "No Schema"
        notes_driven = driven_raw if driven_raw else ""
        notes = _build_actionable_notes(
            coverage=coverage, status=status, is_commented=is_commented,
            compl_notes=compl_notes_this, completeness=notes_completeness,
            driven=notes_driven, repo_source=repo_source, path=path,
        )

        records.append(EndpointRecord(
            path=path, method=method, category=category,
            subcategory=subcategory, handler=handler,
            in_spec=method_in_spec, x_annotation=x_annotation,
            exists_in_meshery_router=exists_in_meshery,
            exists_in_cloud_router=exists_in_cloud,
            is_commented=is_commented,
            belongs_to_meshery=belongs_to_meshery,
            belongs_to_cloud=belongs_to_cloud,
            meshery_schema_backed=meshery_schema_backed,
            cloud_schema_backed=cloud_schema_backed,
            meshery_schema_completeness=meshery_completeness,
            cloud_schema_completeness=cloud_completeness,
            meshery_schema_driven=meshery_driven,
            cloud_schema_driven=cloud_driven,
            coverage=coverage, status=status,
            compl_notes=compl_notes_this, notes=notes,
            repo_source=repo_source,
        ))

    # ------------------------------------------------------------------
    # Pass 2: Spec-only endpoints (Schema Underlap)
    # ------------------------------------------------------------------
    for norm_path, spec_methods in sorted(all_paths.items()):
        original = original_paths.get(norm_path, norm_path)
        category, subcategory = categorize(original, path_categories)

        for method in sorted(spec_methods):
            if (norm_path, method) in router_norm_keys or (norm_path, "ALL") in router_norm_keys:
                continue

            xi = x_internal_map.get((norm_path, method), [])
            is_cloud_xi = "cloud" in xi
            is_meshery_xi = "meshery" in xi

            coverage = "Schema Underlap"
            status = "Cloud-only" if is_cloud_xi else "Unimplemented"

            if is_cloud_xi:
                x_annotation = "Cloud-only"
            elif is_meshery_xi:
                x_annotation = "Meshery"
            else:
                x_annotation = "None"

            belongs_to_meshery, belongs_to_cloud = derive_ownership(x_annotation)

            meshery_schema_backed = belongs_to_meshery
            cloud_schema_backed = belongs_to_cloud

            op = operations_map.get((norm_path, method))
            meshery_completeness_val: Optional[str] = None
            cloud_completeness_val: Optional[str] = None

            if meshery_schema_backed:
                if op:
                    meshery_completeness_val, _ = assess_completeness(op, method)
                else:
                    meshery_completeness_val = "Stub"

            if cloud_schema_backed:
                if op:
                    cloud_completeness_val, _ = assess_completeness(op, method)
                else:
                    cloud_completeness_val = "Stub"

            primary_completeness = meshery_completeness_val or cloud_completeness_val or "Stub"
            notes = _build_actionable_notes(
                coverage=coverage, status=status, is_commented=False,
                compl_notes=[], completeness=primary_completeness,
                driven="", repo_source="",
            )

            records.append(EndpointRecord(
                path=original, method=method, category=category,
                subcategory=subcategory, in_spec=True,
                x_annotation=x_annotation,
                belongs_to_meshery=belongs_to_meshery,
                belongs_to_cloud=belongs_to_cloud,
                meshery_schema_backed=meshery_schema_backed,
                cloud_schema_backed=cloud_schema_backed,
                meshery_schema_completeness=meshery_completeness_val,
                cloud_schema_completeness=cloud_completeness_val,
                coverage=coverage, status=status, notes=notes,
            ))

    return sorted(records, key=endpoint_sort_key)


# ---------------------------------------------------------------------------
# Multi-repo merge
# ---------------------------------------------------------------------------

_COV_RANK: Dict[str, int] = {"Overlap": 3, "Server Underlap": 2, "Schema Underlap": 1}


def merge_endpoint_lists(
    meshery_eps: List[EndpointRecord],
    cloud_eps: List[EndpointRecord],
) -> List[EndpointRecord]:
    """Produce a single unified endpoint record list from meshery + cloud."""
    meshery_by_key: Dict[Tuple[str, str], EndpointRecord] = {}
    for rec in meshery_eps:
        norm = normalize_path(rec.path)
        key = (norm, rec.method)
        if key not in meshery_by_key or (
            _COV_RANK.get(rec.coverage, 0) > _COV_RANK.get(meshery_by_key[key].coverage, 0)
        ):
            meshery_by_key[key] = rec

    cloud_by_key: Dict[Tuple[str, str], EndpointRecord] = {}
    for rec in cloud_eps:
        norm = normalize_path(rec.path)
        key = (norm, rec.method)
        if key not in cloud_by_key or (
            _COV_RANK.get(rec.coverage, 0) > _COV_RANK.get(cloud_by_key[key].coverage, 0)
        ):
            cloud_by_key[key] = rec

    merged: List[EndpointRecord] = []
    all_keys = sorted(set(meshery_by_key) | set(cloud_by_key))

    for key in all_keys:
        m_rec = meshery_by_key.get(key)
        c_rec = cloud_by_key.get(key)

        if m_rec and not c_rec:
            merged.append(m_rec)
            continue
        if c_rec and not m_rec:
            merged.append(c_rec)
            continue

        if _COV_RANK.get(m_rec.coverage, 0) >= _COV_RANK.get(c_rec.coverage, 0):
            primary = m_rec
        else:
            primary = c_rec

        def _prefer_raw(a: Optional[str], b: Optional[str]) -> Optional[str]:
            return a if a is not None else b

        m_notes = m_rec.notes.strip()
        c_notes = c_rec.notes.strip()
        if m_notes == c_notes:
            combined_notes = m_notes
        elif not m_notes:
            combined_notes = c_notes
        elif not c_notes:
            combined_notes = m_notes
        else:
            combined_notes = f"[Meshery Server]\n{m_notes}\n\n[Meshery Cloud]\n{c_notes}"

        rec = EndpointRecord(
            path=primary.path, method=primary.method,
            category=primary.category, subcategory=primary.subcategory,
            handler=primary.handler,
            in_spec=m_rec.in_spec or c_rec.in_spec,
            x_annotation=primary.x_annotation,
            exists_in_meshery_router=m_rec.exists_in_meshery_router or c_rec.exists_in_meshery_router,
            exists_in_cloud_router=m_rec.exists_in_cloud_router or c_rec.exists_in_cloud_router,
            is_commented=primary.is_commented,
            belongs_to_meshery=m_rec.belongs_to_meshery or c_rec.belongs_to_meshery,
            belongs_to_cloud=m_rec.belongs_to_cloud or c_rec.belongs_to_cloud,
            meshery_schema_backed=m_rec.meshery_schema_backed or c_rec.meshery_schema_backed,
            cloud_schema_backed=m_rec.cloud_schema_backed or c_rec.cloud_schema_backed,
            meshery_schema_completeness=_prefer_raw(
                m_rec.meshery_schema_completeness, c_rec.meshery_schema_completeness
            ),
            cloud_schema_completeness=_prefer_raw(
                c_rec.cloud_schema_completeness, m_rec.cloud_schema_completeness
            ),
            meshery_schema_driven=m_rec.meshery_schema_driven,
            cloud_schema_driven=c_rec.cloud_schema_driven,
            coverage=primary.coverage, status=primary.status,
            compl_notes=primary.compl_notes, notes=combined_notes,
            repo_source="both",
        )
        merged.append(rec)

    return sorted(merged, key=endpoint_sort_key)
