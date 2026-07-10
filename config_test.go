package d1

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseDSNValidation(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		err  error
	}{
		{name: "empty", dsn: "", err: ErrEmptyDSN},
		{name: "short", dsn: "d1://", err: ErrShortDSN},
		{name: "not_d1", dsn: "mysql://user:pass@example", err: ErrNotD1},
		{name: "invalid_db", dsn: "d1://account:token@short", err: ErrInvalidDB},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDSN(tt.dsn)
			if !errors.Is(err, tt.err) {
				t.Fatalf("expected %v, got %v", tt.err, err)
			}
		})
	}
}

func TestNewDSNRoundTripEscapesToken(t *testing.T) {
	cfg := Config{
		AccountID:  "account",
		APIToken:   `tok:@/?#%`,
		DatabaseID: "12345678-1234-1234-1234-123456789012",
		Timeout:    12 * time.Second,
	}
	dsn := NewDSN(cfg)
	if strings.Contains(dsn, `tok:@/?#%`) {
		t.Fatalf("token was not escaped in DSN: %s", dsn)
	}
	got, err := ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != cfg.AccountID || got.APIToken != cfg.APIToken || got.DatabaseID != cfg.DatabaseID || got.Timeout != cfg.Timeout {
		t.Fatalf("unexpected parsed config: %+v", got)
	}
}

func TestNewDSNWorkerRoundTrip(t *testing.T) {
	cfg := Config{
		Mode:           ExecutorModeWorker,
		WorkerEndpoint: "https://d1.example.workers.dev/proxy/",
		WorkerToken:    `worker:@/?#%`,
		Timeout:        8 * time.Second,
	}
	dsn := NewDSN(cfg)
	if strings.Contains(dsn, cfg.WorkerToken) {
		t.Fatalf("worker token was not escaped in DSN: %s", dsn)
	}
	got, err := ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ExecutorModeWorker || got.WorkerEndpoint != "https://d1.example.workers.dev/proxy" || got.WorkerToken != cfg.WorkerToken || got.Timeout != cfg.Timeout {
		t.Fatalf("unexpected parsed worker config: %+v", got)
	}
}

func TestNewDSNRoundTripsSubsecondTimeout(t *testing.T) {
	cfg := Config{
		Mode:           ExecutorModeWorker,
		WorkerEndpoint: "https://d1.example.workers.dev",
		WorkerToken:    "token",
		Timeout:        750 * time.Millisecond,
	}
	got, err := ParseDSN(NewDSN(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if got.Timeout != cfg.Timeout {
		t.Fatalf("expected %s, got %s", cfg.Timeout, got.Timeout)
	}
}

func TestWorkerConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		err  error
	}{
		{
			name: "unknown_mode",
			cfg:  Config{Mode: "other"},
			err:  ErrInvalidExecutorMode,
		},
		{
			name: "missing_endpoint",
			cfg:  Config{Mode: ExecutorModeWorker, WorkerToken: "token"},
			err:  ErrInvalidWorkerEndpoint,
		},
		{
			name: "invalid_endpoint",
			cfg:  Config{Mode: ExecutorModeWorker, WorkerEndpoint: "ftp://example.com", WorkerToken: "token"},
			err:  ErrInvalidWorkerEndpoint,
		},
		{
			name: "missing_token",
			cfg:  Config{Mode: ExecutorModeWorker, WorkerEndpoint: "https://example.com"},
			err:  ErrMissingWorkerToken,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); !errors.Is(err, tt.err) {
				t.Fatalf("expected %v, got %v", tt.err, err)
			}
		})
	}
}

func TestParseDSNRejectsInvalidTimeout(t *testing.T) {
	_, err := ParseDSN("d1://account:token@12345678-1234-1234-1234-123456789012?timeout=0")
	if !errors.Is(err, ErrInvalidTimeout) {
		t.Fatalf("expected %v, got %v", ErrInvalidTimeout, err)
	}
}

func TestRESTConfigRequiresCredentials(t *testing.T) {
	databaseID := "12345678-1234-1234-1234-123456789012"
	for _, cfg := range []Config{
		{DatabaseID: databaseID, APIToken: "token"},
		{DatabaseID: databaseID, AccountID: "account"},
	} {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected REST credential validation error for %+v", cfg)
		}
	}
}

func TestRESTConfigRejectsInvalidBaseURL(t *testing.T) {
	cfg := Config{
		AccountID:  "account",
		APIToken:   "token",
		DatabaseID: "12345678-1234-1234-1234-123456789012",
		BaseURL:    "ftp://example.com",
	}
	if err := cfg.Validate(); !errors.Is(err, ErrInvalidBaseURL) {
		t.Fatalf("expected %v, got %v", ErrInvalidBaseURL, err)
	}
}
