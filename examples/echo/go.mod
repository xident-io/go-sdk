// This example is its own Go module so that the framework dependency does not
// leak into the root SDK module (the SDK stays dependency-free). The `replace`
// directive points at the local SDK checkout so the example always builds
// against the code in this repository.
module github.com/xident-io/go-sdk/examples/echo

go 1.23.0

require (
	github.com/labstack/echo/v4 v4.13.4
	github.com/xident-io/go-sdk v0.0.0
)

require (
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/text v0.25.0 // indirect
)

replace github.com/xident-io/go-sdk => ../..
