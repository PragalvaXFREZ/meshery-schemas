"""OpenAPI spec parser and structural completeness assessment."""

import sys

from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

try:
    import yaml
except ImportError:
    sys.exit("Missing dependency: pip install pyyaml")

from .config import HTTP_METHODS
from .models import normalize_path


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


def assess_completeness(operation: dict, method: str) -> Tuple[str, List[str]]:
    """Assess schema completeness for a single OpenAPI operation.

    Returns (completeness, detail_notes) where detail_notes lists every
    specific finding (missing fields, bare types, property gaps, etc.).
    """
    notes: List[str] = []

    # Derive whether a request body is expected from the spec itself —
    # not from the HTTP method name. The spec is the source of truth.
    req_body_spec = operation.get("requestBody", {})
    expects_body = bool(isinstance(req_body_spec, dict) and req_body_spec)

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
    if expects_body:
        if "$ref" in req_body_spec:
            request_meaningful = True
            ref = req_body_spec["$ref"].rsplit("/", 1)[-1]
            notes.append(f"requestBody: references {ref}")
        else:
            req_content = req_body_spec.get("content", {})
            req_schema = _get_content_schema(req_content)
            request_meaningful = _has_meaningful_schema(req_schema)
            notes.extend(_describe_schema(req_schema, "requestBody"))

    # --- Response side ---
    response_meaningful = False
    responses = operation.get("responses", {})

    if isinstance(responses, dict):
        for code, resp in responses.items():
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

    if not any(str(c).startswith("2") for c in responses or {}):
        notes.append("no 2xx success response defined")

    # --- Classify ---
    if expects_body:
        if request_meaningful and response_meaningful:
            return "Full", notes
        if request_meaningful or response_meaningful:
            return "Partial", notes
        return "Stub", notes
    else:
        if response_meaningful:
            return "Full", notes
        return "Stub", notes


def _build_tag_category_map(doc: dict) -> Dict[str, Tuple[str, str]]:
    """Build a mapping from OpenAPI tag name to (category, subcategory)."""
    tag_display: Dict[str, str] = {}
    for tag_def in doc.get("tags", []):
        if isinstance(tag_def, dict) and "name" in tag_def:
            display = tag_def.get("x-displayName", tag_def["name"])
            tag_display[tag_def["name"]] = display

    tag_to_category: Dict[str, Tuple[str, str]] = {}
    for group in doc.get("x-tagGroups", []):
        if not isinstance(group, dict):
            continue
        group_name = group.get("name", "Other")
        for tag_name in group.get("tags", []):
            display = tag_display.get(tag_name, tag_name)
            tag_to_category[tag_name] = (group_name, display)

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

            if norm not in original_paths:
                original_paths[norm] = path

            if norm not in path_categories:
                op_tags = details.get("tags", [])
                if isinstance(op_tags, list):
                    for tag_name in op_tags:
                        if tag_name in tag_to_category:
                            path_categories[norm] = tag_to_category[tag_name]
                            break

            xi = details.get("x-internal", [])
            if not isinstance(xi, list):
                xi = [xi] if xi else []
            x_internal[(norm, m_upper)] = xi

            comp, cnotes = assess_completeness(details, method)
            completeness[(norm, m_upper)] = comp
            compl_notes[(norm, m_upper)] = cnotes

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
# Spec schema field extraction (for cross-check completeness)
# ---------------------------------------------------------------------------

def collect_property_names(schema: Any) -> Set[str]:
    """Recursively collect property names from an OpenAPI schema."""
    if not isinstance(schema, dict):
        return set()

    props = set()

    if "properties" in schema and isinstance(schema["properties"], dict):
        props.update(schema["properties"].keys())

    for combo in ("allOf", "oneOf", "anyOf"):
        if combo in schema and isinstance(schema[combo], list):
            for sub in schema[combo]:
                props.update(collect_property_names(sub))

    if schema.get("type") == "array" and isinstance(
        schema.get("items"), dict
    ):
        props.update(collect_property_names(schema["items"]))

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
                    req_fields = collect_property_names(media_obj["schema"])
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
                        resp_fields = collect_property_names(
                            media_obj["schema"]
                        )
                        break
            break  # use first 2xx only

    return {"request_fields": req_fields, "response_fields": resp_fields}
