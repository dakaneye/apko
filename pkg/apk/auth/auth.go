package auth

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chainguard-dev/clog"
	"golang.org/x/time/rate"
)

// DefaultAuthenticators is a list of authenticators that are used by default.
var DefaultAuthenticators Authenticator = multiAuthenticator{EnvAuth{}, CGRAuth{}}

// Authenticator is an interface for types that can add HTTP basic auth to a
// request.
type Authenticator interface {
	AddAuth(ctx context.Context, req *http.Request) error
}

// MultiAuthenticator returns an Authenticator that tries each of the given
// authenticators in order until one of them adds auth to the request.
func MultiAuthenticator(auths ...Authenticator) Authenticator { return multiAuthenticator(auths) }

type multiAuthenticator []Authenticator

func (m multiAuthenticator) AddAuth(ctx context.Context, req *http.Request) error {
	for _, a := range m {
		if _, _, ok := req.BasicAuth(); ok {
			// The request has auth, so we can stop here.
			return nil
		}
		if err := a.AddAuth(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// EnvAuth adds HTTP basic auth to the request if the request URL matches the
// HTTP_AUTH environment variable.
type EnvAuth struct{}

func (e EnvAuth) AddAuth(_ context.Context, req *http.Request) error {
	env := os.Getenv("HTTP_AUTH")
	parts := strings.Split(env, ":")
	if len(parts) != 4 || parts[0] != "basic" {
		return nil
	}
	if req.URL.Host == parts[1] {
		req.SetBasicAuth(parts[2], parts[3])
	}
	return nil
}

// CGRAuth adds HTTP basic auth to the request if the request URL matches
// apk.cgr.dev and the `chainctl` command is available.
//
// It executes `chainctl` to get a token. If you need to assume an identity
// then you should use NewChainguardIdentityAuth instead.
type CGRAuth struct{}

var sometimes = rate.Sometimes{Interval: 10 * time.Minute}
var tok string

func (c CGRAuth) AddAuth(ctx context.Context, req *http.Request) error {
	log := clog.FromContext(ctx)

	host := "apk.cgr.dev"
	// TODO(jason): Use a more general way to get the host.
	if h := os.Getenv("APKO_APK_HOST"); h != "" {
		host = h
	}
	if req.Host != host {
		return nil
	}

	sometimes.Do(func() {
		out, err := exec.CommandContext(ctx, "chainctl", "auth", "token", "--audience", host).Output()
		if err != nil {
			log.Warnf("Error running `chainctl auth token`: %v", err)
			return
		}
		tok = string(out)
	})
	req.SetBasicAuth("user", tok)
	return nil
}

// StaticAuth is an Authenticator that adds HTTP basic auth to the request if
// the request URL matches the given domain.
func StaticAuth(domain, user, pass string) Authenticator {
	return staticAuth{domain, user, pass}
}

type staticAuth struct{ domain, user, pass string }

func (s staticAuth) AddAuth(_ context.Context, req *http.Request) error {
	if req.Host == s.domain {
		req.SetBasicAuth(s.user, s.pass)
	}
	return nil
}
