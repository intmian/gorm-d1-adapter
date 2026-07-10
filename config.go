package d1

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL        = "https://api.cloudflare.com/client/v4"
	DefaultTimeoutSeconds = 30
)

type ExecutorMode string

const (
	ExecutorModeREST   ExecutorMode = "rest"
	ExecutorModeWorker ExecutorMode = "worker"
)

type Config struct {
	Mode           ExecutorMode
	AccountID      string
	APIToken       string
	DatabaseID     string
	BaseURL        string
	WorkerEndpoint string
	WorkerToken    string
	Timeout        time.Duration
}

func (c Config) WithDefaults() Config {
	if c.Mode == "" {
		c.Mode = ExecutorModeREST
	}
	if c.Mode == ExecutorModeREST && c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeoutSeconds * time.Second
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	c.WorkerEndpoint = strings.TrimRight(c.WorkerEndpoint, "/")
	return c
}

func (c Config) Validate() error {
	c = c.WithDefaults()
	switch c.Mode {
	case ExecutorModeREST:
		if c.AccountID == "" {
			return ErrMissingAccountID
		}
		if c.APIToken == "" {
			return ErrMissingAPIToken
		}
		if c.DatabaseID == "" || len(c.DatabaseID) != 36 {
			return ErrInvalidDB
		}
		if err := validateBaseURL(c.BaseURL); err != nil {
			return err
		}
	case ExecutorModeWorker:
		if err := validateWorkerEndpoint(c.WorkerEndpoint); err != nil {
			return err
		}
		if c.WorkerToken == "" {
			return ErrMissingWorkerToken
		}
	default:
		return ErrInvalidExecutorMode
	}
	return nil
}

func NewDSN(cfg Config) string {
	cfg = cfg.WithDefaults()
	if cfg.Mode == ExecutorModeWorker {
		q := make(url.Values)
		q.Set("mode", string(cfg.Mode))
		q.Set("worker_endpoint", cfg.WorkerEndpoint)
		q.Set("worker_token", cfg.WorkerToken)
		if cfg.Timeout != DefaultTimeoutSeconds*time.Second {
			q.Set("timeout", cfg.Timeout.String())
		}
		return "d1:///?" + q.Encode()
	}

	u := url.URL{
		Scheme: "d1",
		User:   url.UserPassword(cfg.AccountID, cfg.APIToken),
		Host:   cfg.DatabaseID,
	}
	q := u.Query()
	if cfg.Timeout > 0 && cfg.Timeout != DefaultTimeoutSeconds*time.Second {
		q.Set("timeout", cfg.Timeout.String())
	}
	if cfg.BaseURL != "" && strings.TrimRight(cfg.BaseURL, "/") != DefaultBaseURL {
		q.Set("base_url", cfg.BaseURL)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func ParseDSN(dsn string) (Config, error) {
	if dsn == "" {
		return Config{}, ErrEmptyDSN
	}
	if len(dsn) < len("d1://x") {
		return Config{}, ErrShortDSN
	}
	if !strings.HasPrefix(dsn, "d1") {
		return Config{}, ErrNotD1
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return Config{}, err
	}
	if u.Scheme != "d1" {
		return Config{}, ErrNotD1
	}

	cfg := Config{}
	if u.User != nil {
		cfg.AccountID = u.User.Username()
		cfg.APIToken, _ = u.User.Password()
	}
	cfg.DatabaseID = u.Host

	q := u.Query()
	if v := q.Get("account_id"); v != "" {
		cfg.AccountID = v
	}
	if v := q.Get("api_token"); v != "" {
		cfg.APIToken = v
	}
	if v := q.Get("database_id"); v != "" {
		cfg.DatabaseID = v
	}
	if v := q.Get("mode"); v != "" {
		cfg.Mode = ExecutorMode(strings.ToLower(v))
	}
	if v := q.Get("base_url"); v != "" {
		cfg.BaseURL = v
	}
	if v := q.Get("worker_endpoint"); v != "" {
		cfg.WorkerEndpoint = v
	}
	if v := q.Get("worker_token"); v != "" {
		cfg.WorkerToken = v
	}
	if v := q.Get("timeout"); v != "" {
		duration, err := parseTimeout(v)
		if err != nil {
			return Config{}, ErrInvalidTimeout
		}
		cfg.Timeout = duration
	}

	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parseTimeout(value string) (time.Duration, error) {
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0, ErrInvalidTimeout
		}
		return time.Duration(seconds) * time.Second, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, ErrInvalidTimeout
	}
	return duration, nil
}

func validateBaseURL(baseURL string) error {
	u, err := url.ParseRequestURI(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return ErrInvalidBaseURL
	}
	return nil
}

func validateWorkerEndpoint(endpoint string) error {
	if endpoint == "" {
		return ErrInvalidWorkerEndpoint
	}
	u, err := url.ParseRequestURI(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return ErrInvalidWorkerEndpoint
	}
	return nil
}
