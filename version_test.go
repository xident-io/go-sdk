package xident

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

// releaseVersionEnv carries the git tag being released into the guard below.
// It is set by .github/workflows/release.yml and by nothing else.
const releaseVersionEnv = "XIDENT_RELEASE_VERSION"

// bareSemverRE matches MAJOR.MINOR.PATCH with no leading "v" and no suffix.
//
// The shape matters in two directions: SDKVersion is concatenated into the
// User-Agent, and the release guard compares it against the git tag with the
// leading "v" stripped. A value like "v1.3.0" or "1.3" would break one or the
// other silently.
var bareSemverRE = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

func TestSDKVersionIsBareSemver(t *testing.T) {
	if !bareSemverRE.MatchString(SDKVersion) {
		t.Errorf("SDKVersion = %q, want bare MAJOR.MINOR.PATCH with no leading %q", SDKVersion, "v")
	}
}

func TestDefaultUserAgentDerivesFromSDKVersion(t *testing.T) {
	want := "Xident-Go/" + SDKVersion
	if defaultUserAgent != want {
		t.Errorf("defaultUserAgent = %q, want %q -- it must be built from SDKVersion, not hardcoded", defaultUserAgent, want)
	}
}

// TestSDKVersionReachesTheWire pins the behaviour that actually broke.
//
// SDKVersion sat at "1.0.0" while v1.1.0 and v1.2.0 shipped, so every request
// from those builds told the API it was a 1.0.0 client. Asserting the constant
// in isolation would not have caught a User-Agent that stopped deriving from
// it, so assert the value that leaves the process.
func TestSDKVersionReachesTheWire(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	got := make(chan string, 1)
	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("User-Agent")
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	})

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}
	if _, err := client.do(context.Background(), req, nil); err != nil {
		t.Fatalf("do() error: %v", err)
	}

	want := "Xident-Go/" + SDKVersion
	if sent := <-got; sent != want {
		t.Errorf("outbound User-Agent = %q, want %q", sent, want)
	}
}

// TestSDKVersionMatchesReleaseTag fails a release whose git tag disagrees with
// SDKVersion.
//
// go-sdk has no manifest -- `go get` resolves the version straight from the git
// tag -- so the tag and this constant are the only two places a version exists,
// and nothing inside the source tree can observe the tag. Reading it here with
// `git describe` would be worse than no check at all: CI clones shallow and
// without tags, and module consumers get a tarball from the Go module proxy
// with no .git directory whatsoever. The lookup would fail on exactly the
// machines that matter and pass on a developer laptop, so its verdict would
// carry no information.
//
// The tag is therefore INJECTED rather than discovered. On a tag push GitHub
// hands the workflow the tag name directly, needing no history and no fetch
// depth; the workflow puts it in the environment and runs this test. The
// comparison rule lives here in Go, versioned and reviewable, instead of in a
// shell one-liner that nothing tests.
//
// Outside a release the variable is unset and there is no tag to compare
// against yet, so the check does not apply.
func TestSDKVersionMatchesReleaseTag(t *testing.T) {
	tag := os.Getenv(releaseVersionEnv)
	if tag == "" {
		t.Skipf("%s is unset -- this guard runs only on a tag push (see .github/workflows/release.yml)", releaseVersionEnv)
	}

	want := strings.TrimPrefix(tag, "v")
	if SDKVersion != want {
		t.Fatalf("SDKVersion = %q but the release tag is %q.\n"+
			"Set SDKVersion in config.go to %q, or re-cut the tag to match.",
			SDKVersion, tag, want)
	}
}
