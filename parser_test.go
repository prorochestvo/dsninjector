package dsninjector

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path"
	"testing"
)

func TestMarshal(t *testing.T) {
	t.Parallel()

	assertRoundTrip := func(t *testing.T, src string) {
		t.Helper()
		orig, err := Parse(src)
		require.NoError(t, err, src)
		out, err := Marshal(orig)
		require.NoError(t, err, src)
		got, err := Parse(out)
		require.NoError(t, err, out)
		require.Equal(t, orig.Driver(), got.Driver(), "driver: %q -> %q", src, out)
		require.Equal(t, orig.Host(), got.Host(), "host: %q -> %q", src, out)
		require.Equal(t, orig.Port(), got.Port(), "port: %q -> %q", src, out)
		require.Equal(t, orig.Login(), got.Login(), "login: %q -> %q", src, out)
		require.Equal(t, orig.Password(), got.Password(), "password: %q -> %q", src, out)
		require.Equal(t, orig.Database(), got.Database(), "database: %q -> %q", src, out)
		require.ElementsMatch(t, orig.OptionsNames(), got.OptionsNames(), "options: %q -> %q", src, out)
		for _, n := range orig.OptionsNames() {
			require.Equal(t, orig.Option(n), got.Option(n), "option %q: %q -> %q", n, src, out)
		}
	}

	t.Run("network-form round-trip", func(t *testing.T) {
		t.Parallel()
		// Network forms that invert through Parse. Since Parse peels the ?opts tail
		// up front, an authority with a query but no /database (redis://h:6379?db=0)
		// now round-trips too. Compared at the accessor level per the plan:
		// mysql://:@localhost/dbname parses with present-but-empty login/password
		// keys that assembleDSN correctly omits, so map identity would fail while
		// Login()/Password() ("" either way) hold.
		cases := []string{
			"postgres://user:password@localhost:5432/dbname",
			"mysql://:@localhost/dbname",
			"postgres://user:password@localhost:5432/dbname?sslmode=disable&timeout=30",
			"postgres://user:pass@localhost/dbname",
			"pg://user:pass@localhost/dbname?sslmode=disable",
			"mysql://user:pass@localhost/dbname",
			"sqlserver://user:pass@remote-host.com/dbname",
			"mssql://user:pass@remote-host.com/instance/dbname",
			"ms://user:pass@remote-host.com:port/instance/dbname?keepAlive=10",
			"oracle://user:pass@somehost.com/sid",
			"sap://user:pass@localhost/dbname",
			"odbc+postgres://user:pass@localhost:port/dbname?option1=",
			"https://localhost:8080/dbname?option1=1",
			"sqlite://_:_@_:_/path",
			"redis://h:6379?db=0",
			"//host/db",
			"//localhost:5432/mydb",
		}
		for _, src := range cases {
			t.Run(src, func(t *testing.T) {
				t.Parallel()
				assertRoundTrip(t, src)
			})
		}
	})

	t.Run("deterministic sorted query", func(t *testing.T) {
		t.Parallel()
		ds, err := Parse("postgres://user:password@localhost:5432/dbname?timeout=30&sslmode=disable")
		require.NoError(t, err)
		out, err := Marshal(ds)
		require.NoError(t, err)
		require.Equal(t, "postgres://user:password@localhost:5432/dbname?sslmode=disable&timeout=30", out)
	})

	t.Run("no credentials when both empty", func(t *testing.T) {
		t.Parallel()
		ds, err := Parse("mysql://:@localhost/dbname")
		require.NoError(t, err)
		out, err := Marshal(ds)
		require.NoError(t, err)
		require.Equal(t, "mysql://localhost/dbname", out)
		require.NotContains(t, out, "@")
	})

	t.Run("driver-less authority emits leading //", func(t *testing.T) {
		t.Parallel()
		ds, err := Parse("//host/db")
		require.NoError(t, err)
		out, err := Marshal(ds)
		require.NoError(t, err)
		require.Equal(t, "//host/db", out)
	})

	t.Run("empty option value round-trips", func(t *testing.T) {
		t.Parallel()
		ds, err := Parse("odbc+postgres://user:pass@localhost:port/dbname?option1=")
		require.NoError(t, err)
		out, err := Marshal(ds)
		require.NoError(t, err)
		require.Equal(t, "odbc+postgres://user:pass@localhost:port/dbname?option1=", out)
	})

	t.Run("telegram token trick survives round-trip", func(t *testing.T) {
		t.Parallel()
		// Multiple downstream projects depend on this form: chat id in login, the
		// full <botid>:<secret> token rejoined by Addr().
		const (
			chatID = "123456789"
			botID  = "987654321"
			secret = "AAHb1-Cd_EfGhIjKlMnOpQrStUvWxYz0123456789"
		)
		token := botID + ":" + secret
		orig, err := Parse("tbot://" + chatID + ":@" + token)
		require.NoError(t, err)
		out, err := Marshal(orig)
		require.NoError(t, err)
		got, err := Parse(out)
		require.NoError(t, err)
		require.Equal(t, chatID, got.Login())
		require.Equal(t, token, got.Addr())
	})

	t.Run("never returns an error", func(t *testing.T) {
		t.Parallel()
		_, err := Marshal(&DataSourceMapper{})
		require.NoError(t, err)
	})
}

func TestUnmarshal(t *testing.T) {
	t.Run("env present", func(t *testing.T) {
		t.Setenv("DSN_INJECTOR_UNMARSHAL_PRESENT", "postgres://user:pass@localhost:5432/db?limit=42")
		ds, err := Unmarshal("DSN_INJECTOR_UNMARSHAL_PRESENT")
		require.NoError(t, err)
		require.Equal(t, "postgres", ds.Driver())
		// A method added in this plan is reachable on the concrete return type.
		require.Equal(t, 42, ds.OptionInt("limit"))
	})

	t.Run("env absent with default", func(t *testing.T) {
		ds, err := Unmarshal("DSN_INJECTOR_UNMARSHAL_ABSENT_WITH_DEFAULT", "mysql://h/db")
		require.NoError(t, err)
		require.Equal(t, "mysql", ds.Driver())
	})

	t.Run("env absent without default returns error and nil", func(t *testing.T) {
		ds, err := Unmarshal("DSN_INJECTOR_UNMARSHAL_ABSENT_NO_DEFAULT")
		require.Error(t, err)
		require.Nil(t, ds)
	})
}

func TestUnmarshalOrEmpty(t *testing.T) {
	t.Run("env present", func(t *testing.T) {
		t.Setenv("DSN_INJECTOR_OREMPTY_PRESENT", "redis://localhost:6379/0?db=3")
		ds := UnmarshalOrEmpty("DSN_INJECTOR_OREMPTY_PRESENT")
		require.NotNil(t, ds)
		require.Equal(t, "redis", ds.Driver())
		require.Equal(t, 3, ds.OptionInt("db"))
	})

	t.Run("env absent without default returns non-nil empty", func(t *testing.T) {
		ds := UnmarshalOrEmpty("DSN_INJECTOR_OREMPTY_ABSENT")
		require.NotNil(t, ds)
		require.Equal(t, "", ds.Driver())
		// The empty mapper is fully usable, including new methods with defaults.
		require.Equal(t, 7, ds.OptionInt("anything", 7))
	})

	t.Run("env absent with default", func(t *testing.T) {
		ds := UnmarshalOrEmpty("DSN_INJECTOR_OREMPTY_ABSENT_WITH_DEFAULT", "sqlite:/tmp/x.db")
		require.NotNil(t, ds)
		require.Equal(t, "sqlite", ds.Driver())
	})
}

func TestMustParse(t *testing.T) {
	t.Parallel()

	t.Run("success matches Parse", func(t *testing.T) {
		t.Parallel()
		const src = "postgres://u:p@h:5432/db"
		want, err := Parse(src)
		require.NoError(t, err)
		require.Equal(t, want, MustParse(src))
	})

	t.Run("panics on invalid input", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() { _ = MustParse("x://h/d?tok=%zz") })
	})

	t.Run("panic does not echo the input secret", func(t *testing.T) {
		t.Parallel()
		const secret = "SECRETTOKEN"
		defer func() {
			r := recover()
			require.NotNil(t, r)
			err, ok := r.(error)
			require.True(t, ok)
			require.NotContains(t, err.Error(), secret)
		}()
		_ = MustParse("x://h/d?tok=%zz" + secret)
	})
}

func TestInitEnvFrom(t *testing.T) {
	key01 := "DSN_INJECTOR_TEST_KEY_01"
	key02 := "DSN_INJECTOR_TEST_KEY_02"
	key03 := "DSN_INJECTOR_TEST_KEY_03"

	require.Equal(t, "", os.Getenv(key01))
	require.Equal(t, "", os.Getenv(key02))
	require.Equal(t, "", os.Getenv(key03))

	p1 := path.Join(t.TempDir(), "dsn_injector_test.1.env")
	p2 := path.Join(t.TempDir(), "dsn_injector_test.2.env")
	p3 := path.Join(t.TempDir(), "dsn_injector_test.3.env")

	require.NoError(t, os.WriteFile(p1, []byte(fmt.Sprintf("%s=TEST_VALUE_01", key01)), 0666))
	require.NoError(t, os.WriteFile(p2, []byte(fmt.Sprintf("%s=TEST_VALUE_02", key02)), 0666))
	require.NoError(t, os.WriteFile(p3, []byte(fmt.Sprintf("%s=TEST_VALUE_03", key03)), 0666))

	require.NoError(t, InitEnvFrom(p1, p2, p3))

	require.Equal(t, "TEST_VALUE_01", os.Getenv(key01))
	require.Equal(t, "TEST_VALUE_02", os.Getenv(key02))
	require.Equal(t, "TEST_VALUE_03", os.Getenv(key03))
}

func TestParse(t *testing.T) {
	t.Parallel()

	successfulTestCases := []struct {
		src string
		dsm *DataSourceMapper
	}{
		{
			src: "postgres://user:password@localhost:5432/dbname",
			dsm: &DataSourceMapper{KeyDriver: "postgres", KeyHost: "localhost", KeyPort: "5432", KeyLogin: "user", KeyPassword: "password", KeyDatabase: "dbname"},
		},
		{
			src: "mysql://:@localhost/dbname",
			dsm: &DataSourceMapper{KeyDriver: "mysql", KeyHost: "localhost", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "dbname"},
		},
		{
			src: "postgres://user:password@localhost:5432/dbname?sslmode=disable&timeout=30",
			dsm: &DataSourceMapper{KeyDriver: "postgres", KeyHost: "localhost", KeyPort: "5432", KeyLogin: "user", KeyPassword: "password", KeyDatabase: "dbname", "sslmode": "disable", "timeout": "30"},
		},
		{
			src: "postgres://user:pass@localhost/dbname",
			dsm: &DataSourceMapper{KeyDriver: "postgres", KeyHost: "localhost", KeyPort: "", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "dbname"},
		},
		{
			src: "pg://user:pass@localhost/dbname?sslmode=disable",
			dsm: &DataSourceMapper{KeyDriver: "pg", KeyHost: "localhost", KeyPort: "", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "dbname", "sslmode": "disable"},
		},
		{
			src: "mysql://user:pass@localhost/dbname",
			dsm: &DataSourceMapper{KeyDriver: "mysql", KeyHost: "localhost", KeyPort: "", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "dbname"},
		},
		{
			src: "mysql:/var/run/mysqld/mysqld.sock",
			dsm: &DataSourceMapper{KeyDriver: "mysql", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "/var/run/mysqld/mysqld.sock"},
		},
		{
			src: "sqlserver://user:pass@remote-host.com/dbname",
			dsm: &DataSourceMapper{KeyDriver: "sqlserver", KeyHost: "remote-host.com", KeyPort: "", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "dbname"},
		},
		{
			src: "mssql://user:pass@remote-host.com/instance/dbname",
			dsm: &DataSourceMapper{KeyDriver: "mssql", KeyHost: "remote-host.com", KeyPort: "", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "instance/dbname"},
		},
		{
			src: "ms://user:pass@remote-host.com:port/instance/dbname?keepAlive=10",
			dsm: &DataSourceMapper{KeyDriver: "ms", KeyHost: "remote-host.com", KeyPort: "port", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "instance/dbname", "keepAlive": "10"},
		},
		{
			src: "oracle://user:pass@somehost.com/sid",
			dsm: &DataSourceMapper{KeyDriver: "oracle", KeyHost: "somehost.com", KeyPort: "", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "sid"},
		},
		{
			src: "sap://user:pass@localhost/dbname",
			dsm: &DataSourceMapper{KeyDriver: "sap", KeyHost: "localhost", KeyPort: "", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "dbname"},
		},
		{
			src: "sqlite:/path/to/file.db",
			dsm: &DataSourceMapper{KeyDriver: "sqlite", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "/path/to/file.db"},
		},
		{
			src: "file:myfile.sqlite3?loc=auto",
			dsm: &DataSourceMapper{KeyDriver: "file", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "/myfile.sqlite3", "loc": "auto"},
		},
		{
			src: "odbc+postgres://user:pass@localhost:port/dbname?option1=",
			dsm: &DataSourceMapper{KeyDriver: "odbc+postgres", KeyHost: "localhost", KeyPort: "port", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "dbname", "option1": ""},
		},
		{
			src: "https://localhost:8080/dbname?option1=1",
			dsm: &DataSourceMapper{KeyDriver: "https", KeyHost: "localhost", KeyPort: "8080", KeyLogin: "", KeyPassword: "", KeyDatabase: "dbname", "option1": "1"},
		},
		{
			src: "google.com",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "google.com", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: ""},
		},
		{
			src: "localhost",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "localhost", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: ""},
		},
		{
			src: "localhost:5432",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "localhost", KeyPort: "5432", KeyLogin: "", KeyPassword: "", KeyDatabase: ""},
		},
		{
			src: "example.com:443",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "example.com", KeyPort: "443", KeyLogin: "", KeyPassword: "", KeyDatabase: ""},
		},
		{
			src: "1.2.3.4:5432",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "1.2.3.4", KeyPort: "5432", KeyLogin: "", KeyPassword: "", KeyDatabase: ""},
		},
		{
			src: "localhost:5432/mydb?ssl=on",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "localhost", KeyPort: "5432", KeyLogin: "", KeyPassword: "", KeyDatabase: "mydb", "ssl": "on"},
		},
		{
			src: "./data/app.db",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "./data/app.db"},
		},
		{
			src: "/opt/app/x.db",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "/opt/app/x.db"},
		},
		{
			src: "./data/wordhunter.db",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "./data/wordhunter.db"},
		},
		{
			src: "tbot://123456789:@987654321:AAHb1-Cd_EfGhIjKlMnOpQrStUvWxYz0123456789",
			dsm: &DataSourceMapper{KeyDriver: "tbot", KeyHost: "987654321", KeyPort: "AAHb1-Cd_EfGhIjKlMnOpQrStUvWxYz0123456789", KeyLogin: "123456789", KeyPassword: "", KeyDatabase: ""},
		},
		{
			src: "tbot://123456789:@987654321:AAHb1-Cd_EfGhIjKlMnOpQrStUvWxYz0123456789/",
			dsm: &DataSourceMapper{KeyDriver: "tbot", KeyHost: "987654321", KeyPort: "AAHb1-Cd_EfGhIjKlMnOpQrStUvWxYz0123456789", KeyLogin: "123456789", KeyPassword: "", KeyDatabase: "/"},
		},
		{
			src: "sqlite://_:_@_:_/path",
			dsm: &DataSourceMapper{KeyDriver: "sqlite", KeyHost: "_", KeyPort: "_", KeyLogin: "_", KeyPassword: "_", KeyDatabase: "path"},
		},
		{
			src: "//host/db",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "host", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "db"},
		},
		{
			src: "//localhost:5432/mydb",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "localhost", KeyPort: "5432", KeyLogin: "", KeyPassword: "", KeyDatabase: "mydb"},
		},
		{
			src: "localhost:5432?ssl=on",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "localhost", KeyPort: "5432", KeyLogin: "", KeyPassword: "", KeyDatabase: "", "ssl": "on"},
		},
		{
			src: "google.com?foo=bar",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "google.com", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "", "foo": "bar"},
		},
		{
			src: "./data/app.db?cache=shared",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "./data/app.db", "cache": "shared"},
		},
		{
			src: "google.com/mydb",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "", KeyPort: "", KeyLogin: "", KeyPassword: "", KeyDatabase: "google.com/mydb"},
		},
		{
			// Authority form with a query but no /database: the ?opts tail must be
			// peeled before the regex so it does not land in the port.
			src: "mongodb://user:pass@localhost:27017?limit=20",
			dsm: &DataSourceMapper{KeyDriver: "mongodb", KeyHost: "localhost", KeyPort: "27017", KeyLogin: "user", KeyPassword: "pass", KeyDatabase: "", "limit": "20"},
		},
		{
			src: "postgres://localhost:5432?sslmode=disable",
			dsm: &DataSourceMapper{KeyDriver: "postgres", KeyHost: "localhost", KeyPort: "5432", KeyLogin: "", KeyPassword: "", KeyDatabase: "", "sslmode": "disable"},
		},
		{
			src: "redis://h:6379?db=0",
			dsm: &DataSourceMapper{KeyDriver: "redis", KeyHost: "h", KeyPort: "6379", KeyLogin: "", KeyPassword: "", KeyDatabase: "", "db": "0"},
		},
		{
			src: "//host:5432?ssl=on",
			dsm: &DataSourceMapper{KeyDriver: "", KeyHost: "host", KeyPort: "5432", KeyLogin: "", KeyPassword: "", KeyDatabase: "", "ssl": "on"},
		},
	}

	t.Run("successful", func(t *testing.T) {
		t.Parallel()
		for _, testcase := range successfulTestCases {
			t.Run(testcase.src, func(t *testing.T) {
				t.Parallel()
				got, err := Parse(testcase.src)
				require.NoError(t, err, testcase.src)
				require.Equal(t, testcase.dsm, got, testcase.src)
			})
		}
	})

	t.Run("telegram token via Addr", func(t *testing.T) {
		t.Parallel()
		// tbot://<chatid>:@<botid>:<secret>: consumers read Login()==chatid and
		// Addr()==<botid>:<secret>. A real bot-token secret contains '-'/'_', and a
		// trailing slash must change only Database(), never Addr()/Login().
		const (
			chatID = "123456789"
			botID  = "987654321"
			secret = "AAHb1-Cd_EfGhIjKlMnOpQrStUvWxYz0123456789"
		)
		token := botID + ":" + secret

		withoutSlash, err := Parse("tbot://" + chatID + ":@" + token)
		require.NoError(t, err)
		withSlash, err := Parse("tbot://" + chatID + ":@" + token + "/")
		require.NoError(t, err)

		require.Equal(t, chatID, withoutSlash.Login())
		require.Equal(t, token, withoutSlash.Addr())
		require.Equal(t, chatID, withSlash.Login())
		require.Equal(t, token, withSlash.Addr())
		require.Equal(t, withoutSlash.Login(), withSlash.Login())
		require.Equal(t, withoutSlash.Addr(), withSlash.Addr())
		require.Equal(t, "", withoutSlash.Database())
		require.Equal(t, "/", withSlash.Database())
	})

	t.Run("ipv6 does not panic", func(t *testing.T) {
		t.Parallel()
		// IPv6 authorities are out of scope (SplitN shreds them); guarantee only
		// that the input does not panic.
		require.NotPanics(t, func() { _, _ = Parse("[::1]:6379") })
	})

	t.Run("malformed options tail", func(t *testing.T) {
		t.Parallel()
		_, err := Parse("localhost:5432?%zz")
		require.Error(t, err)
	})

	t.Run("query parse error does not leak the input", func(t *testing.T) {
		t.Parallel()
		// Defensive hardening: an invalid percent-escape in the query reaches the
		// url.ParseQuery failure with a secret adjacent to the bad fragment; the
		// returned error must be the static message and never echo the secret.
		const secret = "SECRET"
		_, err := Parse("x://h/d?tok=%zz" + secret)
		require.Error(t, err)
		require.NotContains(t, err.Error(), secret)
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		// Boundary: Parse("") reshapes to "//" and yields database="/". Harmless —
		// Unmarshal rejects empty input before Parse is reached.
		got, err := Parse("")
		require.NoError(t, err)
		require.Equal(t, "/", got.Database())
	})
}
