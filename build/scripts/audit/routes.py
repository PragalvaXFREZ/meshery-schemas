"""Route scanning and helpers.

Active routes come from the Go AST helper.  This module provides:
- Commented-route scanner (regex, Gorilla Mux only)
- Route merging and per-verb explosion utilities
"""

import re

from pathlib import Path
from typing import Any, Dict, List


def scan_commented_gorilla_routes(repo: Path, router_file_rel: str) -> List[Dict[str, Any]]:
    """Scan a Gorilla Mux router file for commented-out route registrations.

    The Go AST helper handles active routes.  Commented-out lines like
    ``// gMux.Handle("/path", h.Handler).Methods("GET")`` are not valid Go
    syntax, so the AST parser cannot see them.  This lightweight regex
    scanner fills that gap.

    Returns route dicts with ``commented: True``.
    """
    router_file = repo / router_file_rel
    if not router_file.exists():
        return []

    routes: List[Dict[str, Any]] = []
    for line in router_file.read_text(errors="replace").splitlines():
        stripped = line.strip()
        if not stripped.startswith("//"):
            continue
        clean = re.sub(r"^//\s*", "", stripped)
        path_m = re.search(
            r'gMux\.(Handle|HandleFunc|PathPrefix)\s*\(\s*"([^"]+)"', clean
        )
        if not path_m:
            continue
        path = path_m.group(2)
        methods_m = re.search(r"\.\s*Methods\(\s*(.+?)\s*\)", clean)
        methods = re.findall(r'"([A-Z]+)"', methods_m.group(1)) if methods_m else ["ALL"]
        # Extract handler name: last h.FuncName reference
        handler_m = re.findall(r"h\.([A-Z]\w+)", clean)
        handler = handler_m[-1] if handler_m else "<unknown>"
        routes.append({
            "path": path,
            "methods": sorted(methods),
            "handler": handler,
            "commented": True,
        })
    return routes


def routes_from_go_analysis(go_routes: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Convert Go AST route entries to the format expected by the audit pipeline."""
    routes = []
    for r in go_routes:
        methods = r.get("methods") or ["ALL"]
        handler = r.get("handler", "") or "<unknown>"
        routes.append({
            "path": r["path"],
            "methods": sorted(methods),
            "handler": handler,
            "commented": r.get("commented", False),
        })
    return routes


def merge_comment_routes(
    primary_routes: List[Dict[str, Any]],
    commented_routes: List[Dict[str, Any]],
) -> List[Dict[str, Any]]:
    """Append commented routes that aren't already in the primary list."""
    merged = list(primary_routes)
    seen = {
        (route["path"], tuple(route.get("methods", [])))
        for route in merged
    }
    for route in commented_routes:
        key = (route["path"], tuple(route.get("methods", [])))
        if key not in seen:
            seen.add(key)
            merged.append(route)
    return merged


def explode_routes_to_per_verb(routes: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Explode multi-method routes into per-verb entries.

    Input:  [{"path": "/foo", "methods": ["GET", "POST"], ...}]
    Output: [{"path": "/foo", "method": "GET", "methods": ["GET"], ...},
             {"path": "/foo", "method": "POST", "methods": ["POST"], ...}]
    """
    result = []
    for route in routes:
        for method in route["methods"]:
            entry = dict(route)
            entry["method"] = method
            entry["methods"] = [method]
            result.append(entry)
    return result
