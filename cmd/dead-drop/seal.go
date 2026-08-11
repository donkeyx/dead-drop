package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/donkeyx/dead-drop/blob"
)

func cmdSeal(args []string) int {
	fs := flag.NewFlagSet("seal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	inPath := fs.String("in", "", "input file (required; - for stdin)")
	outPath := fs.String("out", "", "output .seal package (required; - for stdout)")
	keyOut := fs.String("key-out", "", "write master key (base64url) to this file (required unless -key-stdout)")
	keyStdout := fs.Bool("key-stdout", false, "print master key base64url on stdout (only if -out is not stdout)")
	passEnv := fs.String("passphrase-env", "", "env var holding optional passphrase (never use argv)")
	strong := fs.Bool("strong", false, "use stronger Argon2id params when passphrase is set (CLI profile)")
	name := fs.String("name", "", "filename stored inside envelope (default: basename of -in, empty for stdin)")
	ct := fs.String("ct", "", "content-type (default: application/octet-stream or text/plain for empty name)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *inPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "seal: -in and -out are required")
		fs.Usage()
		return exitUsage
	}
	if *keyOut == "" && !*keyStdout {
		fmt.Fprintln(os.Stderr, "seal: provide -key-out FILE or -key-stdout")
		return exitUsage
	}
	if *keyStdout && *outPath == "-" {
		fmt.Fprintln(os.Stderr, "seal: -key-stdout cannot be used with -out - (both need stdout)")
		return exitUsage
	}

	plain, err := readInput(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seal: read input: %v\n", err)
		return exitUsage
	}

	filename := *name
	if filename == "" && *inPath != "-" {
		filename = filepath.Base(*inPath)
	}
	contentType := *ct
	if contentType == "" {
		if filename == "" {
			contentType = "text/plain; charset=utf-8"
		} else {
			contentType = "application/octet-stream"
		}
	}

	var pass []byte
	if *passEnv != "" {
		v, ok := os.LookupEnv(*passEnv)
		if !ok || v == "" {
			fmt.Fprintf(os.Stderr, "seal: env %q empty or unset\n", *passEnv)
			return exitUsage
		}
		pass = []byte(v)
		defer zeroSlice(pass)
	}

	opt := blob.SealOptions{
		Passphrase:  pass,
		Filename:    filename,
		ContentType: contentType,
	}
	if *strong && len(pass) > 0 {
		// cli-strong-ish defaults from DESIGN (time=3, mem=65536, para=4)
		opt.ArgonTime = 3
		opt.ArgonMem = 65536
		opt.ArgonPara = 4
	}

	masterKey, err := blob.GenerateMasterKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "seal: generate key: %v\n", err)
		return exitUsage
	}
	defer zeroSlice(masterKey)

	pkg, err := blob.Seal(plain, masterKey, opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seal: encrypt: %v\n", err)
		return exitUsage
	}

	if err := writeOutput(*outPath, pkg); err != nil {
		fmt.Fprintf(os.Stderr, "seal: write package: %v\n", err)
		return exitUsage
	}

	keyStr := blob.EncodeKeyB64URL(masterKey)
	if *keyOut != "" {
		// 0600 key file
		if err := os.WriteFile(*keyOut, []byte(keyStr+"\n"), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "seal: write key: %v\n", err)
			return exitUsage
		}
	}
	if *keyStdout {
		fmt.Println(keyStr)
	} else if *keyOut != "" {
		fmt.Fprintf(os.Stderr, "wrote package %s\nwrote key %s\n", displayPath(*outPath), *keyOut)
		fmt.Fprintf(os.Stderr, "keep the key offline; the package alone cannot be opened.\n")
	}
	return exitOK
}

func displayPath(p string) string {
	if p == "-" {
		return "stdout"
	}
	return p
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		return os.ReadFile("/dev/stdin")
	}
	return os.ReadFile(path)
}

func writeOutput(path string, data []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func zeroSlice(b []byte) {
	for i := range b {
		b[i] = 0
	}
	// keep reference alive
	if len(b) > 0 {
		_ = b[0]
	}
}
