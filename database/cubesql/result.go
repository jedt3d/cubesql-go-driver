package cubesql

import core "github.com/jedt3d/cubesql-go-driver/cubesql"

type execResult struct {
	native core.Result
}

func (result execResult) LastInsertId() (int64, error) {
	return result.native.LastInsertID()
}

func (result execResult) RowsAffected() (int64, error) {
	return result.native.RowsAffected()
}
