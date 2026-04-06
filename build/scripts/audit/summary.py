"""CLI summary tables and endpoint rendering."""

from typing import Any, Dict, List, Optional, Set, Tuple

from .config import SUMMARY_KEYS, SUMMARY_TABLE_ROWS


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


def _is_complete_value(value: str) -> bool:
    return value == "True"


def _is_driven_value(value: str) -> bool:
    return value in {"True", "Partial"}


def collect_endpoint_summary(
    endpoints: List[Dict[str, Any]],
    spec_data: Dict[str, Any],
) -> Dict[str, Any]:
    """Collect final CLI summary counts from spec and computed endpoint rows.

    Summary semantics:
      - Endpoints (Spec) comes from the bundled spec only.
      - Endpoints (Router) and all other metrics come from route-aware analysis.
    """
    summary: Dict[str, Any] = {
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

        _ms_backed = ep.get("backed_ms") if _ms_present else None
        _mc_backed = ep.get("backed_mc") if _mc_present else None
        _backed_vals = {v for v in (_ms_backed, _mc_backed) if v and v != "-"}
        if "True" in _backed_vals:
            total["schema_backed"] += 1
        elif "False" in _backed_vals:
            total["not_schema_backed"] += 1

        _ms_compl = ep.get("completeness_ms") if _ms_present else None
        _mc_compl = ep.get("completeness_mc") if _mc_present else None
        if _is_complete_value(_ms_compl or "") or _is_complete_value(_mc_compl or ""):
            total["complete"] += 1
        elif _ms_compl == "False" or _mc_compl == "False":
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
            if ep.get("backed_ms") == "True":
                meshery["schema_backed"] += 1
            elif ep.get("backed_ms") == "False":
                meshery["not_schema_backed"] += 1
            elif ep.get("backed_ms") != "-":
                meshery["no_schema"] += 1
            if _is_complete_value(ep.get("completeness_ms", "")):
                meshery["complete"] += 1
            elif ep.get("completeness_ms") == "False":
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
            if ep.get("backed_mc") == "True":
                cloud["schema_backed"] += 1
            elif ep.get("backed_mc") == "False":
                cloud["not_schema_backed"] += 1
            elif ep.get("backed_mc") != "-":
                cloud["no_schema"] += 1
            if _is_complete_value(ep.get("completeness_mc", "")):
                cloud["complete"] += 1
            elif ep.get("completeness_mc") == "False":
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


def print_verbose_endpoints(
    endpoints: List[Dict[str, Any]],
    include_coverage: bool = False,
) -> None:
    """Print per-endpoint details in verbose mode."""
    print("\nDetailed endpoint output:")
    for ep in endpoints:
        cov_part = f"cov={ep['coverage']:16s} " if include_coverage else ""
        print(
            f"  {ep['path']:55s} [{ep['methods']:20s}] "
            f"{cov_part}st={ep['status']:14s} "
            f"bk_ms={ep['backed_ms']:5s} bk_mc={ep['backed_mc']:5s} "
            f"comp_ms={ep['completeness_ms']:7s} comp_mc={ep['completeness_mc']:7s} "
            f"drv_ms={ep['driven_ms']:7s} drv_mc={ep['driven_mc']:7s}"
        )
