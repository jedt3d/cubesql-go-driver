package cubesql

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	core "github.com/jedt3d/cubesql-go-driver/cubesql"
)

const DriverName = "cubesql"

var ErrInvalidDSN = errors.New("cubesql database/sql: invalid DSN")

// Config describes equivalent physical CubeSQL connections for database/sql.
type Config struct {
	Options  core.Options
	Database string
}

func (config Config) Validate() error {
	if err := config.Options.Validate(); err != nil {
		return fmt.Errorf("%w: invalid connection options", ErrInvalidDSN)
	}
	if strings.IndexByte(config.Database, 0) >= 0 {
		return fmt.Errorf("%w: database contains NUL", ErrInvalidDSN)
	}
	return nil
}

// ParseDSN parses cubesql://user:password@host:port/database URLs. Supported
// query keys are timeout and encryption, where encryption is clear or aes256.
func ParseDSN(dsn string) (Config, error) {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme != DriverName || parsed.Opaque != "" || parsed.Fragment != "" {
		return Config{}, fmt.Errorf("%w: malformed URL", ErrInvalidDSN)
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return Config{}, fmt.Errorf("%w: username is required", ErrInvalidDSN)
	}
	password, passwordSet := parsed.User.Password()
	if !passwordSet {
		return Config{}, fmt.Errorf("%w: password must be explicit", ErrInvalidDSN)
	}
	host := parsed.Hostname()
	if host == "" {
		return Config{}, fmt.Errorf("%w: host is required", ErrInvalidDSN)
	}
	port := 4430
	if text := parsed.Port(); text != "" {
		port, err = strconv.Atoi(text)
		if err != nil || port <= 0 || port > 65535 {
			return Config{}, fmt.Errorf("%w: invalid port", ErrInvalidDSN)
		}
	} else if strings.Contains(parsed.Host, ":") && net.ParseIP(host) == nil {
		return Config{}, fmt.Errorf("%w: invalid host or port", ErrInvalidDSN)
	}

	path := strings.TrimPrefix(parsed.Path, "/")
	if strings.Contains(path, "/") {
		return Config{}, fmt.Errorf("%w: database must be one path segment", ErrInvalidDSN)
	}
	query := parsed.Query()
	for key, values := range query {
		if key != "timeout" && key != "encryption" {
			return Config{}, fmt.Errorf("%w: unsupported query key", ErrInvalidDSN)
		}
		if len(values) != 1 {
			return Config{}, fmt.Errorf("%w: duplicate query key", ErrInvalidDSN)
		}
	}
	timeout := time.Duration(0)
	if text := query.Get("timeout"); text != "" {
		timeout, err = time.ParseDuration(text)
		if err != nil || timeout < 0 {
			return Config{}, fmt.Errorf("%w: invalid timeout", ErrInvalidDSN)
		}
	}
	encryption := core.EncryptionClear
	switch query.Get("encryption") {
	case "", "clear":
	case "aes256":
		encryption = core.EncryptionAES256
	default:
		return Config{}, fmt.Errorf("%w: invalid encryption", ErrInvalidDSN)
	}

	config := Config{
		Options: core.Options{
			Host:       host,
			Port:       port,
			Username:   parsed.User.Username(),
			Password:   password,
			Timeout:    timeout,
			Encryption: encryption,
		},
		Database: path,
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// OpenDB returns a database/sql pool configured without embedding credentials
// in a DSN string.
func OpenDB(config Config) (*sql.DB, error) {
	connector, err := NewConnector(config)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}
