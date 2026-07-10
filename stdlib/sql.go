package stdlib

import (
	"database/sql"

	d1 "github.com/intmian/gorm-d1-adapter"
)

func init() {
	sql.Register(d1.DriverName, d1.NewDriver())
}
