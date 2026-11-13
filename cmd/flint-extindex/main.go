// Command flint-extindex makes the signed extension index Flint installs
// from (docs/design/extensions.md, "Distribution"; internal/extensions/index).
// It is what the release workflow of flint-ide/extensions runs.
//
//	flint-extindex keygen
//	    Prints a new key pair. The public key goes into Flint
//	    (index.OfficialPublicKey) or into extensions.indexPublicKey; the
//	    secret key goes into the signing secret and nowhere else.
//
//	flint-extindex build -out <dir> -base-url <https://…/> <input>…
//	    Packs each input and writes <dir>/index.json. An input is a built
//	    extension folder or a .tar.gz of one, as <platform>=<path> for an
//	    extension with a program (darwin-arm64, darwin-amd64, linux-amd64,
//	    linux-arm64) and as a plain <path> for one without (packed once, as
//	    "any"). -base-url is where the files of <dir> will be served.
//
//	flint-extindex sign [-key-env FLINT_EXTINDEX_KEY | -key-file <file>] <index.json>
//	    Writes <index.json>.sig. The secret key is read from the environment
//	    variable or the file, never from the command line.
//
//	flint-extindex verify -key <public key> [-archives <dir>] <index.json>
//	    Checks the signature (<index.json>.sig) and the index's content; with
//	    -archives, also that every archive in <dir> has the index's size and
//	    SHA-256.
//
// Exit status 1 means the command failed; 2, that it was called wrongly.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"flint/internal/extensions/index"
)

// DefaultKeyEnv is the environment variable sign reads the secret key from.
const DefaultKeyEnv = "FLINT_EXTINDEX_KEY"

const usage = `usage:
  flint-extindex keygen
  flint-extindex build -out <dir> -base-url <https://…/> [<platform>=]<folder or .tar.gz>…
  flint-extindex sign [-key-env ` + DefaultKeyEnv + ` | -key-file <file>] <index.json>
  flint-extindex verify -key <public key> [-archives <dir>] <index.json>
`

// errUsage: the command line is wrong (exit status 2).
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "flint-extindex:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	parse := func() error {
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(stderr, "flint-extindex:", err)
			return errUsage
		}
		return nil
	}
	switch args[0] {
	case "keygen":
		if err := parse(); err != nil || fs.NArg() != 0 {
			return errUsage
		}
		return keygen(stdout)
	case "build":
		out := fs.String("out", "", "")
		base := fs.String("base-url", "", "")
		if err := parse(); err != nil || *out == "" || *base == "" || fs.NArg() == 0 {
			return errUsage
		}
		return build(stdout, *out, *base, fs.Args())
	case "sign":
		keyEnv := fs.String("key-env", DefaultKeyEnv, "")
		keyFile := fs.String("key-file", "", "")
		if err := parse(); err != nil || fs.NArg() != 1 {
			return errUsage
		}
		return sign(stdout, fs.Arg(0), *keyEnv, *keyFile, getenv)
	case "verify":
		key := fs.String("key", "", "")
		archives := fs.String("archives", "", "")
		if err := parse(); err != nil || fs.NArg() != 1 || *key == "" {
			return errUsage
		}
		return verify(stdout, fs.Arg(0), *key, *archives)
	}
	return errUsage
}

func keygen(stdout io.Writer) error {
	public, secret, err := index.GenerateKey()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "public key: %s\nsecret key: %s\n", public, secret)
	fmt.Fprintf(stdout, "\nThe public key verifies the index: build it into Flint (index.OfficialPublicKey) or set it as extensions.indexPublicKey.\n"+
		"The secret key signs it: store it as the signing secret (%s) and nowhere else. Whoever has it can publish extensions Flint will offer.\n", DefaultKeyEnv)
	return nil
}

func build(stdout io.Writer, out, base string, args []string) error {
	inputs := make([]index.Input, len(args))
	for i, arg := range args {
		inputs[i] = index.ParseInput(arg)
	}
	ix, err := index.Build(index.BuildOptions{Inputs: inputs, OutDir: out, BaseURL: base})
	if err != nil {
		return err
	}
	for _, e := range ix.Extensions {
		platforms := make([]string, 0, len(e.Archives))
		for _, p := range append([]string{index.Any}, index.Platforms...) {
			if _, ok := e.Archives[p]; ok {
				platforms = append(platforms, p)
			}
		}
		fmt.Fprintf(stdout, "%s %s (%s): %s\n", e.ID, e.Version, e.Kind, strings.Join(platforms, ", "))
	}
	fmt.Fprintf(stdout, "wrote %s/%s (not signed yet: run flint-extindex sign)\n", strings.TrimRight(out, "/"), index.FileName)
	return nil
}

func sign(stdout io.Writer, path, keyEnv, keyFile string, getenv func(string) string) error {
	var secret, where string
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return fmt.Errorf("read the signing key: %w", err)
		}
		secret, where = string(data), "the file "+keyFile
	} else {
		secret, where = getenv(keyEnv), "the environment variable "+keyEnv
	}
	if strings.TrimSpace(secret) == "" {
		return fmt.Errorf("no signing key: %s is empty. Nothing was signed; an unsigned index must not be published", where)
	}
	key, err := index.ParseSecretKey(secret)
	if err != nil {
		return fmt.Errorf("%s: %w", where, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Sign only what Flint will accept.
	if _, err := index.Parse(data); err != nil {
		return err
	}
	if err := os.WriteFile(path+index.SignatureSuffix, index.Sign(key, data), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s (public key: %s)\n", path+index.SignatureSuffix, index.PublicOf(key))
	return nil
}

func verify(stdout io.Writer, path, public, archives string) error {
	key, err := index.ParsePublicKey(public)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(path + index.SignatureSuffix)
	if err != nil {
		return fmt.Errorf("no signature: %w", err)
	}
	ix, err := index.Open(data, sig, key)
	if err != nil {
		return err
	}
	if archives != "" {
		if err := index.CheckFiles(ix, archives); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "ok: %s is signed with this key; extensions listed: %d\n", path, len(ix.Extensions))
	return nil
}
