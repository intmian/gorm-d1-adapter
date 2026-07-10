package d1

import (
	"context"
	"crypto/rand"
	"database/sql/driver"
	"fmt"
	"io"
	"sync/atomic"
)

type Connection struct {
	ID       string
	cfg      Config
	executor Executor
	closed   atomic.Bool
}

func Open(dsn string) (*Connection, error) {
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	return OpenConfig(cfg)
}

func OpenConfig(cfg Config) (*Connection, error) {
	return OpenConfigContext(context.Background(), cfg)
}

func OpenConfigContext(ctx context.Context, cfg Config) (*Connection, error) {
	executor, err := NewExecutor(cfg)
	if err != nil {
		return nil, err
	}
	return OpenWithExecutorContext(ctx, cfg, executor)
}

func OpenWithExecutor(cfg Config, executor Executor) (*Connection, error) {
	return OpenWithExecutorContext(context.Background(), cfg, executor)
}

func OpenWithExecutorContext(ctx context.Context, cfg Config, executor Executor) (*Connection, error) {
	if executor == nil {
		return nil, ErrInvalidExecutorConfig
	}
	conn := &Connection{
		ID:       newConnectionID(),
		cfg:      cfg.WithDefaults(),
		executor: executor,
	}
	if err := executor.Verify(ctx); err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Connection) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *Connection) Prepare(query string) (driver.Stmt, error) {
	if c.isClosed() {
		return nil, ErrClosed
	}
	return &Stmt{conn: c, query: query}, nil
}

func (c *Connection) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if c.isClosed() {
		return nil, ErrClosed
	}
	return &Stmt{conn: c, query: query}, nil
}

func (c *Connection) Begin() (driver.Tx, error) {
	return nil, ErrUnsupportedTransaction
}

func (c *Connection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, ErrUnsupportedTransaction
}

func (c *Connection) Ping(context.Context) error {
	if c.isClosed() {
		return ErrClosed
	}
	return nil
}

func (c *Connection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.isClosed() {
		return nil, ErrClosed
	}
	params := namedValuesToAny(args)
	result, err := c.executor.Execute(ctx, ExecuteRequest{Mode: ModeExec, SQL: query, Params: params})
	if err != nil {
		return nil, err
	}
	return &Result{result: result}, nil
}

func (c *Connection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.isClosed() {
		return nil, ErrClosed
	}
	params := namedValuesToAny(args)
	result, err := c.executor.Execute(ctx, ExecuteRequest{Mode: ModeQuery, SQL: query, Params: params})
	if err != nil {
		return nil, err
	}
	return newRows(result.ResultSet), nil
}

func (c *Connection) Exec(query string, args []driver.Value) (driver.Result, error) {
	return c.ExecContext(context.Background(), query, valuesToNamed(args))
}

func (c *Connection) Query(query string, args []driver.Value) (driver.Rows, error) {
	return c.QueryContext(context.Background(), query, valuesToNamed(args))
}

func (c *Connection) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (c *Connection) Verify(ctx context.Context) error {
	if c.isClosed() {
		return ErrClosed
	}
	return c.executor.Verify(ctx)
}

func (c *Connection) isClosed() bool {
	return c == nil || c.closed.Load()
}

type Stmt struct {
	conn  *Connection
	query string
}

func (s *Stmt) Close() error {
	return nil
}

func (s *Stmt) NumInput() int {
	return -1
}

func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), valuesToNamed(args))
}

func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), valuesToNamed(args))
}

func (s *Stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.ExecContext(ctx, s.query, args)
}

func (s *Stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.QueryContext(ctx, s.query, args)
}

type Result struct {
	result ExecuteResult
}

func (r *Result) LastInsertId() (int64, error) {
	return r.result.ResultSet.Meta.LastRowID, nil
}

func (r *Result) RowsAffected() (int64, error) {
	return r.result.ResultSet.Meta.Changes, nil
}

type Rows struct {
	columns []string
	rows    [][]any
	index   int
}

func newRows(result ResultSet) *Rows {
	return &Rows{
		columns: append([]string(nil), result.Columns...),
		rows:    append([][]any(nil), result.Rows...),
	}
}

func (r *Rows) Columns() []string {
	return append([]string(nil), r.columns...)
}

func (r *Rows) Close() error {
	return nil
}

func (r *Rows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.index]
	r.index++

	for i := range dest {
		if i >= len(row) {
			dest[i] = nil
			continue
		}
		column := ""
		if i < len(r.columns) {
			column = r.columns[i]
		}
		value, err := DecodeColumnValue(column, row[i])
		if err != nil {
			return err
		}
		dest[i] = value
	}
	return nil
}

func namedValuesToAny(args []driver.NamedValue) []any {
	out := make([]any, len(args))
	for i, arg := range args {
		out[i] = arg.Value
	}
	return out
}

func valuesToNamed(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return out
}

func newConnectionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "d1-connection"
	}
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var (
	_ driver.Conn               = (*Connection)(nil)
	_ driver.ConnPrepareContext = (*Connection)(nil)
	_ driver.ConnBeginTx        = (*Connection)(nil)
	_ driver.Pinger             = (*Connection)(nil)
	_ driver.ExecerContext      = (*Connection)(nil)
	_ driver.QueryerContext     = (*Connection)(nil)
	_ driver.Execer             = (*Connection)(nil)
	_ driver.Queryer            = (*Connection)(nil)
	_ driver.NamedValueChecker  = (*Connection)(nil)
	_ driver.Stmt               = (*Stmt)(nil)
	_ driver.StmtExecContext    = (*Stmt)(nil)
	_ driver.StmtQueryContext   = (*Stmt)(nil)
	_ driver.Result             = (*Result)(nil)
	_ driver.Rows               = (*Rows)(nil)
)
