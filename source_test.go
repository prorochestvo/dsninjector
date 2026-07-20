package dsninjector

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	_ DataSource   = &DataSourceMapper{}
	_ fmt.Stringer = &DataSourceMapper{}
)

func TestDataSourceMapper_Addr(t *testing.T) {
	testcases := []struct {
		name        string
		mapper      DataSourceMapper
		defaultPort []int
		expected    string
	}{
		{
			name:     "With hostname and port",
			mapper:   DataSourceMapper{"hostname": "localhost", "port": "5432"},
			expected: "localhost:5432",
		},
		{
			name:        "With hostname and default port",
			mapper:      DataSourceMapper{"hostname": "localhost"},
			defaultPort: []int{3306},
			expected:    "localhost:3306",
		},
		{
			name:     "With empty port and without default port",
			mapper:   DataSourceMapper{"hostname": "localhost"},
			expected: "localhost",
		},
		{
			name:     "Without any information",
			mapper:   DataSourceMapper{},
			expected: "",
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, test.mapper.Addr(test.defaultPort...))
		})
	}
}

func TestDataSourceMapper_AuthBasicBase64(t *testing.T) {
	testcases := []struct {
		name     string
		mapper   DataSourceMapper
		expected string
	}{
		{
			name:     "Basic Authentication",
			mapper:   DataSourceMapper{"login": "user", "password": "pass"},
			expected: base64.StdEncoding.EncodeToString([]byte("user:pass")),
		},
		{
			name:     "Empty credentials",
			mapper:   DataSourceMapper{"login": "", "password": ""},
			expected: base64.StdEncoding.EncodeToString([]byte(":")),
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, test.mapper.AuthBasicBase64())
		})
	}
}

func TestDataSourceMapper_Driver(t *testing.T) {
	testcases := []struct {
		name     string
		mapper   DataSourceMapper
		expected string
	}{
		{
			name:     "With driver",
			mapper:   DataSourceMapper{"driver": "postgres"},
			expected: "postgres",
		},
		{
			name:     "Without driver",
			mapper:   DataSourceMapper{"driver": ""},
			expected: "",
		},
	}

	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, test.mapper.Driver())
		})
	}
}

func TestDataSourceMapper_SetDriver(t *testing.T) {
	t.Parallel()
	ds := DataSourceMapper{}
	ds.SetDriver("postgres")
	require.Equal(t, "postgres", ds.Driver())
	require.Equal(t, "postgres", ds.Option(KeyDriver))
	ds.SetDriver("mysql")
	require.Equal(t, "mysql", ds.Driver())
}

func TestDataSourceMapper_Host(t *testing.T) {
	tests := []struct {
		name     string
		mapper   DataSourceMapper
		expected string
	}{
		{
			name:     "With hostname",
			mapper:   DataSourceMapper{"hostname": "localhost"},
			expected: "localhost",
		},
		{
			name:     "Without hostname",
			mapper:   DataSourceMapper{"hostname": ""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.mapper.Host())
		})
	}
}

func TestDataSourceMapper_SetHost(t *testing.T) {
	t.Parallel()
	ds := DataSourceMapper{}
	ds.SetHost("localhost")
	require.Equal(t, "localhost", ds.Host())
	require.Equal(t, "localhost", ds.Option(KeyHost))
}

func TestDataSourceMapper_Port(t *testing.T) {
	tests := []struct {
		name     string
		mapper   DataSourceMapper
		expected int
	}{
		{
			name:     "With port",
			mapper:   DataSourceMapper{"port": "3306"},
			expected: 3306,
		},
		{
			name:     "Without port",
			mapper:   DataSourceMapper{"port": ""},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.mapper.Port())
		})
	}
}

func TestDataSourceMapper_SetPort(t *testing.T) {
	t.Parallel()
	ds := DataSourceMapper{}
	ds.SetPort(5432)
	require.Equal(t, 5432, ds.Port())
	require.Equal(t, "5432", ds.Option(KeyPort))
}

func TestDataSourceMapper_Login(t *testing.T) {
	tests := []struct {
		name     string
		mapper   DataSourceMapper
		expected string
	}{
		{
			name:     "With login",
			mapper:   DataSourceMapper{"login": "login"},
			expected: "login",
		},
		{
			name:     "Without login",
			mapper:   DataSourceMapper{"login": ""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.mapper.Login())
		})
	}
}

func TestDataSourceMapper_SetLogin(t *testing.T) {
	t.Parallel()
	ds := DataSourceMapper{}
	ds.SetLogin("user")
	require.Equal(t, "user", ds.Login())
	require.Equal(t, "user", ds.Option(KeyLogin))
}

func TestDataSourceMapper_Password(t *testing.T) {
	tests := []struct {
		name     string
		mapper   DataSourceMapper
		expected string
	}{
		{
			name:     "With password",
			mapper:   DataSourceMapper{"password": "password"},
			expected: "password",
		},
		{
			name:     "Without password",
			mapper:   DataSourceMapper{"password": ""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.mapper.Password())
		})
	}
}

func TestDataSourceMapper_SetPassword(t *testing.T) {
	t.Parallel()
	ds := DataSourceMapper{}
	ds.SetPassword("secret")
	require.Equal(t, "secret", ds.Password())
	require.Equal(t, "secret", ds.Option(KeyPassword))
}

func TestDataSourceMapper_Database(t *testing.T) {
	tests := []struct {
		name     string
		mapper   DataSourceMapper
		expected string
	}{
		{
			name:     "With database",
			mapper:   DataSourceMapper{"database": "database"},
			expected: "database",
		},
		{
			name:     "Without database",
			mapper:   DataSourceMapper{"database": ""},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.mapper.Database())
		})
	}
}

func TestDataSourceMapper_SetDatabase(t *testing.T) {
	t.Parallel()
	ds := DataSourceMapper{}
	ds.SetDatabase("dbname")
	require.Equal(t, "dbname", ds.Database())
	require.Equal(t, "dbname", ds.Option(KeyDatabase))
}

func TestDataSourceMapper_OptionsNames(t *testing.T) {
	t.Parallel()

	t.Run("excludes reserved keys, includes free-form", func(t *testing.T) {
		t.Parallel()
		ds := DataSourceMapper{
			KeyDriver: "postgres", KeyHost: "h", KeyPort: "5432",
			KeyLogin: "u", KeyPassword: "p", KeyDatabase: "db",
			"sslmode": "disable", "timeout": "30",
		}
		require.ElementsMatch(t, []string{"sslmode", "timeout"}, ds.OptionsNames())
	})

	t.Run("empty when only reserved keys present", func(t *testing.T) {
		t.Parallel()
		ds := DataSourceMapper{KeyDriver: "postgres", KeyHost: "h"}
		require.Empty(t, ds.OptionsNames())
	})

	t.Run("empty mapper", func(t *testing.T) {
		t.Parallel()
		ds := DataSourceMapper{}
		require.Empty(t, ds.OptionsNames())
	})
}

func TestDataSourceMapper_Option(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mapper   DataSourceMapper
		key      string
		def      []string
		expected string
	}{
		{"present returns value", DataSourceMapper{"k": "v"}, "k", nil, "v"},
		{"present but empty returns empty not default", DataSourceMapper{"k": ""}, "k", []string{"fallback"}, ""},
		{"absent returns single default", DataSourceMapper{}, "k", []string{"fallback"}, "fallback"},
		{"absent returns joined defaults", DataSourceMapper{}, "k", []string{"a", "b"}, "a;b"},
		{"absent without default returns empty", DataSourceMapper{}, "k", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.mapper.Option(tt.key, tt.def...))
		})
	}
}

func TestDataSourceMapper_SetOption(t *testing.T) {
	t.Parallel()

	t.Run("sets on existing map", func(t *testing.T) {
		t.Parallel()
		ds := DataSourceMapper{}
		ds.SetOption("k", "v")
		require.Equal(t, "v", ds.Option("k"))
	})

	t.Run("allocates a nil-backed map", func(t *testing.T) {
		t.Parallel()
		var ds DataSourceMapper // nil map
		ds.SetOption("k", "v")
		require.Equal(t, "v", ds.Option("k"))
	})

	t.Run("overwrites an existing value", func(t *testing.T) {
		t.Parallel()
		ds := DataSourceMapper{"k": "old"}
		ds.SetOption("k", "new")
		require.Equal(t, "new", ds.Option("k"))
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("field options set only their fields", func(t *testing.T) {
		t.Parallel()
		ds := New(WithDriver("postgres"), WithHost("localhost"), WithPort(5432))
		require.Equal(t, "postgres", ds.Driver())
		require.Equal(t, "localhost", ds.Host())
		require.Equal(t, 5432, ds.Port())
		require.Equal(t, "", ds.Login())
		require.Empty(t, ds.OptionsNames())
	})

	t.Run("empty construction is non-nil and interface-usable", func(t *testing.T) {
		t.Parallel()
		ds := New()
		require.NotNil(t, ds)
		accept := func(DataSource) {}
		accept(ds) // proves *DataSourceMapper satisfies DataSource
		require.Equal(t, "", ds.Driver())
	})

	t.Run("explicit zero values are set, not absent", func(t *testing.T) {
		t.Parallel()
		ds := New(WithLogin(""), WithPort(0))
		// A present-empty key returns "" rather than the supplied default.
		require.Equal(t, "", ds.Option(KeyLogin, "sentinel"))
		require.Equal(t, "0", ds.Option(KeyPort, "sentinel"))
	})

	t.Run("option appears in OptionsNames", func(t *testing.T) {
		t.Parallel()
		ds := New(WithOption("sslmode", "disable"))
		require.Equal(t, "disable", ds.Option("sslmode"))
		require.Contains(t, ds.OptionsNames(), "sslmode")
	})

	t.Run("all field and option constructors", func(t *testing.T) {
		t.Parallel()
		ds := New(
			WithDriver("postgres"),
			WithHost("localhost"),
			WithPort(5432),
			WithLogin("user"),
			WithPassword("pass"),
			WithDatabase("db"),
			WithOption("k", "v"),
		)
		require.Equal(t, "postgres", ds.Driver())
		require.Equal(t, "localhost", ds.Host())
		require.Equal(t, 5432, ds.Port())
		require.Equal(t, "user", ds.Login())
		require.Equal(t, "pass", ds.Password())
		require.Equal(t, "db", ds.Database())
		require.Equal(t, "v", ds.Option("k"))
	})
}

func TestDataSourceMapper_Redacted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{"masks a non-empty password", "postgres://user:password@localhost:5432/db", "postgres://user:xxxxx@localhost:5432/db"},
		{"no mask when password empty, login kept", "mysql://user:@localhost/db", "mysql://user:@localhost/db"},
		{"omits credentials when both empty", "mysql://:@localhost/db", "mysql://localhost/db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ds, err := Parse(tt.src)
			require.NoError(t, err)
			require.Equal(t, tt.expected, ds.Redacted())
		})
	}

	t.Run("password value never appears in output", func(t *testing.T) {
		t.Parallel()
		ds := New(WithDriver("postgres"), WithLogin("u"), WithPassword("s3cr3t"), WithHost("h"))
		require.NotContains(t, ds.Redacted(), "s3cr3t")
	})
}

func TestDataSourceMapper_String(t *testing.T) {
	t.Parallel()

	ds := New(WithDriver("postgres"), WithLogin("user"), WithPassword("s3cr3t"),
		WithHost("localhost"), WithPort(5432), WithDatabase("db"))

	require.Equal(t, ds.Redacted(), ds.String())
	require.NotContains(t, ds.String(), "s3cr3t")
	// %v on the pointer routes through String(), so logging it is secret-safe.
	require.NotContains(t, fmt.Sprintf("%v", ds), "s3cr3t")
}

func TestDataSourceMapper_OptionInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mapper   DataSourceMapper
		def      []int
		expected int
	}{
		{"present valid", DataSourceMapper{"limit": "42"}, nil, 42},
		{"present negative", DataSourceMapper{"limit": "-5"}, nil, -5},
		{"present empty returns default", DataSourceMapper{"limit": ""}, []int{100}, 100},
		{"present garbage returns default", DataSourceMapper{"limit": "abc"}, []int{100}, 100},
		{"absent with default", DataSourceMapper{}, []int{100}, 100},
		{"absent without default returns zero", DataSourceMapper{}, nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.mapper.OptionInt("limit", tt.def...))
		})
	}
}

func TestDataSourceMapper_OptionBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mapper   DataSourceMapper
		def      []bool
		expected bool
	}{
		{"present true", DataSourceMapper{"tls": "true"}, nil, true},
		{"present 1", DataSourceMapper{"tls": "1"}, nil, true},
		{"present false overrides default", DataSourceMapper{"tls": "false"}, []bool{true}, false},
		{"present 0 overrides default", DataSourceMapper{"tls": "0"}, []bool{true}, false},
		{"present garbage returns default", DataSourceMapper{"tls": "yes"}, []bool{true}, true},
		{"present empty returns default", DataSourceMapper{"tls": ""}, []bool{true}, true},
		{"absent with default", DataSourceMapper{}, []bool{true}, true},
		{"absent without default returns zero", DataSourceMapper{}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.mapper.OptionBool("tls", tt.def...))
		})
	}
}

func TestDataSourceMapper_OptionDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mapper   DataSourceMapper
		def      []time.Duration
		expected time.Duration
	}{
		{"present seconds", DataSourceMapper{"timeout": "30s"}, nil, 30 * time.Second},
		{"present minutes", DataSourceMapper{"timeout": "5m"}, nil, 5 * time.Minute},
		{"present unitless returns default", DataSourceMapper{"timeout": "30"}, []time.Duration{time.Second}, time.Second},
		{"present garbage returns default", DataSourceMapper{"timeout": "abc"}, []time.Duration{time.Second}, time.Second},
		{"present empty returns default", DataSourceMapper{"timeout": ""}, []time.Duration{time.Second}, time.Second},
		{"absent with default", DataSourceMapper{}, []time.Duration{30 * time.Second}, 30 * time.Second},
		{"absent without default returns zero", DataSourceMapper{}, nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.mapper.OptionDuration("timeout", tt.def...))
		})
	}
}

func TestDataSourceMapper_Require(t *testing.T) {
	t.Parallel()

	t.Run("no keys returns nil", func(t *testing.T) {
		t.Parallel()
		ds := New(WithDriver("x"))
		require.NoError(t, ds.Require())
	})

	t.Run("all present and non-empty", func(t *testing.T) {
		t.Parallel()
		ds := New(WithLogin("u"), WithPassword("p"))
		require.NoError(t, ds.Require(KeyLogin, KeyPassword))
	})

	t.Run("one missing is named, present is not", func(t *testing.T) {
		t.Parallel()
		ds := New(WithLogin("u"))
		err := ds.Require(KeyLogin, KeyPassword)
		require.Error(t, err)
		require.Contains(t, err.Error(), KeyPassword)
		require.NotContains(t, err.Error(), KeyLogin)
	})

	t.Run("present but empty counts as missing", func(t *testing.T) {
		t.Parallel()
		ds := New(WithLogin("u"), WithPassword(""))
		err := ds.Require(KeyLogin, KeyPassword)
		require.Error(t, err)
		require.Contains(t, err.Error(), KeyPassword)
	})

	t.Run("multiple missing named in argument order", func(t *testing.T) {
		t.Parallel()
		ds := New(WithHost("h"))
		err := ds.Require(KeyLogin, KeyPassword, KeyDatabase)
		require.Error(t, err)
		require.Contains(t, err.Error(), "login, password, database")
	})

	t.Run("names the key never the value", func(t *testing.T) {
		t.Parallel()
		// A present required field holds a real secret; a different required field
		// is missing so an error is produced. The error must name the missing key
		// and never echo the present field's secret value.
		const secret = "s3cr3t-token-value"
		ds := New(WithPassword(secret))
		err := ds.Require(KeyLogin, KeyPassword)
		require.Error(t, err)
		require.Contains(t, err.Error(), KeyLogin)       // missing key is named
		require.NotContains(t, err.Error(), KeyPassword) // present key is not named
		require.NotContains(t, err.Error(), secret)      // secret value never leaks
	})
}
