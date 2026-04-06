#!/usr/bin/env python3
"""
Meshery API Schema Audit

Compares data sources within meshery/meshery and meshery/schemas to produce
a comprehensive audit of every API endpoint:

  1. server/router/server.go    -> registered API endpoints
  2. _openapi_build/merged_openapi.yml -> authoritative OpenAPI spec (from this repo)
  3. server/handlers/*.go       -> schema-driven check (import analysis)

For each endpoint the script computes:
  - Coverage      -- Overlap / Server Underlap / Schema Underlap
  - Status        -- Active / Deprecated / Unimplemented / Cloud-only
  - Schema-Backed -- Is the endpoint defined in the OpenAPI spec?
  - Schema Completeness -- Full / Partial / Stub / N/A
  - Schema-Driven -- Does the handler import+use meshery/schemas types?

Optionally writes results to a Google Sheet. Credentials come from
environment variables -- never hardcoded.

Intended to be run via Make targets from the meshery-schemas repo root:

  make api-audit            # Dry-run audit -- prints summary only
  make api-audit-update     # Audit and write results to Google Sheet
  make api-audit-setup      # Install Python dependencies only

For advanced usage or CI, the script can be invoked directly:

  python build/scripts/api-audit.py --meshery-repo ../meshery --dry-run --verbose
  python build/scripts/api-audit.py --meshery-repo ../meshery --sheet-id $SHEET_ID
"""

import argparse
import os
import sys

from pathlib import Path
from typing import List

from audit.analyzer import setup_repo_analysis
from audit.classify import classify_endpoints, merge_endpoint_lists
from audit.config import (
    DEFAULT_SPEC_PATH,
    MESHERY_CLOUD_CONFIG,
    MESHERY_CONFIG,
)
from audit.models import format_record_for_sheet
from audit.openapi import parse_openapi
from audit.sheets import has_sheet_credentials_configured, prefetch_sheet_snapshot, update_sheet
from audit.summary import (
    collect_endpoint_summary,
    print_verbose_endpoints,
    render_audit_summary_table,
)


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
    if args.sheet_id and not args.dry_run:
        missing: List[str] = []
        if not args.meshery_repo and not args.cloud_repo:
            missing.append("MESHERY_REPO / --meshery-repo (or --cloud-repo)")
        if not has_sheet_credentials_configured():
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
        m_routes, m_schema_map, m_handler_io, m_go_fields, _m_stats = setup_repo_analysis(
            meshery_root, MESHERY_CONFIG
        )
        meshery_eps = classify_endpoints(
            m_routes, spec_data, m_schema_map,
            handler_io_map=m_handler_io,
            go_fields_map=m_go_fields,
            repo_source="meshery",
        )

        print("Analyzing Meshery Cloud...")
        c_routes, c_schema_map, c_handler_io, c_go_fields, _c_stats = setup_repo_analysis(
            cloud_root, MESHERY_CLOUD_CONFIG
        )
        cloud_eps = classify_endpoints(
            c_routes, spec_data, c_schema_map,
            handler_io_map=c_handler_io,
            go_fields_map=c_go_fields,
            repo_source="cloud",
        )

        endpoint_records = merge_endpoint_lists(meshery_eps, cloud_eps)

    else:
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
        routes, schema_map, handler_io_map, go_fields_map, _repo_stats = setup_repo_analysis(
            repo_root, repo_config
        )
        endpoint_records = classify_endpoints(
            routes, spec_data, schema_map,
            handler_io_map=handler_io_map,
            go_fields_map=go_fields_map,
            repo_source=repo_source,
        )

    # --- Single formatting pass: convert fact records to sheet-ready dicts ---
    endpoints = [format_record_for_sheet(rec) for rec in endpoint_records]

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
            print_verbose_endpoints(endpoints)
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
        print_verbose_endpoints(endpoints, include_coverage=True)
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
