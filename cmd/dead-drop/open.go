package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/donkeyx/dead-drop/blob"
)

func cmdOpen(args []string) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	inPath := fs.String("in", "", "input .seal package (required; - for stdin)")
	outPath := fs.String("out", "", "output plaintext file (required; - for stdout)")
	key := fs.String("key", "", "master key base64url (prefer -key-file)")
	keyFile := fs.String("key-file", "", "file containing master key base64url")
	passEnv := fs.String("passphrase-env", "", "env var holding optional passphrase")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "open: -in and -out are required")
		fs.Usage()
		return exitUsage
	}
	if *key == "" && *keyFile == "" {
		fmt.Fprintln(os.Stderr, "open: provide -key or -key-file")
		return exitUsage
	}
	if *key != "" && *keyFile != "" {
		fmt.Fprintln(os.Stderr, "open: use only one of -key or -key-file")
		return exitUsage
	}

	keyStr := *key
	if *keyFile != "" {
		b, err := os.ReadFile(*keyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open: read key file: %v\n", err)
			return exitUsage
		}
		keyStr = strings.TrimSpace(string(b))
	}
	masterKey, err := blob.DecodeKeyB64URL(keyStr)
	if err != nil || len(masterKey) != blob.MasterKeySize {
		fmt.Fprintln(os.Stderr, "open: invalid master key")
		return exitUsage
	}
	defer zeroSlice(masterKey)

	var pass []byte
	if *passEnv != "" {
		v, ok := os.LookupEnv(*passEnv)
		if !ok {
			fmt.Fprintf(os.Stderr, "open: env %q unset\n", *passEnv)
			return exitUsage
		}
		pass = []byte(v)
		defer zeroSlice(pass)
	}

	pkg, err := readInput(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: read package: %v\n", err)
		return exitUsage
	}

	res, err := blob.Open(pkg, masterKey, pass)
	if err != nil {
		if errors.Is(err, blob.ErrPassphraseNeeded) {
			fmt.Fprintln(os.Stderr, "open: passphrase required (use -passphrase-env)")
			return exitDecrypt
		}
		if errors.Is(err, blob.ErrDecryptFailed) || errors.Is(err, blob.ErrInvalidKey) {
			fmt.Fprintf(os.Stderr, "open: decrypt failed: %v\n", err)
			return exitDecrypt
		}
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		return exitDecrypt
	}

	if err := writeOutput(*outPath, res.Plaintext); err != nil {
		fmt.Fprintf(os.Stderr, "open: write output: %v\n", err)
		return exitUsage
	}
	if *outPath != "-" && res.Filename != "" {
		fmt.Fprintf(os.Stderr, "opened as %s (envelope name %q, type %s)\n", *outPath, res.Filename, res.ContentType)
	}
	return exitOK
}
