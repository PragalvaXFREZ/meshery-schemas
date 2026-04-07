"""Go AST analyzer bridge and per-repo analysis setup."""

import json
import subprocess
import sys

from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

from .config import HANDLERS_DIR, PROVIDER_BYTES_SENTINEL, RepoConfig
from .routes import (
    explode_routes_to_per_verb,
    merge_comment_routes,
    routes_from_go_analysis,
    scan_commented_gorilla_routes,
)


def run_go_analyzer(
    repo: Path,
    config: Optional[RepoConfig] = None,
) -> Dict[str, Any]:
    """Run the Go AST helper and return its parsed JSON output.

    The helper (build/scripts/analyze_handlers/main.go) uses go/ast to extract:
      - Per-handler schema import usage and request/response types
      - Transitive type aliases from local models to schema packages
      - JSON struct field names from handlers, models, and schemas models
    """
    schemas_root = Path(__file__).resolve().parents[3]
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
    # Pass router file for Go AST route extraction
    if config and config.router_file:
        router_path = repo / config.router_file
        if router_path.exists():
            cmd += [
                "--router-file", str(router_path),
                "--router-dialect", config.router_dialect,
            ]

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
            "ERROR: 'go' not found — Go AST analyzer requires Go to be installed",
            file=sys.stderr,
        )
    except subprocess.CalledProcessError as exc:
        print(
            f"ERROR: Go analyzer exited with code {exc.returncode}\n"
            f"  stderr: {exc.stderr.strip()[:200]}",
            file=sys.stderr,
        )
    except (json.JSONDecodeError, ValueError) as exc:
        print(
            f"ERROR: Go analyzer output could not be parsed: {exc}",
            file=sys.stderr,
        )
    return {"handlers": {}, "type_aliases": {}, "struct_fields": {}}


def go_type_lookup_key(type_name: Optional[str]) -> Optional[str]:
    """Normalize a Go type string to the qualified lookup key emitted by the analyzer."""
    if not type_name or type_name == PROVIDER_BYTES_SENTINEL:
        return None

    key = type_name.strip()
    while key.startswith("*"):
        key = key[1:]
    while key.startswith("[]"):
        key = key[2:]
    return key.removesuffix("{}") or None


def apply_alias_struct_fields(
    go_fields_map: Dict[str, Set[str]],
    alias_targets: Dict[str, str],
) -> None:
    """Populate field lookup entries for local aliases of schema model types."""
    for alias, target in alias_targets.items():
        alias_key = go_type_lookup_key(alias)
        target_key = go_type_lookup_key(target)
        if not alias_key or not target_key or alias_key in go_fields_map:
            continue
        target_fields = go_fields_map.get(target_key)
        if target_fields:
            go_fields_map[alias_key] = set(target_fields)


def _prefer_type_with_fields(
    types: List[str],
    go_fields_map: Dict[str, Set[str]],
) -> Optional[str]:
    """Prefer an extracted type that can be cross-checked against fields."""
    for type_name in types:
        key = go_type_lookup_key(type_name)
        if key and go_fields_map.get(key):
            return type_name
    return types[0] if types else None


def upgrade_schema_map(
    schema_map: Dict[str, Tuple[str, str]],
    handler_io_map: Dict[str, Dict[str, Optional[str]]],
    type_aliases: Dict[str, str],
    schema_module: str = "github.com/meshery/schemas",
) -> Dict[str, Tuple[str, str]]:
    """Upgrade FALSE entries in schema_map using transitive type alias lookups.

    When a handler's I/O type (request or response) resolves through a local
    alias to a schema type, it should be classified TRUE/Partial, not FALSE.
    """
    if not type_aliases or not handler_io_map:
        return schema_map

    result = dict(schema_map)
    for handler, (status, _reason) in result.items():
        if status != "FALSE":
            continue
        io = handler_io_map.get(handler, {})
        for t in [io.get("request_type"), io.get("response_type")]:
            key = go_type_lookup_key(t)
            if not key:
                continue
            imp = type_aliases.get(key)
            if not imp:
                continue
            bare = key.rsplit(".", 1)[-1]
            rel = imp.replace(schema_module + "/", "")
            if "models/v" in rel:
                result[handler] = ("TRUE", f"alias: {bare} → {rel}")
                break
            if "models/core" in rel:
                result[handler] = ("Partial", f"alias: {bare} → models/core")
                # don't break — a versioned import on the other type would win

    return result


def setup_repo_analysis(
    repo_root: Path,
    repo_config: RepoConfig,
) -> Tuple[
    List[Dict[str, Any]],
    Dict[str, Tuple[str, str]],
    Dict[str, Dict[str, Optional[str]]],
    Dict[str, Set[str]],
    Dict[str, int],
]:
    """Parse routes and run handler analysis for one repo.

    Returns (routes, schema_map, handler_io_map, go_fields_map, analysis_stats).

    Routes are returned as **per-verb entries**: each route dict has a scalar
    ``"method"`` key (e.g. ``"GET"``) and ``"methods"`` is a one-element list
    kept for compatibility.
    """
    analysis = run_go_analyzer(repo_root, config=repo_config)

    # --- Routes (from Go AST; commented routes via lightweight regex scan) ---
    go_routes = analysis.get("routes")
    if not go_routes:
        print(
            "ERROR: Go AST analyzer returned no routes — "
            "ensure 'go' is installed and --router-file is correct.",
            file=sys.stderr,
        )
        sys.exit(1)
    routes = routes_from_go_analysis(go_routes)
    if repo_config.router_dialect == "gorilla":
        commented = scan_commented_gorilla_routes(repo_root, repo_config.router_file)
        routes = merge_comment_routes(routes, commented)

    # Explode multi-method routes into per-verb entries.
    routes = explode_routes_to_per_verb(routes)

    go_fields_map: Dict[str, Set[str]] = {
        name: set(fields) for name, fields in analysis["struct_fields"].items()
    }
    apply_alias_struct_fields(
        go_fields_map,
        analysis.get("type_alias_targets", {}),
    )

    # Build handler_io_map from the new plural request_types / response_types
    # fields, falling back to the legacy singular request_type / response_type.
    handler_io_map: Dict[str, Dict[str, Optional[str]]] = {}
    for name, info in analysis["handlers"].items():
        req_types = info.get("request_types") or []
        resp_types = info.get("response_types") or []
        # Legacy fallback: singular fields from older Go helper output
        if not req_types and info.get("request_type"):
            req_types = [info["request_type"]]
        if not resp_types and info.get("response_type"):
            resp_types = [info["response_type"]]
        file_abs = info.get("file", "")
        try:
            file_rel = str(Path(file_abs).relative_to(repo_root)) if file_abs else ""
        except ValueError:
            file_rel = file_abs
        handler_io_map[name] = {
            "request_type": _prefer_type_with_fields(req_types, go_fields_map),
            "response_type": _prefer_type_with_fields(resp_types, go_fields_map),
            "request_types": req_types,
            "response_types": resp_types,
            "body_read_via_readall": info.get("body_read_via_readall", False),
            "file": file_rel,
        }

    schema_map_direct: Dict[str, Tuple[str, str]] = {
        name: (info["schema_import_usage"], info["schema_reason"])
        for name, info in analysis["handlers"].items()
    }

    schema_map = upgrade_schema_map(
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
