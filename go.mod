module github.com/Neo-Desktop/bbs-sec

go 1.24.0

toolchain go1.24.7

require (
	github.com/coder/websocket v1.8.13
	github.com/dop251/goja v0.0.0-20260603125802-cfe4039cb6d7
	github.com/ergochat/irc-go v0.7.0
	github.com/fclairamb/ftpserverlib v0.30.0
	github.com/go-sql-driver/mysql v1.8.1
	github.com/jackc/pgx/v5 v5.7.2
	github.com/jlaffaye/ftp v0.2.0
	github.com/racingmars/go3270 v0.9.13
	github.com/spf13/afero v1.15.0
	golang.org/x/crypto v0.31.0
	golang.org/x/image v0.23.0
	golang.org/x/sys v0.40.0
	golang.org/x/term v0.27.0
	golang.org/x/text v0.28.0
	google.golang.org/protobuf v1.36.6
	modernc.org/sqlite v1.33.1
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20240409012703-83162a5b38cd // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sync v0.16.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)

replace github.com/racingmars/go3270 => github.com/Neo-Desktop/go3270-sec v0.9.14-0.20261018223830-6c7224eb64db
