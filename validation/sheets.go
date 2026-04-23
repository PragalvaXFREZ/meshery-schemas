package validation

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const sheetName = "Verification of API Endpoints - Combined"
const auditSummaryCellColumn = 2

func sheetRange(r string) string {
	return fmt.Sprintf("'%s'!%s", sheetName, r)
}

func sheetValuesRange() string {
	return fmt.Sprintf("'%s'", sheetName)
}

type auditSheetLayout struct {
	headerRow   int
	foundHeader bool
	generated   map[string]int
}

type rowOperationKind string

const (
	rowOperationInsert rowOperationKind = "insert"
	rowOperationDelete rowOperationKind = "delete"
	rowOperationMove   rowOperationKind = "move"
)

type sheetRowOperation struct {
	Kind             rowOperationKind
	StartIndex       int
	EndIndex         int
	DestinationIndex int
}

type sheetCellUpdate struct {
	Row    int
	Column int
	Value  string
}

type sheetUpdatePlan struct {
	MinRows     int
	MinCols     int
	RowOps      []sheetRowOperation
	CellUpdates []sheetCellUpdate
}

type auditSheetSummaryStats struct {
	ServerEndpoints                  int
	CloudEndpoints                   int
	MissingSchema                    int
	SchemaDrivenServer               int
	SchemaDrivenCloud                int
	PathParamDrift                   int
	UnimplementedEndpointsWithSchema int
}

type sheetRowSlot struct {
	Key reconcileKey
	Row ConsumerAuditRow
}

func ensureSheetExists(ctx context.Context, srv *sheets.Service, sheetID string) error {
	ss, err := srv.Spreadsheets.Get(sheetID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get spreadsheet: %w", err)
	}
	for _, sh := range ss.Sheets {
		if sh != nil && sh.Properties != nil && sh.Properties.Title == sheetName {
			return nil
		}
	}
	_, err = srv.Spreadsheets.BatchUpdate(sheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				AddSheet: &sheets.AddSheetRequest{
					Properties: &sheets.SheetProperties{Title: sheetName},
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("create sheet %q: %w", sheetName, err)
	}
	return nil
}

func sheetPropertiesByTitle(ctx context.Context, srv *sheets.Service, sheetID, title string) (*sheets.SheetProperties, error) {
	ss, err := srv.Spreadsheets.Get(sheetID).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get spreadsheet: %w", err)
	}
	for _, sh := range ss.Sheets {
		if sh != nil && sh.Properties != nil && sh.Properties.Title == title {
			return sh.Properties, nil
		}
	}
	return nil, fmt.Errorf("sheet %q not found", title)
}

func ensureManagedGridSize(ctx context.Context, srv *sheets.Service, sheetID string, minRows, minCols int) error {
	props, err := sheetPropertiesByTitle(ctx, srv, sheetID, sheetName)
	if err != nil {
		return err
	}
	if props.GridProperties == nil {
		props.GridProperties = &sheets.GridProperties{}
	}

	rowCount := int(props.GridProperties.RowCount)
	colCount := int(props.GridProperties.ColumnCount)
	if rowCount >= minRows && colCount >= minCols {
		return nil
	}

	newRows := rowCount
	if newRows < minRows {
		newRows = minRows
	}
	newCols := colCount
	if newCols < minCols {
		newCols = minCols
	}

	_, err = srv.Spreadsheets.BatchUpdate(sheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{
			{
				UpdateSheetProperties: &sheets.UpdateSheetPropertiesRequest{
					Properties: &sheets.SheetProperties{
						SheetId: props.SheetId,
						GridProperties: &sheets.GridProperties{
							RowCount:    int64(newRows),
							ColumnCount: int64(newCols),
						},
					},
					Fields: "gridProperties.rowCount,gridProperties.columnCount",
				},
			},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("resize sheet grid: %w", err)
	}
	return nil
}

// AuditedColumnValue returns the cell value of any generated column by its
// sheet-header name (e.g. "x-annotated"). Returns "" for an unknown column.
// The CLI uses it to render before/after diffs against the authoritative
// column list in generatedColumns.
func AuditedColumnValue(row ConsumerAuditRow, columnName string) string {
	for _, c := range generatedColumns {
		if c.Name == columnName {
			return c.get(row)
		}
	}
	return ""
}

// AuditedChangedColumns returns reconcile-significant generated columns whose
// values differ between two rows.
func AuditedChangedColumns(prev, cur ConsumerAuditRow) []string {
	return changedColumns(prev, cur)
}

// reconcileKey for reconciliation: (Endpoint, Method) per architecture §10.2.
type reconcileKey struct {
	Endpoint string
	Method   string
}

func keyOf(r ConsumerAuditRow) reconcileKey {
	key := looseMatchKey(r.Method, r.Endpoint)
	return reconcileKey{Endpoint: key.Path, Method: key.Method}
}

// reconcileOutput bundles the results of a reconcile pass: live rows that
// belong in the sheet body and endpoints removed in this run.
type reconcileOutput struct {
	Tracked      []TrackedEndpoint
	NewDeletions []DeletionRecord
}

// reconcile compares the current audit rows against a previous serialized
// view from Google Sheets. It is pure logic — no I/O — so it is fully testable.
func reconcile(current []ConsumerAuditRow, previous [][]string) reconcileOutput {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	prevRows := parseSheetRows(previous)
	prevByKey := make(map[reconcileKey]ConsumerAuditRow, len(prevRows))
	for _, r := range prevRows {
		prevByKey[keyOf(r)] = r
	}

	tracked := make([]TrackedEndpoint, 0, len(current))
	seen := make(map[reconcileKey]bool, len(current))

	for _, cur := range current {
		key := keyOf(cur)
		seen[key] = true
		prev, exists := prevByKey[key]
		if !exists {
			cur.ChangeLog = now
			tracked = append(tracked, TrackedEndpoint{Row: cur, State: StateNew})
			continue
		}
		cur = applyGeneratedNAOverrides(prev, cur)
		changed := changedColumns(prev, cur)
		if len(changed) == 0 {
			cur.ChangeLog = prev.ChangeLog
			tracked = append(tracked, TrackedEndpoint{Row: cur, State: StateExisting})
			continue
		}
		cur.ChangeLog = now
		cur = applyGeneratedNAOverrides(prev, cur)
		prevCopy := prev
		tracked = append(tracked, TrackedEndpoint{Row: cur, State: StateChanged, Prev: &prevCopy})
	}

	var newDeletions []DeletionRecord
	for _, r := range prevRows {
		if seen[keyOf(r)] {
			continue
		}
		rec := DeletionRecord{
			Endpoint:  r.Endpoint,
			Method:    r.Method,
			RemovedAt: now,
		}
		newDeletions = append(newDeletions, rec)
	}

	return reconcileOutput{
		Tracked:      tracked,
		NewDeletions: newDeletions,
	}
}

func applyGeneratedNAOverrides(prev, cur ConsumerAuditRow) ConsumerAuditRow {
	for _, col := range generatedColumns {
		if isNAOverride(col.get(prev)) {
			col.set(&cur, col.get(prev))
		}
	}
	return cur
}

func isNAOverride(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "n/a")
}

func defaultAuditSheetLayout() auditSheetLayout {
	generated := make(map[string]int, len(generatedColumns))
	for i, col := range generatedColumns {
		generated[col.Name] = i
	}
	return auditSheetLayout{
		generated: generated,
	}
}

func headerIndex(row []string) map[string]int {
	out := make(map[string]int, len(row))
	for i, cell := range row {
		name := strings.TrimSpace(cell)
		if name == "" {
			continue
		}
		if _, exists := out[name]; !exists {
			out[name] = i
		}
	}
	return out
}

func findAuditHeader(rows [][]string) (int, map[string]int, bool) {
	for i, row := range rows {
		index := headerIndex(row)
		matches := 0
		for _, col := range generatedColumns {
			if _, ok := index[col.Name]; ok {
				matches++
			}
		}
		if matches == len(generatedColumns) {
			return i, index, true
		}
	}
	return 0, nil, false
}

func findAuditHeaderRow(rows [][]string) (int, bool) {
	headerRow, _, ok := findAuditHeader(rows)
	return headerRow, ok
}

func auditLayout(rows [][]string) auditSheetLayout {
	layout := defaultAuditSheetLayout()
	headerRow, index, ok := findAuditHeader(rows)
	if !ok {
		return layout
	}
	layout.headerRow = headerRow
	layout.foundHeader = true
	for _, col := range generatedColumns {
		layout.generated[col.Name] = index[col.Name]
	}
	return layout
}

func auditHeaderStartRow(rows [][]string) int {
	return auditLayout(rows).headerRow + 1
}

// parseSheetRows accepts the raw [][]string we received from a sheet read.
func parseSheetRows(rows [][]string) []ConsumerAuditRow {
	if len(rows) == 0 {
		return nil
	}
	layout := auditLayout(rows)
	start := 0
	if layout.foundHeader {
		start = layout.headerRow + 1
	}
	out := make([]ConsumerAuditRow, 0, len(rows)-start)
	for _, r := range rows[start:] {
		if len(r) == 0 {
			continue
		}
		row := rowFromSheetRow(r, layout)
		out = append(out, row)
	}
	return out
}

func rowFromSheetRow(cols []string, layout auditSheetLayout) ConsumerAuditRow {
	get := func(i int) string {
		if i < len(cols) {
			return cols[i]
		}
		return ""
	}
	var row ConsumerAuditRow
	for _, col := range generatedColumns {
		col.set(&row, get(layout.generated[col.Name]))
	}
	return row
}

// changedColumns compares the reconcile-flagged columns of two rows and
// returns the names of any that differ. Only columns with Reconcile == true
// in generatedColumns trigger a StateChanged transition.
func changedColumns(a, b ConsumerAuditRow) []string {
	var changed []string
	for _, col := range generatedColumns {
		if !col.Reconcile {
			continue
		}
		if col.get(a) != col.get(b) {
			changed = append(changed, col.Name)
		}
	}
	return changed
}

// trackedToSheetRows converts a slice of TrackedEndpoints back into the
// [][]string shape that downstream sheet writers expect (header + rows).
func trackedToSheetRows(tracked []TrackedEndpoint) [][]string {
	rows := make([]ConsumerAuditRow, len(tracked))
	for i, t := range tracked {
		rows[i] = t.Row
	}
	return rowsToSheetRows(rows)
}

// rowsToSheetRows converts plain audit rows into the header+rows shape used by
// sheet writers.
func rowsToSheetRows(rows []ConsumerAuditRow) [][]string {
	out := make([][]string, 0, len(rows)+1)
	header := append([]string(nil), auditHeader...)
	out = append(out, header)
	for _, r := range rows {
		out = append(out, r.toRow())
	}
	return out
}

// readSheet pulls every value out of the combined audit sheet.
// The returned rows are exactly what reconcile expects.
func readSheet(ctx context.Context, srv *sheets.Service, sheetID string) ([][]string, error) {
	resp, err := srv.Spreadsheets.Values.Get(sheetID, sheetValuesRange()).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read sheet: %w", err)
	}
	rows := make([][]string, 0, len(resp.Values))
	for _, raw := range resp.Values {
		row := make([]string, 0, len(raw))
		for _, cell := range raw {
			row = append(row, fmt.Sprint(cell))
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// columnLetter converts a zero-based column index to its A1-notation letter
// (A=0, B=1, …, Z=25, AA=26).
func columnLetter(index int) string {
	if index < 0 {
		return ""
	}
	var out []byte
	for index >= 0 {
		out = append([]byte{byte('A' + index%26)}, out...)
		index = index/26 - 1
	}
	return string(out)
}

func bodyStartForLayout(layout auditSheetLayout) int {
	if layout.foundHeader {
		return layout.headerRow + 1
	}
	return layout.headerRow + 1
}

func previousBodyStartForLayout(layout auditSheetLayout) int {
	if layout.foundHeader {
		return layout.headerRow + 1
	}
	return 0
}

func previousSheetSlots(previous [][]string, layout auditSheetLayout) []sheetRowSlot {
	start := previousBodyStartForLayout(layout)
	if start >= len(previous) {
		return nil
	}
	slots := make([]sheetRowSlot, 0, len(previous)-start)
	for _, raw := range previous[start:] {
		row := rowFromSheetRow(raw, layout)
		slots = append(slots, sheetRowSlot{Key: keyOf(row), Row: row})
	}
	return slots
}

func desiredRowsFromTracked(tracked []TrackedEndpoint) []ConsumerAuditRow {
	rows := make([]ConsumerAuditRow, len(tracked))
	for i, t := range tracked {
		rows[i] = t.Row
	}
	return rows
}

func desiredKeyCounts(rows []ConsumerAuditRow) map[reconcileKey]int {
	counts := make(map[reconcileKey]int, len(rows))
	for _, row := range rows {
		counts[keyOf(row)]++
	}
	return counts
}

func planSheetUpdate(previous [][]string, tracked []TrackedEndpoint) sheetUpdatePlan {
	return planSheetUpdateWithSummary(previous, tracked, "")
}

func planSheetUpdateWithSummary(previous [][]string, tracked []TrackedEndpoint, summary string) sheetUpdatePlan {
	originalRowCount := len(previous)
	layout := auditLayout(previous)
	insertedTopRows := 0
	if summary != "" && layout.foundHeader && layout.headerRow == 0 {
		previous = prependEmptyRows(previous, 1)
		layout = auditLayout(previous)
		insertedTopRows = 1
	}
	if summary != "" && !layout.foundHeader {
		layout.headerRow = 1
	}

	desiredRows := desiredRowsFromTracked(tracked)
	bodyStart := bodyStartForLayout(layout)
	maxManagedColumn := len(generatedColumns) - 1
	for _, col := range generatedColumns {
		maxManagedColumn = max(maxManagedColumn, layout.generated[col.Name])
	}

	plan := sheetUpdatePlan{
		MinRows: max(1, bodyStart+len(desiredRows)),
		MinCols: max(len(generatedColumns), maxManagedColumn+1),
	}
	if summary != "" {
		plan.MinCols = max(plan.MinCols, auditSummaryCellColumn+1)
	}
	if insertedTopRows > 0 {
		plan.RowOps = append(plan.RowOps, sheetRowOperation{
			Kind:       rowOperationInsert,
			StartIndex: 0,
			EndIndex:   insertedTopRows,
		})
	}
	if !layout.foundHeader && len(previous) > 0 {
		insertRows := layout.headerRow + 1
		plan.RowOps = append(plan.RowOps, sheetRowOperation{
			Kind:       rowOperationInsert,
			StartIndex: 0,
			EndIndex:   insertRows,
		})
		insertedTopRows += insertRows
	}

	desiredCounts := desiredKeyCounts(desiredRows)
	keptCounts := make(map[reconcileKey]int, len(desiredCounts))
	previousSlots := previousSheetSlots(previous, layout)
	current := make([]sheetRowSlot, 0, len(previousSlots))
	for i, slot := range previousSlots {
		keep := false
		if slot.Key.Endpoint != "" || slot.Key.Method != "" {
			if keptCounts[slot.Key] < desiredCounts[slot.Key] {
				keep = true
				keptCounts[slot.Key]++
			}
		}
		if keep {
			current = append(current, slot)
			continue
		}
		plan.RowOps = append(plan.RowOps, sheetRowOperation{
			Kind:       rowOperationDelete,
			StartIndex: bodyStart + i,
			EndIndex:   bodyStart + i + 1,
		})
	}
	sort.SliceStable(plan.RowOps, func(i, j int) bool {
		if plan.RowOps[i].Kind == rowOperationInsert && plan.RowOps[i].StartIndex == 0 {
			return true
		}
		if plan.RowOps[j].Kind == rowOperationInsert && plan.RowOps[j].StartIndex == 0 {
			return false
		}
		if plan.RowOps[i].Kind == rowOperationDelete && plan.RowOps[j].Kind == rowOperationDelete {
			return plan.RowOps[i].StartIndex > plan.RowOps[j].StartIndex
		}
		return false
	})

	desiredByIndex := make([]sheetRowSlot, len(desiredRows))
	for i, row := range desiredRows {
		desiredByIndex[i] = sheetRowSlot{Key: keyOf(row), Row: row}
	}

	for i, desired := range desiredByIndex {
		if i < len(current) && current[i].Key == desired.Key {
			continue
		}
		found := -1
		for j := i + 1; j < len(current); j++ {
			if current[j].Key == desired.Key {
				found = j
				break
			}
		}
		if found >= 0 {
			plan.RowOps = append(plan.RowOps, sheetRowOperation{
				Kind:             rowOperationMove,
				StartIndex:       bodyStart + found,
				EndIndex:         bodyStart + found + 1,
				DestinationIndex: bodyStart + i,
			})
			moved := current[found]
			copy(current[i+1:found+1], current[i:found])
			current[i] = moved
			continue
		}
		plan.RowOps = append(plan.RowOps, sheetRowOperation{
			Kind:       rowOperationInsert,
			StartIndex: bodyStart + i,
			EndIndex:   bodyStart + i + 1,
		})
		current = append(current, sheetRowSlot{})
		copy(current[i+1:], current[i:])
		current[i] = sheetRowSlot{Key: desired.Key}
	}

	plan.CellUpdates = append(plan.CellUpdates, headerCellUpdates(layout)...)
	plan.CellUpdates = append(plan.CellUpdates, summaryCellUpdates(previous, summary, insertedTopRows)...)
	for i, desired := range desiredRows {
		var previousRow *ConsumerAuditRow
		if i < len(current) && current[i].Key == keyOf(desired) && (current[i].Key.Endpoint != "" || current[i].Key.Method != "") {
			previousRow = &current[i].Row
		}
		plan.CellUpdates = append(plan.CellUpdates, rowCellUpdates(bodyStart+i, layout, previousRow, desired)...)
	}

	plan.MinRows = max(plan.MinRows, originalRowCount+insertedTopRows)
	return plan
}

func prependEmptyRows(rows [][]string, count int) [][]string {
	if count <= 0 {
		return rows
	}
	out := make([][]string, 0, len(rows)+count)
	for i := 0; i < count; i++ {
		out = append(out, nil)
	}
	out = append(out, rows...)
	return out
}

func headerCellUpdates(layout auditSheetLayout) []sheetCellUpdate {
	if layout.foundHeader {
		return nil
	}
	updates := make([]sheetCellUpdate, 0, len(generatedColumns))
	for _, col := range generatedColumns {
		updates = append(updates, sheetCellUpdate{
			Row:    layout.headerRow,
			Column: layout.generated[col.Name],
			Value:  col.Name,
		})
	}
	return updates
}

func summaryCellUpdates(previous [][]string, summary string, insertedTopRows int) []sheetCellUpdate {
	if summary == "" {
		return nil
	}
	previousValue := ""
	if insertedTopRows == 0 && len(previous) > 0 && len(previous[0]) > auditSummaryCellColumn {
		previousValue = previous[0][auditSummaryCellColumn]
	}
	if previousValue == summary {
		return nil
	}
	return []sheetCellUpdate{{
		Row:    0,
		Column: auditSummaryCellColumn,
		Value:  summary,
	}}
}

func rowCellUpdates(rowIndex int, layout auditSheetLayout, previous *ConsumerAuditRow, desired ConsumerAuditRow) []sheetCellUpdate {
	var updates []sheetCellUpdate
	for _, col := range generatedColumns {
		previousValue := ""
		if previous != nil {
			previousValue = col.get(*previous)
		}
		desiredValue := col.get(desired)
		if previousValue == desiredValue {
			continue
		}
		updates = append(updates, sheetCellUpdate{
			Row:    rowIndex,
			Column: layout.generated[col.Name],
			Value:  desiredValue,
		})
	}
	return updates
}

func valueRangesFromCellUpdates(updates []sheetCellUpdate) []*sheets.ValueRange {
	if len(updates) == 0 {
		return nil
	}
	sort.SliceStable(updates, func(i, j int) bool {
		if updates[i].Column != updates[j].Column {
			return updates[i].Column < updates[j].Column
		}
		return updates[i].Row < updates[j].Row
	})

	var ranges []*sheets.ValueRange
	for i := 0; i < len(updates); {
		col := updates[i].Column
		startRow := updates[i].Row
		values := [][]any{{updates[i].Value}}
		endRow := startRow
		i++
		for i < len(updates) && updates[i].Column == col && updates[i].Row == endRow+1 {
			values = append(values, []any{updates[i].Value})
			endRow = updates[i].Row
			i++
		}
		colName := columnLetter(col)
		a1 := fmt.Sprintf("%s%d", colName, startRow+1)
		if endRow != startRow {
			a1 = fmt.Sprintf("%s%d:%s%d", colName, startRow+1, colName, endRow+1)
		}
		ranges = append(ranges, &sheets.ValueRange{
			Range:  sheetRange(a1),
			Values: values,
		})
	}
	return ranges
}

func buildAuditSheetSummary(previousRows, currentRows []ConsumerAuditRow) string {
	prev := auditSheetSummaryStatsFromRows(previousRows)
	cur := auditSheetSummaryStatsFromRows(currentRows)

	return strings.Join([]string{
		fmt.Sprintf("Total Endpoints: Server %s, Cloud %s",
			formatCountWithDelta(cur.ServerEndpoints, prev.ServerEndpoints),
			formatCountWithDelta(cur.CloudEndpoints, prev.CloudEndpoints)),
		fmt.Sprintf("Missing schema: %s", formatCountWithDelta(cur.MissingSchema, prev.MissingSchema)),
		fmt.Sprintf("Schema Driven - Server: %s", formatCountWithDelta(cur.SchemaDrivenServer, prev.SchemaDrivenServer)),
		fmt.Sprintf("Schema Driven - Cloud: %s", formatCountWithDelta(cur.SchemaDrivenCloud, prev.SchemaDrivenCloud)),
		fmt.Sprintf("Path/param drift: %s", formatCountWithDelta(cur.PathParamDrift, prev.PathParamDrift)),
		fmt.Sprintf("Unimplemented endpoints with schema: %s", formatCountWithDelta(cur.UnimplementedEndpointsWithSchema, prev.UnimplementedEndpointsWithSchema)),
	}, "\n")
}

func formatCountWithDelta(current, previous int) string {
	delta := current - previous
	if delta == 0 {
		return fmt.Sprintf("%d", current)
	}
	return fmt.Sprintf("%d (%+d)", current, delta)
}

func auditSheetSummaryStatsFromRows(rows []ConsumerAuditRow) auditSheetSummaryStats {
	var stats auditSheetSummaryStats
	for _, row := range rows {
		serverActive := activeInConsumer(row.EndpointStatus, "meshery")
		cloudActive := activeInConsumer(row.EndpointStatus, "cloud")
		if serverActive {
			stats.ServerEndpoints++
		}
		if cloudActive {
			stats.CloudEndpoints++
		}

		if row.XAnnotated == XAnnotatedNoSchema {
			stats.MissingSchema++
			continue
		}

		if appliesTo(row, "meshery") {
			if row.SchemaDrivenMeshery == auditStatusTrue {
				stats.SchemaDrivenServer++
			}
		}
		if appliesTo(row, "cloud") {
			if row.SchemaDrivenCloud == auditStatusTrue {
				stats.SchemaDrivenCloud++
			}
		}
		if unimplementedWithSchemaInConsumer(row, "meshery") {
			stats.UnimplementedEndpointsWithSchema++
		}
		if unimplementedWithSchemaInConsumer(row, "cloud") {
			stats.UnimplementedEndpointsWithSchema++
		}
		if value := strings.TrimSpace(row.PathDrift); value != "" && !isNAOverride(value) {
			stats.PathParamDrift++
		}
	}
	return stats
}

func unimplementedWithSchemaInConsumer(row ConsumerAuditRow, consumer string) bool {
	return appliesTo(row, consumer) && !activeInConsumer(row.EndpointStatus, consumer)
}

func activeInConsumer(endpointStatus, consumer string) bool {
	switch consumer {
	case "meshery":
		switch endpointStatus {
		case EndpointStatusActiveBoth,
			EndpointStatusActiveMesheryServer,
			EndpointStatusActiveMesheryServerMissingCloud:
			return true
		}
	case "cloud":
		switch endpointStatus {
		case EndpointStatusActiveBoth,
			EndpointStatusActiveMesheryCloud,
			EndpointStatusActiveMesheryCloudMissingServer:
			return true
		}
	}
	return false
}

func appliesTo(row ConsumerAuditRow, consumer string) bool {
	switch consumer {
	case "meshery":
		return row.XAnnotated == XAnnotatedMesheryOnly || row.XAnnotated == XAnnotatedBoth
	case "cloud":
		return row.XAnnotated == XAnnotatedCloudOnly || row.XAnnotated == XAnnotatedBoth
	default:
		return false
	}
}

func rowOperationRequests(sheetID int64, ops []sheetRowOperation) []*sheets.Request {
	requests := make([]*sheets.Request, 0, len(ops))
	for _, op := range ops {
		switch op.Kind {
		case rowOperationInsert:
			requests = append(requests, &sheets.Request{
				InsertDimension: &sheets.InsertDimensionRequest{
					Range: &sheets.DimensionRange{
						SheetId:    sheetID,
						Dimension:  "ROWS",
						StartIndex: int64(op.StartIndex),
						EndIndex:   int64(op.EndIndex),
						ForceSendFields: []string{
							"StartIndex",
							"EndIndex",
						},
					},
					InheritFromBefore: op.StartIndex > 0,
				},
			})
		case rowOperationDelete:
			requests = append(requests, &sheets.Request{
				DeleteDimension: &sheets.DeleteDimensionRequest{
					Range: &sheets.DimensionRange{
						SheetId:    sheetID,
						Dimension:  "ROWS",
						StartIndex: int64(op.StartIndex),
						EndIndex:   int64(op.EndIndex),
						ForceSendFields: []string{
							"StartIndex",
							"EndIndex",
						},
					},
				},
			})
		case rowOperationMove:
			requests = append(requests, &sheets.Request{
				MoveDimension: &sheets.MoveDimensionRequest{
					Source: &sheets.DimensionRange{
						SheetId:    sheetID,
						Dimension:  "ROWS",
						StartIndex: int64(op.StartIndex),
						EndIndex:   int64(op.EndIndex),
						ForceSendFields: []string{
							"StartIndex",
							"EndIndex",
						},
					},
					DestinationIndex: int64(op.DestinationIndex),
					ForceSendFields: []string{
						"DestinationIndex",
					},
				},
			})
		}
	}
	return requests
}

// writeSheet updates only the rows and cells that differ from the reconciled
// audit state. Structural edits move, insert, or delete whole rows so
// user-owned cells stay attached to their endpoint identity.
func writeSheet(ctx context.Context, srv *sheets.Service, sheetID string, previous [][]string, tracked []TrackedEndpoint, summary string) error {
	plan := planSheetUpdateWithSummary(previous, tracked, summary)
	if err := ensureManagedGridSize(ctx, srv, sheetID, plan.MinRows, plan.MinCols); err != nil {
		return fmt.Errorf("ensure managed sheet grid size: %w", err)
	}

	if len(plan.RowOps) > 0 {
		props, err := sheetPropertiesByTitle(ctx, srv, sheetID, sheetName)
		if err != nil {
			return err
		}
		requests := rowOperationRequests(props.SheetId, plan.RowOps)
		if len(requests) > 0 {
			_, err = srv.Spreadsheets.BatchUpdate(sheetID, &sheets.BatchUpdateSpreadsheetRequest{
				Requests: requests,
			}).Context(ctx).Do()
			if err != nil {
				return fmt.Errorf("apply sheet row operations: %w", err)
			}
		}
	}

	data := valueRangesFromCellUpdates(plan.CellUpdates)
	if len(data) > 0 {
		_, err := srv.Spreadsheets.Values.BatchUpdate(sheetID, &sheets.BatchUpdateValuesRequest{
			ValueInputOption: "RAW",
			Data:             data,
		}).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("update managed sheet cells: %w", err)
		}
	}
	return nil
}

// newSheetsService builds a Google Sheets client from a JSON credentials blob.
// It expects either service-account credentials or any other format
// google.CredentialsFromJSON understands.
func newSheetsService(ctx context.Context, creds []byte) (*sheets.Service, error) {
	if len(creds) == 0 {
		return nil, fmt.Errorf("consumer-audit: empty Google credentials")
	}
	gc, err := google.CredentialsFromJSON(ctx, creds, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	srv, err := sheets.NewService(ctx, option.WithCredentials(gc))
	if err != nil {
		return nil, fmt.Errorf("sheets client: %w", err)
	}
	return srv, nil
}

// reconcileFromOpts applies the requested reconciliation flow:
//   - SheetID set → read sheet, reconcile, write sheet, install tracked rows
//   - neither     → no-op
func reconcileFromOpts(opts ConsumerAuditOptions, result *ConsumerAuditResult) error {
	if result == nil {
		return nil
	}

	if opts.SheetID != "" {
		// Cap each Sheets round-trip so a stalled Google API call cannot
		// hang CI or local runs indefinitely.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		srv, err := newSheetsService(ctx, opts.SheetsCredentials)
		if err != nil {
			return err
		}
		if err := ensureSheetExists(ctx, srv, opts.SheetID); err != nil {
			return err
		}
		previous, err := readSheet(ctx, srv, opts.SheetID)
		if err != nil {
			return err
		}
		previousRows := parseSheetRows(previous)
		out := reconcile(result.Rows, previous)
		summary := buildAuditSheetSummary(previousRows, desiredRowsFromTracked(out.Tracked))
		if err := writeSheet(ctx, srv, opts.SheetID, previous, out.Tracked, summary); err != nil {
			return err
		}
		result.Tracked = out.Tracked
		result.NewDeletions = out.NewDeletions
		return nil
	}

	return nil
}
