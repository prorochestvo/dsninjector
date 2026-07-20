# DSNInjector

A tiny, zero-dependency Go library for parsing, building, and loading DSNs
(Data Source Names / connection strings) of the form
`driver://login:password@host:port/database?opt=val`. One generic parser serves
databases (PostgreSQL, MySQL, MongoDB, Redis, SQLite, …) and any other
scheme-shaped config — HTTP endpoints, message-bus URLs, API base URLs.

## Features

- Parse a DSN into a map-backed value object with typed accessors (`Driver`,
  `Host`, `Port`, `Login`, `Password`, `Database`, `Addr`) and query options,
  including typed reads: `OptionInt`, `OptionBool`, `OptionDuration`.
- Build a DSN programmatically with `New` + `WithHost`/`WithPort`/… , rebuild any
  DSN with `Marshal`, and validate required fields with `Require`.
- Read a DSN straight from an environment variable (`Unmarshal` /
  `UnmarshalOrEmpty`, with fallback defaults) and load `.env`-style files with
  `InitEnvFrom`.
- Log safely with `Redacted()` (masks the password); `MustParse` for tests and
  init-time configuration.
- Standard library only — no third-party runtime dependencies, no CGO.

## Install

```bash
go get github.com/prorochestvo/dsninjector
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/prorochestvo/dsninjector"
)

func main() {
	ds, err := dsninjector.Parse("mysql://user:password@localhost:3306/dbname?charset=utf8")
	if err != nil {
		panic(err)
	}

	fmt.Println(ds.Driver())          // "mysql"
	fmt.Println(ds.Addr())            // "localhost:3306"
	fmt.Println(ds.Port())            // 3306
	fmt.Println(ds.Database())        // "dbname"
	fmt.Println(ds.Option("charset")) // "utf8"
}
```

Read a DSN from the environment, optionally loading a `.env` file first:

```go
_ = dsninjector.InitEnvFrom(".env") // missing files are skipped

ds, err := dsninjector.Unmarshal("DATABASE_DSN", "postgres://localhost:5432/app")
if err != nil {
	panic(err)
}
_ = ds
```

Build one programmatically and log it without leaking the password:

```go
ds := dsninjector.New(
	dsninjector.WithDriver("postgres"),
	dsninjector.WithHost("db.internal"),
	dsninjector.WithPort(5432),
	dsninjector.WithLogin("app"),
	dsninjector.WithPassword("s3cret"),
	dsninjector.WithDatabase("app"),
)
fmt.Println(ds.Redacted()) // postgres://app:xxxxx@db.internal:5432/app
```

## Notes

- A scheme is optional: `localhost:5432`, `example.com`, and filesystem paths
  parse without a `driver://` prefix.
- Reserved fields are `driver`, `hostname`, `port`, `login`, `password`,
  `database`; every other query-string key is a free-form option.
- Getters coerce on read and never panic — `Port` returns `0` for an empty or
  non-numeric port.
- Credentials without a colon are treated as password-only:
  `redis://secret@host` yields `Password() == "secret"` and `Login() == ""`.
- **Secrets:** `Marshal` emits credentials in cleartext and `Redacted()` masks
  only the password field — never log a DSN whose secret rides in the host, port,
  or a query option (`?apikey=…`). `AuthBasicBase64` is encoding, not secrecy.

## License

[MIT](LICENSE)
