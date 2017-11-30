# Dividat Driver

Dividat drivers and hardware test suites.

## Prerequisites

-   [Go](https://golang.org/)
-   [Glide](https://glide.sh/)

For packaging:

-   [UPX](https://upx.github.io/)
-   osslsigncode (macOS: `brew install osslsigncode`)
-   AWS CLI (macOS: `brew install awscli`)

## Compatibility

Firefox, Safari and Edge not supported as they are not yet properly implementing _loopback as a trustworthy origin_, see:

-   Firefox (tracking): <https://bugzilla.mozilla.org/show_bug.cgi?id=1376309>
-   Edge: <https://developer.microsoft.com/en-us/microsoft-edge/platform/issues/11963735/>
-   Safari: <https://bugs.webkit.org/show_bug.cgi?id=171934>

## Quick start

-   Clone repository, this will be the $GOPATH

-   In your editor settings, set $GOPATH to repo if needed

-   Install dependencies:

        make deps

-   Build and run

        make
        ./bin/dividat-driver

    Or use `go run`:

        go run src/cmd/dividat-driver/main.go start

## Tests

Run the test suite with: `npm test`.

## Tools

### Data replayer

Logged data can be replayed for debugging purposes.

For default settings: `npm run replay`

To replay an other recording: `npm run replay -- rec/simple.dat`

To slow down the replay: `npm run replay -- -t 100`
