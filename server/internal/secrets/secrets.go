// Package secrets resolves a secret from an external source instead of keeping
// it in a config file: a password manager, a file, the environment or any
// command. It is used for the vault passphrase and for integration tokens.
//
// A reference looks like "scheme:value" with optional parameters:
//
//	env:LUMEN_PASSPHRASE
//	file:/run/secrets/lumen
//	command:pass show lumen/vault
//	bitwarden:lumen-vault
//	keepass:D:/keys/personal.kdbx#Lumen/vault?password=env:KDBX_PASSWORD
//
// Parameter values may themselves be references, which is how a KeePass
// database password can come from the environment. Resolution never logs or
// persists the secret itself.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

var (
	ErrUnknownScheme = errors.New("unknown secret scheme")
	ErrEmptySecret   = errors.New("secret is empty")
	ErrTooDeep       = errors.New("secret reference nested too deeply")
)

// maxDepth bounds nested references so a cyclic config cannot hang the process.
const maxDepth = 4

// Spec is a parsed secret reference.
type Spec struct {
	Scheme   string
	Value    string     // everything before '?', with '#fragment' kept in Value
	Fragment string     // part after '#', used by keepass entry paths
	Params   url.Values // parsed from '?...'
	Raw      string
}

// Parse splits "scheme:value#fragment?params" without touching the filesystem.
func Parse(ref string) (Spec, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Spec{}, fmt.Errorf("%w: empty reference", ErrUnknownScheme)
	}
	scheme, rest, ok := strings.Cut(ref, ":")
	// A bare path is the friendliest default for --passphrase-from, and a
	// single-letter "scheme" is a Windows drive letter, not a provider.
	if !ok || len(scheme) == 1 {
		return Spec{Scheme: "file", Value: ref, Raw: ref, Params: url.Values{}}, nil
	}
	spec := Spec{Scheme: strings.ToLower(strings.TrimSpace(scheme)), Raw: ref, Params: url.Values{}}

	if q := strings.LastIndex(rest, "?"); q >= 0 {
		params, err := url.ParseQuery(rest[q+1:])
		if err != nil {
			return Spec{}, fmt.Errorf("bad parameters in %q: %w", ref, err)
		}
		spec.Params = params
		rest = rest[:q]
	}
	if h := strings.LastIndex(rest, "#"); h >= 0 {
		spec.Fragment = rest[h+1:]
		rest = rest[:h]
	}
	spec.Value = rest
	return spec, nil
}

// Provider turns one reference into a secret.
type Provider interface {
	// Scheme is the prefix this provider answers to.
	Scheme() string
	// Resolve returns the secret. The resolver is passed in so a provider can
	// resolve nested references such as a database password.
	Resolve(ctx context.Context, spec Spec, r *Resolver) ([]byte, error)
	// Describe is a human-readable summary for logs — it must never include
	// the secret itself.
	Describe(spec Spec) string
}

// Resolver looks up references using the registered providers.
type Resolver struct {
	providers map[string]Provider
	runner    Runner
	depth     int
}

// New builds a resolver with every built-in provider.
func New(runner Runner) *Resolver {
	if runner == nil {
		runner = ExecRunner{}
	}
	r := &Resolver{providers: map[string]Provider{}, runner: runner}
	for _, p := range []Provider{
		envProvider{},
		fileProvider{},
		literalProvider{},
		commandProvider{},
		bitwardenProvider{},
		keepassProvider{},
	} {
		r.providers[p.Scheme()] = p
	}
	return r
}

// Register adds or replaces a provider; used by tests and future backends.
func (r *Resolver) Register(p Provider) { r.providers[p.Scheme()] = p }

// Runner exposes the command runner to providers.
func (r *Resolver) Runner() Runner { return r.runner }

// Resolve returns the secret a reference points at, trimmed of the trailing
// newline that files and CLIs normally add.
func (r *Resolver) Resolve(ctx context.Context, ref string) ([]byte, error) {
	if r.depth >= maxDepth {
		return nil, fmt.Errorf("%w: %s", ErrTooDeep, ref)
	}
	spec, err := Parse(ref)
	if err != nil {
		return nil, err
	}
	p, ok := r.providers[spec.Scheme]
	if !ok {
		return nil, fmt.Errorf("%w: %q in %q", ErrUnknownScheme, spec.Scheme, ref)
	}

	nested := &Resolver{providers: r.providers, runner: r.runner, depth: r.depth + 1}
	value, err := p.Resolve(ctx, spec, nested)
	if err != nil {
		// The reference may name an item, never a secret, so it is safe to show.
		return nil, fmt.Errorf("%s: %w", p.Describe(spec), err)
	}
	value = trimSecret(value)
	if len(value) == 0 {
		return nil, fmt.Errorf("%s: %w", p.Describe(spec), ErrEmptySecret)
	}
	return value, nil
}

// Describe renders a reference for logs and error messages.
func (r *Resolver) Describe(ref string) string {
	spec, err := Parse(ref)
	if err != nil {
		return "секрет"
	}
	if p, ok := r.providers[spec.Scheme]; ok {
		return p.Describe(spec)
	}
	return spec.Scheme
}

// param returns a parameter value, falling back to a default.
func (s Spec) param(key, def string) string {
	if v := s.Params.Get(key); v != "" {
		return v
	}
	return def
}

// trimSecret drops the trailing newline CLIs and files add, but keeps any
// deliberate leading or inner whitespace of a passphrase.
func trimSecret(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

type envProvider struct{}

func (envProvider) Scheme() string { return "env" }

func (envProvider) Describe(s Spec) string {
	return "переменная окружения " + s.Value
}

func (envProvider) Resolve(_ context.Context, s Spec, _ *Resolver) ([]byte, error) {
	v, ok := os.LookupEnv(s.Value)
	if !ok {
		return nil, fmt.Errorf("переменная %s не задана", s.Value)
	}
	return []byte(v), nil
}

type fileProvider struct{}

func (fileProvider) Scheme() string { return "file" }

func (fileProvider) Describe(s Spec) string { return "файл " + s.Value }

func (fileProvider) Resolve(_ context.Context, s Spec, _ *Resolver) ([]byte, error) {
	data, err := os.ReadFile(s.Value)
	if err != nil {
		return nil, err
	}
	// Only the first line is the secret; the rest may be a comment.
	if i := strings.IndexAny(string(data), "\r\n"); i >= 0 {
		data = data[:i]
	}
	return data, nil
}

// literalProvider exists for tests and throwaway setups. It is deliberately
// unattractive: the value ends up in shell history and process listings.
type literalProvider struct{}

func (literalProvider) Scheme() string { return "literal" }

func (literalProvider) Describe(Spec) string { return "литерал" }

func (literalProvider) Resolve(_ context.Context, s Spec, _ *Resolver) ([]byte, error) {
	return []byte(s.Value), nil
}
