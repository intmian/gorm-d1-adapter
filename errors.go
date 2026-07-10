package d1

import "errors"

const DriverName = "d1"

var (
	ErrInvalidAPI                = errors.New("invalid api")
	ErrClosed                    = errors.New("d1: connection is closed")
	ErrEmptyDSN                  = errors.New("dsn is empty")
	ErrShortDSN                  = errors.New("dsn specified is impossibly short")
	ErrNotD1                     = errors.New("dsn does not start with 'd1'")
	ErrInvalidDB                 = errors.New("invalid database id")
	ErrInvalidMode               = errors.New("invalid execute mode")
	ErrInvalidExecutorMode       = errors.New("d1: invalid executor mode")
	ErrInvalidWorkerEndpoint     = errors.New("d1: invalid worker endpoint")
	ErrMissingWorkerToken        = errors.New("d1: worker token is required")
	ErrInvalidTimeout            = errors.New("d1: timeout must be a positive integer")
	ErrUnsupportedTransaction    = errors.New("d1: transactions are not supported by this driver")
	ErrDestructiveMigration      = errors.New("d1: destructive schema migration is not supported")
	ErrConstraintsNotImplemented = errors.New("d1: constraints are not implemented")
	ErrEmptyResultSet            = errors.New("d1: empty result set")
	ErrInvalidExecutorConfig     = errors.New("d1: invalid executor config")
	ErrWorkerResponseTooLarge    = errors.New("d1: worker response exceeds limit")
	ErrWorkerResultCountMismatch = errors.New("d1: worker batch result count does not match request")
)
