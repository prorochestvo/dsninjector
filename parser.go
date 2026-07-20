package dsninjector

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Marshal renders dsm as a Parse-compatible DSN
// (driver://[login:password@]addr/database?opts) with the query keys sorted for
// deterministic output. It always returns a nil error.
//
// The output contains the credentials in CLEARTEXT (login:password) — this is a
// behavior change from the earlier base64-encoded output. Never log the result;
// use Redacted for display. Marshal round-trips network-form DSNs through Parse
// at the field level. It does not percent-escape special characters and does not
// round-trip socket/file forms (mysql:/path, sqlite:/path, file:name) or values
// containing @ : / % — those need a net/url engine and are out of scope.
func Marshal(dsm DataSource) (string, error) {
	return assembleDSN(dsm, dsm.Password()), nil
}

// Unmarshal reads the connection string from the environment variable named key,
// falling back to defaultValue when it is unset, and parses it. It returns the
// concrete *DataSourceMapper so the typed accessors are reachable directly.
// Check the returned error rather than the pointer: a failure returns a nil
// *DataSourceMapper.
func Unmarshal(key string, defaultValue ...string) (*DataSourceMapper, error) {
	env, exists := os.LookupEnv(key)
	if !exists && len(defaultValue) > 0 {
		env = strings.Join(defaultValue, ";")
	}

	env = strings.TrimSpace(env)
	if env == "" {
		return nil, fmt.Errorf("could not recognize configuration")
	}

	dns, err := Parse(env)
	if err != nil {
		return nil, err
	}

	return dns, nil
}

// UnmarshalOrEmpty behaves like Unmarshal but never errors: on any failure it
// returns a non-nil empty *DataSourceMapper.
func UnmarshalOrEmpty(key string, defaultValue ...string) *DataSourceMapper {
	dsn, err := Unmarshal(key, defaultValue...)
	if err != nil {
		dsn = &DataSourceMapper{}
	}
	return dsn
}

// InitEnvFrom reads environment variables from the file and sets them
func InitEnvFrom(filePaths ...string) error {
	if len(filePaths) == 0 {
		filePaths = []string{".env"}
	}

	for _, filePath := range filePaths {
		if filePath == "" {
			continue
		}

		if s, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) || s.IsDir() {
			continue
		}

		d, err := readEnvVarName(filePath)
		if err != nil {
			return fmt.Errorf("could not extract environment variables from %s, reason: %w", filePath, err)
		}

		for k, v := range d {
			err = os.Setenv(k, v)
			if err != nil {
				return fmt.Errorf("could not set environment variable %s, reason: %w", k, err)
			}
		}
	}

	return nil
}

// Parse reads the connection string and returns DataSource.
func Parse(dns string) (*DataSourceMapper, error) {
	// Peel a trailing "?opts" up front, for every form: the authority regex's
	// instance group ([^/@]+ includes '?') would otherwise swallow a query that
	// has no "/path" before it (e.g. "mongodb://u:p@h:27017?limit=20" would parse
	// the port as "27017?limit=20"). Peeling first covers the authority path too.
	opts := ""
	if q := strings.IndexByte(dns, '?'); q >= 0 {
		opts, dns = dns[q+1:], dns[:q]
	}
	var (
		ds  *DataSourceMapper
		err error
	)
	switch {
	case strings.Contains(dns, "://") || strings.HasPrefix(dns, "//"):
		ds, err = parseAuthority(dns) // authority form
	default:
		// Scheme-less: a bare host, a host:port, a driver:opaque string, or a
		// filesystem path.
		switch colon := strings.IndexByte(dns, ':'); {
		case colon >= 0 && colon+1 < len(dns) && dns[colon+1] >= '0' && dns[colon+1] <= '9':
			ds, err = parseAuthority("//" + dns) // host:port (colon immediately before a digit)
		case colon >= 0:
			ds, err = parseAuthority(dns) // driver:opaque — sqlite:/path, file:x.db
		case strings.Contains(dns, "/"):
			ds, err = parseBarePath(dns) // bare filesystem path -> Database(), verbatim
		default:
			ds, err = parseAuthority("//" + dns) // bare host — google.com, localhost
		}
	}
	if err != nil {
		return nil, err
	}
	if opts != "" {
		if err = applyOptions(*ds, opts); err != nil {
			return nil, err
		}
	}
	return ds, nil
}

// MustParse is like Parse but panics if the connection string cannot be parsed.
// The panic carries only the sanitized, input-free parse error, never the raw
// dsn, so it is safe to use on secret-bearing DSNs. It mirrors the stdlib Must
// idiom (regexp.MustCompile, template.Must) and is intended for package-level
// initialisation and tests.
func MustParse(dsn string) *DataSourceMapper {
	ds, err := Parse(dsn)
	if err != nil {
		panic(err)
	}
	return ds
}

// authorityRx matches an authority-form DSN — an optional driver, an optional
// //credentials@instance section, an optional /path, and an optional ?params —
// and is compiled once here rather than on every Parse call.
var authorityRx = regexp.MustCompile(`(?i)^(?:(?P<driver>[a-z+]+):)?(?://(?:(?P<credentials>[^/@]+)@)?(?P<instance>[^/@]+))?(?:/?(?P<path>[^?]+))?(?:\?(?P<params>.*))?$`)

// applyOptions parses raw as a URL query string and folds each key's values into
// m. A parse failure returns a static, input-free error: url.ParseQuery's
// EscapeError text embeds the offending (untrusted, possibly secret-bearing)
// fragment, so it must never be wrapped or echoed.
func applyOptions(m map[string]string, raw string) error {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return errors.New("invalid connection string query parameters")
	}
	for k, v := range q {
		m[k] = strings.Join(v, ",")
	}
	return nil
}

// assembleDSN builds a Parse-compatible DSN, substituting password so callers
// can pass the real secret (Marshal) or a mask (Redacted). It is the single home
// for the DSN serialization format.
func assembleDSN(ds DataSource, password string) string {
	var b strings.Builder
	if d := ds.Driver(); d != "" {
		b.WriteString(d)
		b.WriteString("://")
	} else {
		b.WriteString("//")
	}
	if login := ds.Login(); login != "" || password != "" {
		b.WriteString(login)
		b.WriteByte(':')
		b.WriteString(password)
		b.WriteByte('@')
	}
	b.WriteString(ds.Addr())
	if db := ds.Database(); db != "" {
		b.WriteByte('/')
		b.WriteString(db)
	}
	names := ds.OptionsNames()
	sort.Strings(names)
	for i, n := range names {
		if i == 0 {
			b.WriteByte('?')
		} else {
			b.WriteByte('&')
		}
		b.WriteString(n)
		b.WriteByte('=')
		b.WriteString(ds.Option(n))
	}
	return b.String()
}

// parseAuthority parses an authority-form DSN via authorityRx, splitting the
// instance into host/port and the credentials into login/password.
func parseAuthority(dns string) (*DataSourceMapper, error) {
	// Match the connection string
	matches := authorityRx.FindStringSubmatch(dns)
	if matches == nil {
		// Static, input-free error: the raw dns carries credentials/tokens and
		// must never appear in an error. This branch is effectively unreachable
		// under the all-optional anchored regex, but is sanitized regardless so it
		// can never leak if the regex is later loosened.
		return nil, errors.New("invalid connection string format")
	}

	m := make(map[string]string)

	// Map the named groups to their values
	names := authorityRx.SubexpNames()
	instance := ""
	credentials := ""
	for i, name := range names {
		if name == "" || i >= len(matches) {
			continue
		}

		switch name {
		case "driver":
			m[KeyDriver] = matches[i]
		case "instance":
			instance = matches[i]
			if strings.Contains(instance, ":") {
				p := strings.SplitN(instance, ":", 2)
				m[KeyHost] = p[0]
				m[KeyPort] = p[1]
			} else {
				m[KeyHost] = instance
				m[KeyPort] = ""
			}
		case "credentials":
			credentials = matches[i]
			if strings.Contains(credentials, ":") {
				p := strings.SplitN(credentials, ":", 2)
				m[KeyLogin] = p[0]
				m[KeyPassword] = p[1]
			} else {
				m[KeyLogin] = ""
				m[KeyPassword] = credentials
			}
		case "path":
			if instance == "" && credentials == "" {
				m[KeyDatabase] = path.Join("/", matches[i])
			} else {
				m[KeyDatabase] = matches[i]
			}
		case "params":
			if err := applyOptions(m, matches[i]); err != nil {
				return nil, err
			}
		}
	}

	ds := DataSourceMapper(m)
	return &ds, nil
}

// parseBarePath maps a scheme-less filesystem path straight to Database(),
// preserving it verbatim (this is what fixes the path.Join relative-path
// corruption at parser.go:138). The "?opts" tail is already stripped by Parse.
func parseBarePath(dns string) (*DataSourceMapper, error) {
	m := map[string]string{
		KeyDriver: "", KeyHost: "", KeyPort: "",
		KeyLogin: "", KeyPassword: "", KeyDatabase: dns,
	}
	ds := DataSourceMapper(m)
	return &ds, nil
}

// readEnvVarName reads environment variables from the file
func readEnvVarName(filePath string) (map[string]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func(closer io.Closer) { err = errors.Join(err, closer.Close()) }(f)

	dataset := make(map[string]string)

	fScanner := bufio.NewScanner(f)
	fScanner.Split(bufio.ScanLines)

	for fScanner.Scan() {
		line := strings.SplitN(fScanner.Text(), "=", 2)
		if len(line) != 2 || len(line[0]) == 0 || len(line[1]) == 0 {
			continue
		}

		key := strings.ToUpper(line[0])
		key = strings.TrimSpace(key)

		val := line[1]
		val = strings.TrimSpace(val)

		dataset[key] = val
	}

	return dataset, nil
}
