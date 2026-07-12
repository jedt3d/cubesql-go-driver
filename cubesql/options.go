package cubesql

import (
	"strings"
	"time"

	"github.com/jedt3d/cubesql-go-driver/internal/csdk"
)

// Encryption selects the CubeSQL wire encryption mode.
type Encryption uint8

const (
	EncryptionClear Encryption = iota
	EncryptionAES256
)

// Options configures one physical CubeSQL connection. A zero Timeout uses the
// SDK default of 12 seconds. Positive sub-second values round up to one second.
// The SDK applies this value to socket connection and write operations, but not
// to ordinary response reads; it is not an end-to-end operation deadline.
type Options struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Timeout    time.Duration
	Encryption Encryption
}

func (options Options) native() (csdk.Options, error) {
	if options.Username == "" || strings.IndexByte(options.Host, 0) >= 0 ||
		strings.IndexByte(options.Username, 0) >= 0 || strings.IndexByte(options.Password, 0) >= 0 {
		return csdk.Options{}, ErrInvalidArgument
	}
	if options.Timeout < 0 {
		return csdk.Options{}, ErrInvalidArgument
	}
	if options.Port < 0 || options.Port > 65535 {
		return csdk.Options{}, ErrInvalidArgument
	}
	if options.Encryption != EncryptionClear && options.Encryption != EncryptionAES256 {
		return csdk.Options{}, ErrInvalidArgument
	}
	timeout := 0
	if options.Timeout > 0 {
		seconds := options.Timeout / time.Second
		if options.Timeout%time.Second != 0 {
			seconds++
		}
		if seconds > time.Duration(int64(^uint32(0)>>1)) {
			return csdk.Options{}, ErrInvalidArgument
		}
		timeout = int(seconds)
	}
	encryption := csdk.EncryptionClear
	if options.Encryption == EncryptionAES256 {
		encryption = csdk.EncryptionAES256
	}
	return csdk.Options{
		Host:       options.Host,
		Port:       options.Port,
		Username:   options.Username,
		Password:   options.Password,
		Timeout:    timeout,
		Encryption: encryption,
	}, nil
}

// Validate checks Options without opening a network connection.
func (options Options) Validate() error {
	_, err := options.native()
	return err
}

// Version returns the pinned CubeSQL C SDK header version.
func Version() string {
	return csdk.Version()
}
