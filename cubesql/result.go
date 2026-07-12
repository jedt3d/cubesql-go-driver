package cubesql

// Result captures connection metrics immediately after an execution. Metric
// failures are reported by the individual accessor so an already-applied SQL
// side effect is never misreported as an execution failure.
type Result struct {
	rowsAffected int64
	rowsError    error
	lastInsertID int64
	lastError    error
}

func (result Result) RowsAffected() (int64, error) {
	return result.rowsAffected, result.rowsError
}

func (result Result) LastInsertID() (int64, error) {
	return result.lastInsertID, result.lastError
}
