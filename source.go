package dsninjector

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// KeyDriver, KeyHost, KeyPort, KeyLogin, KeyPassword, and KeyDatabase are the
// reserved field keys of the map-backed value object. Every other key in a
// DataSourceMapper is a free-form query option.
const (
	KeyDriver   = "driver"
	KeyHost     = "hostname"
	KeyPort     = "port"
	KeyLogin    = "login"
	KeyPassword = "password"
	KeyDatabase = "database"
)

// DataSource exposes a parsed connection string. It is implemented by
// *DataSourceMapper. The interface is intentionally narrow: external test
// doubles satisfy it, so it is not widened — new behaviour is added to the
// concrete *DataSourceMapper (or as free functions) instead.
type DataSource interface {
	Driver() string
	Host() string
	Port() int
	Login() string
	Password() string
	Database() string
	OptionsNames() []string
	Option(name string, defaultValue ...string) string
	Addr(defaultPort ...int) string
	AuthBasicBase64() string
}

// New builds a *DataSourceMapper from functional field options, replacing
// hand-built map literals and DataSource stubs. New() returns a non-nil empty
// mapper usable anywhere a DataSource is expected.
func New(options ...func(*DataSourceMapper)) *DataSourceMapper {
	dsm := &DataSourceMapper{}
	for _, opt := range options {
		opt(dsm)
	}
	return dsm
}

// WithDatabase sets the database field.
func WithDatabase(v string) func(*DataSourceMapper) {
	return func(dsm *DataSourceMapper) { dsm.SetDatabase(v) }
}

// WithDriver sets the driver field.
func WithDriver(v string) func(*DataSourceMapper) {
	return func(dsm *DataSourceMapper) { dsm.SetDriver(v) }
}

// WithHost sets the host field.
func WithHost(v string) func(*DataSourceMapper) {
	return func(dsm *DataSourceMapper) { dsm.SetHost(v) }
}

// WithLogin sets the login field.
func WithLogin(v string) func(*DataSourceMapper) {
	return func(dsm *DataSourceMapper) { dsm.SetLogin(v) }
}

// WithOption sets a free-form query option.
func WithOption(name, value string) func(*DataSourceMapper) {
	return func(dsm *DataSourceMapper) { dsm.SetOption(name, value) }
}

// WithPassword sets the password field.
func WithPassword(v string) func(*DataSourceMapper) {
	return func(dsm *DataSourceMapper) { dsm.SetPassword(v) }
}

// WithPort sets the port field.
func WithPort(v int) func(*DataSourceMapper) {
	return func(dsm *DataSourceMapper) { dsm.SetPort(v) }
}

// DataSourceMapper is a map-backed DataSource. Reserved fields live under the
// Key* constants; every other entry is a free-form query option.
type DataSourceMapper map[string]string

// Addr returns the network address as host[:port], composing the stored host
// and port on demand; when no port is stored and defaultPort is supplied, it is
// appended.
//
// Some drivers repurpose the authority fields: a Telegram DSN of the form
// tbot://<chatid>:@<botid>:<secret> packs the chat id into the login and the bot
// token across host:port, so Addr returns the full "<botid>:<secret>" token
// while Login returns the chat id. Redacted does not mask a secret carried this
// way — only the password component is masked.
func (dsm *DataSourceMapper) Addr(defaultPort ...int) string {
	h := dsm.Option(KeyHost)
	p := dsm.Option(KeyPort)
	if len(p) > 0 {
		p = fmt.Sprintf(":%s", p)
	} else if len(defaultPort) > 0 {
		p = fmt.Sprintf(":%d", defaultPort[0])
	}
	return h + p
}

// AuthBasicBase64 returns base64("login:password") for an HTTP Basic
// Authorization header. Base64 is encoding, not secrecy: treat the result as a
// secret and never log it.
func (dsm *DataSourceMapper) AuthBasicBase64() string {
	l := dsm.Option(KeyLogin)
	p := dsm.Option(KeyPassword)
	basic := fmt.Sprintf("%s:%s", l, p)
	return base64.StdEncoding.EncodeToString([]byte(basic))
}

// Redacted returns the full DSN with a non-empty password field masked as
// "xxxxx" (mirroring net/url.URL.Redacted). It is for display and does not
// round-trip; use Marshal for a complete serialization.
//
// Only the reserved password field is masked. A secret carried elsewhere is
// emitted VERBATIM: a token packed into the authority (host/port — the Telegram
// token trick, see Addr) or a secret placed in a query-option value (e.g.
// ?apikey=…, ?token=…) is not hidden. Do not treat Redacted output as safe for
// such DSNs.
func (dsm *DataSourceMapper) Redacted() string {
	password := dsm.Password()
	if password != "" {
		password = "xxxxx"
	}
	return assembleDSN(dsm, password)
}

// String returns Redacted, so fmt/log of a *DataSourceMapper masks the password
// field rather than printing the raw map. It is not blanket-safe: secrets
// carried in the authority or in a query-option value are emitted verbatim (see
// Redacted). The receiver is a pointer, so printing a bare DataSourceMapper
// value with %v still prints the raw map; every constructor returns a pointer,
// so the common path is covered.
func (dsm *DataSourceMapper) String() string {
	return dsm.Redacted()
}

func (dsm *DataSourceMapper) Driver() string     { return dsm.Option(KeyDriver) }
func (dsm *DataSourceMapper) SetDriver(v string) { dsm.SetOption(KeyDriver, v) }

func (dsm *DataSourceMapper) Host() string     { return dsm.Option(KeyHost) }
func (dsm *DataSourceMapper) SetHost(v string) { dsm.SetOption(KeyHost, v) }

func (dsm *DataSourceMapper) Port() int {
	v, _ := strconv.ParseInt(dsm.Option(KeyPort), 10, 64)
	return int(v)
}
func (dsm *DataSourceMapper) SetPort(v int) { dsm.SetOption(KeyPort, fmt.Sprintf("%d", v)) }

func (dsm *DataSourceMapper) Login() string     { return dsm.Option(KeyLogin) }
func (dsm *DataSourceMapper) SetLogin(v string) { dsm.SetOption(KeyLogin, v) }

func (dsm *DataSourceMapper) Password() string     { return dsm.Option(KeyPassword) }
func (dsm *DataSourceMapper) SetPassword(v string) { dsm.SetOption(KeyPassword, v) }

func (dsm *DataSourceMapper) Database() string     { return dsm.Option(KeyDatabase) }
func (dsm *DataSourceMapper) SetDatabase(v string) { dsm.SetOption(KeyDatabase, v) }

func (dsm *DataSourceMapper) OptionsNames() []string {
	names := make([]string, 0, len(*dsm))
	for k := range *dsm {
		switch k {
		case KeyDriver, KeyHost, KeyPort, KeyLogin, KeyPassword, KeyDatabase:
			continue
		}
		names = append(names, k)
	}
	return names
}

// Option returns the query option stored under name. When the key is absent it
// returns the defaultValue values joined by ";"; when the key is present but
// empty it returns "" — a present-empty value is not replaced by the default. To
// fall back on an empty (present or absent) value, wrap the call with the stdlib
// cmp.Or(dsm.Option(name), "fallback") (Go 1.22+).
func (dsm *DataSourceMapper) Option(name string, defaultValue ...string) string {
	val, exists := (*dsm)[name]
	if !exists {
		val = strings.Join(defaultValue, ";")
	}
	return val
}

// OptionBool parses the option named name with strconv.ParseBool. On a missing,
// empty, or unparseable value it returns defaultValue[0] if supplied, else the
// zero value (false).
func (dsm *DataSourceMapper) OptionBool(name string, defaultValue ...bool) bool {
	fallback := false
	if len(defaultValue) > 0 {
		fallback = defaultValue[0]
	}
	v := dsm.Option(name)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// OptionDuration parses the option named name with time.ParseDuration. On a
// missing, empty, or unparseable value it returns defaultValue[0] if supplied,
// else the zero value. The value must carry a unit ("30s", "5m"): a unit-less
// "30" is a parse error and yields the default — use OptionInt for bare numbers.
func (dsm *DataSourceMapper) OptionDuration(name string, defaultValue ...time.Duration) time.Duration {
	var fallback time.Duration
	if len(defaultValue) > 0 {
		fallback = defaultValue[0]
	}
	v := dsm.Option(name)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// OptionInt parses the option named name with strconv.Atoi. On a missing,
// empty, or unparseable value it returns defaultValue[0] if supplied, else the
// zero value (0).
func (dsm *DataSourceMapper) OptionInt(name string, defaultValue ...int) int {
	fallback := 0
	if len(defaultValue) > 0 {
		fallback = defaultValue[0]
	}
	v := dsm.Option(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func (dsm *DataSourceMapper) SetOption(name string, value string) {
	if *dsm == nil {
		*dsm = make(map[string]string, 6)
	}
	(*dsm)[name] = value
}

// Require returns nil when every named key is present and non-empty, otherwise a
// single plain error naming all offending keys in argument order (e.g. "missing
// required data source fields: login, password"). A present-but-empty field
// counts as missing. The error names keys only, never their values.
func (dsm *DataSourceMapper) Require(keys ...string) error {
	var missing []string
	for _, k := range keys {
		if dsm.Option(k) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required data source fields: %s", strings.Join(missing, ", "))
	}
	return nil
}
