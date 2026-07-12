package cubesql

import (
	"context"
	"database/sql"
	"database/sql/driver"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

var defaultDriver = &Driver{}

func init() {
	sql.Register(DriverName, defaultDriver)
}

type Driver struct{}

func (driverInstance *Driver) Open(name string) (driver.Conn, error) {
	connector, err := driverInstance.OpenConnector(name)
	if err != nil {
		return nil, err
	}
	return connector.Connect(context.Background())
}

func (driverInstance *Driver) OpenConnector(name string) (driver.Connector, error) {
	config, err := ParseDSN(name)
	if err != nil {
		return nil, err
	}
	return NewConnector(config)
}

type Connector struct {
	config Config
}

func NewConnector(config Config) (*Connector, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Connector{config: config}, nil
}

func (connector *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	if connector == nil {
		return nil, core.ErrInvalidArgument
	}
	native, err := core.OpenContext(ctx, connector.config.Options)
	if err != nil {
		return nil, err
	}
	if connector.config.Database != "" {
		if err := native.SetDatabaseContext(ctx, connector.config.Database); err != nil {
			native.Close()
			return nil, err
		}
	}
	return &conn{native: native, database: connector.config.Database}, nil
}

func (connector *Connector) Driver() driver.Driver {
	return defaultDriver
}
