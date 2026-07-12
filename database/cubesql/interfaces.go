package cubesql

import "database/sql/driver"

var (
	_ driver.Driver                         = (*Driver)(nil)
	_ driver.DriverContext                  = (*Driver)(nil)
	_ driver.Connector                      = (*Connector)(nil)
	_ driver.Conn                           = (*conn)(nil)
	_ driver.ConnPrepareContext             = (*conn)(nil)
	_ driver.ConnBeginTx                    = (*conn)(nil)
	_ driver.Pinger                         = (*conn)(nil)
	_ driver.Validator                      = (*conn)(nil)
	_ driver.SessionResetter                = (*conn)(nil)
	_ driver.ExecerContext                  = (*conn)(nil)
	_ driver.QueryerContext                 = (*conn)(nil)
	_ driver.NamedValueChecker              = (*conn)(nil)
	_ driver.Stmt                           = (*stmt)(nil)
	_ driver.StmtExecContext                = (*stmt)(nil)
	_ driver.StmtQueryContext               = (*stmt)(nil)
	_ driver.NamedValueChecker              = (*stmt)(nil)
	_ driver.Rows                           = (*rows)(nil)
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.Result                         = execResult{}
	_ driver.Tx                             = (*transaction)(nil)
)
