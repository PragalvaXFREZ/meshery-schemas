"""Google Sheets integration: auth, diff, batch update, row insertion, highlighting."""

import json
import os
import sys

from collections import defaultdict
from datetime import datetime
from typing import Any, Dict, List, Optional, Set, Tuple

from .config import (
    AUDIT_WORKSHEET_INDEX,
    COL_CATEGORY,
    COL_CHANGELOG,
    COL_ENDPOINTS,
    COL_METHODS,
    COL_STATUS,
    COL_SUBCATEGORY,
    COL_X_ANNOTATED,
    COL_BACKED_MS,
    COL_BACKED_MC,
    COL_COMPLETENESS_MS,
    COL_COMPLETENESS_MC,
    COL_DRIVEN_MS,
    COL_DRIVEN_MC,
    COL_NOTES,
    SHEET_COLUMNS,
)
from .models import endpoint_sort_key, normalize_path


# ---------------------------------------------------------------------------
# Credentials
# ---------------------------------------------------------------------------

_GOOGLE_SCOPES = [
    "https://www.googleapis.com/auth/spreadsheets",
    "https://www.googleapis.com/auth/drive",
]


def _load_google_service_account_creds():
    """Load Google service account credentials from environment variables."""
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


def has_sheet_credentials_configured() -> bool:
    """Return True when sheet credentials are configured via env vars."""
    if os.environ.get("GOOGLE_CREDENTIALS_JSON"):
        return True
    creds_file = os.environ.get("GOOGLE_APPLICATION_CREDENTIALS")
    return bool(creds_file and os.path.exists(creds_file))


# ---------------------------------------------------------------------------
# Sheet helpers
# ---------------------------------------------------------------------------

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
            or endpoint_methods == sheet_methods
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
    """Reset all data rows (row 2 onward) to black text in one API call."""
    if n_rows <= 1:
        return False
    request = {
        "repeatCell": {
            "range": {
                "sheetId": worksheet_id,
                "startRowIndex": 1,
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


# ---------------------------------------------------------------------------
# Main sheet update
# ---------------------------------------------------------------------------

def update_sheet(
    endpoints: List[Dict[str, Any]],
    sheet_id: str,
    dry_run: bool = False,
    prefetched: Optional[Tuple[Any, Any, List[List[str]]]] = None,
) -> Dict[str, Any]:
    """Diff computed endpoints against the sheet and apply updates."""
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

    for ep in endpoints:
        method_str = ep.get("methods", "")
        matched_idx = _find_matching_row(sheet_index, ep["path"], method_str, matched_rows)

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
                ep["category"], ep["subcategory"], ep["path"], ep["methods"],
                ep["sheet_status"], ep["x_annotated"],
                ep["backed_ms"], ep["backed_mc"],
                ep["completeness_ms"], ep["completeness_mc"],
                ep["driven_ms"], ep["driven_mc"],
                ep["notes"], today,
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
                (ep["path"], ep["methods"], set(range(1, len(SHEET_COLUMNS) + 1)))
            )

    new_rows_info.sort(
        key=lambda item: endpoint_sort_key({
            "category": item[1], "subcategory": item[2],
            "path": item[0][COL_ENDPOINTS], "methods": item[0][COL_METHODS],
        })
    )

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

    inserted_rows = 0
    appended_rows = 0
    if not dry_run and new_rows_info:
        inserted_rows, appended_rows = _insert_rows_by_group(ws, new_rows_info, errors)

    highlighted_cells = 0
    if not dry_run and highlight_specs:
        refreshed_rows = ws.get_all_values()
        refreshed_index = _build_sheet_index(refreshed_rows)
        row_cols: Dict[int, Set[int]] = {}

        for path, methods, cols in highlight_specs:
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
            sheet, ws.id, resolved_targets, MAGENTA_TEXT_RGB, errors, "highlight",
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
    """Insert new rows into the correct category/sub-category block."""
    try:
        all_rows = ws.get_all_values()
    except Exception as exc:
        errors.append(f"INSERT ERROR (read failed): {exc}")
        return 0, 0

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
        has_endpoint = bool(
            row[COL_ENDPOINTS].strip() if len(row) > COL_ENDPOINTS else ""
        )
        if cat and has_endpoint:
            group_last_row[(cat, sub)] = idx
            cat_last_row[cat] = idx

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

    inserts.sort(key=lambda item: item[0], reverse=True)
    first_affected = len(all_rows)

    for insert_after, row_data in inserts:
        pos = insert_after + 1
        all_rows.insert(pos, row_data)
        first_affected = min(first_affected, pos)

    all_rows.extend(append_rows)

    inserted_rows = len(inserts)
    appended_rows = len(append_rows)
    if inserted_rows + appended_rows == 0:
        return 0, 0

    try:
        new_row_count = len(all_rows)
        if new_row_count > ws.row_count:
            ws.resize(rows=new_row_count)

        start_row = first_affected + 1
        updated_slice = all_rows[first_affected:]
        n_cols = max((len(r) for r in updated_slice), default=1)
        for i, r in enumerate(updated_slice):
            if len(r) < n_cols:
                updated_slice[i] = r + [""] * (n_cols - len(r))
        end_row = start_row + len(updated_slice) - 1
        cell_range = f"A{start_row}:{chr(64 + min(n_cols, 26))}{end_row}"
        if n_cols > 26:
            cell_range = ws.range(start_row, 1, end_row, n_cols)
            flat = [cell for row in updated_slice for cell in row]
            for i, cell_obj in enumerate(cell_range):
                cell_obj.value = flat[i]
            ws.update_cells(cell_range, value_input_option="RAW")
        else:
            ws.update(cell_range, updated_slice, value_input_option="RAW")
    except Exception as exc:
        errors.append(f"BATCH INSERT ERROR: {exc}")
        return 0, 0

    return inserted_rows, appended_rows


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
