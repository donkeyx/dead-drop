package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/donkeyx/dead-drop/blob"
)

func cmdGet(args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("server", envOr("DEADDROP_SERVER", ""), "server base URL if arg is bare id")
	outPath := fs.String("out", "-", "output file (- for stdout)")
	keyFlag := fs.String("key", "", "master key base64url (if not in URL fragment)")
	keyFile := fs.String("key-file", "", "file with master key")
	passEnv := fs.String("passphrase-env", "", "env var with optional passphrase")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	// Flags must come before the URL (Go flag package stops at first non-flag).
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "get: need exactly one URL or id (flags before the URL: get -out file URL)")
		fs.Usage()
		return exitUsage
	}
	raw := fs.Arg(0)

	id, keyFromURL, baseURL, err := parseShareRef(raw, *server)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get: %v\n", err)
		return exitUsage
	}

	keyStr := *keyFlag
	if *keyFile != "" {
		b, err := os.ReadFile(*keyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "get: key-file: %v\n", err)
			return exitUsage
		}
		keyStr = strings.TrimSpace(string(b))
	}
	if keyStr == "" {
		keyStr = keyFromURL
	}
	if keyStr == "" {
		fmt.Fprintln(os.Stderr, "get: missing key (put it after # in the URL, or use -key / -key-file)")
		return exitUsage
	}
	masterKey, err := blob.DecodeKeyB64URL(keyStr)
	if err != nil || len(masterKey) != blob.MasterKeySize {
		fmt.Fprintln(os.Stderr, "get: invalid master key")
		return exitUsage
	}
	defer zeroSlice(masterKey)

	var pass []byte
	if *passEnv != "" {
		v, ok := os.LookupEnv(*passEnv)
		if !ok {
			fmt.Fprintf(os.Stderr, "get: env %q unset\n", *passEnv)
			return exitUsage
		}
		pass = []byte(v)
		defer zeroSlice(pass)
	}

	apiURL := strings.TrimRight(baseURL, "/") + "/api/v1/secrets/" + url.PathEscape(id)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get: http: %v\n", err)
		return exitUsage
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		fmt.Fprintf(os.Stderr, "get: read body: %v\n", err)
		return exitUsage
	}
	if resp.StatusCode == http.StatusNotFound {
		fmt.Fprintln(os.Stderr, "get: not found (unknown, expired, or already burned)")
		return exitNotFound
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "get: server %s: %s\n", resp.Status, strings.TrimSpace(string(body)))
		return exitUsage
	}

	pkg := body
	// optional future: if Content-Type json...
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		var wrap struct {
			Blob string `json:"blob"`
		}
		if err := json.Unmarshal(body, &wrap); err == nil && wrap.Blob != "" {
			if b, err := blob.DecodeB64URL(wrap.Blob); err == nil {
				pkg = b
			}
		}
	}

	res, err := blob.Open(pkg, masterKey, pass)
	if err != nil {
		if errors.Is(err, blob.ErrPassphraseNeeded) {
			fmt.Fprintln(os.Stderr, "get: passphrase required (-passphrase-env)")
			return exitDecrypt
		}
		fmt.Fprintf(os.Stderr, "get: decrypt failed: %v\n", err)
		return exitDecrypt
	}

	if err := writeOutput(*outPath, res.Plaintext); err != nil {
		fmt.Fprintf(os.Stderr, "get: write: %v\n", err)
		return exitUsage
	}
	if *outPath != "-" {
		fmt.Fprintf(os.Stderr, "wrote %s", *outPath)
		if res.Filename != "" {
			fmt.Fprintf(os.Stderr, " (name %q)", res.Filename)
		}
		fmt.Fprintln(os.Stderr)
	}
	return exitOK
}

// parseShareRef accepts full URL with optional fragment, or bare id (+ -server).
func parseShareRef(raw, serverFlag string) (id, key, base string, err error) {
	// If it looks like a URL
	if strings.Contains(raw, "://") {
		// url.Parse drops fragment into Fragment field
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", "", fmt.Errorf("bad URL: %w", err)
		}
		key = u.Fragment
		// path /s/{id} or /api/v1/secrets/{id}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 0 {
			return "", "", "", fmt.Errorf("URL missing path")
		}
		id = parts[len(parts)-1]
		if id == "" {
			return "", "", "", fmt.Errorf("URL missing id")
		}
		base = u.Scheme + "://" + u.Host
		return id, key, base, nil
	}

	// bare id
	id = raw
	if serverFlag == "" {
		serverFlag = envOr("DEADDROP_SERVER", "http://127.0.0.1:8080")
	}
	u, err := url.Parse(serverFlag)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", "", fmt.Errorf("need valid -server when using bare id")
	}
	return id, "", strings.TrimRight(serverFlag, "/"), nil
}
