package d1

import (
	"context"
	"database/sql/driver"
)

type Driver struct{}

func NewDriver() *Driver {
	return &Driver{}
}

func (d *Driver) Open(dsn string) (driver.Conn, error) {
	return Open(dsn)
}

func (d *Driver) OpenConnector(dsn string) (driver.Connector, error) {
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	return &Connector{cfg: cfg, driver: d}, nil
}

type Connector struct {
	cfg    Config
	driver *Driver
}

func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	return OpenConfigContext(ctx, c.cfg)
}

func (c *Connector) Driver() driver.Driver {
	return c.driver
}

var (
	_ driver.Driver        = (*Driver)(nil)
	_ driver.DriverContext = (*Driver)(nil)
	_ driver.Connector     = (*Connector)(nil)
)
