package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donkeyx/dead-drop/blob"
)

func cmdPut(args []string) int {
	fs := flag.NewFlagSet("put", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("server", envOr("DEADDROP_SERVER", "http://127.0.0.1:8080"), "server base URL")
	inPath := fs.String("in", "", "input file (- for stdin; required unless -file)")
	filePath := fs.String("file", "", "alias for -in")
	passEnv := fs.String("passphrase-env", "", "env var with optional passphrase")
	ttl := fs.String("ttl", "24h", "TTL as Go duration (e.g. 24h, 90m)")
	burn := fs.Bool("burn", true, "burn after first successful download")
	strong := fs.Bool("strong", false, "stronger Argon2 when passphrase set")
	name := fs.String("name", "", "envelope filename (default basename of input)")
	ct := fs.String("ct", "", "content-type")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	path := *inPath
	if path == "" {
		path = *filePath
	}
	if path == "" {
		// allow positional stdin via leftover
		if fs.NArg() == 1 && fs.Arg(0) == "-" {
			path = "-"
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "put: -in FILE is required (use - for stdin)")
		fs.Usage()
		return exitUsage
	}

	plain, err := readInput(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "put: read: %v\n", err)
		return exitUsage
	}

	filename := *name
	if filename == "" && path != "-" {
		filename = filepath.Base(path)
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
			fmt.Fprintf(os.Stderr, "put: env %q empty or unset\n", *passEnv)
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
		opt.ArgonTime = 3
		opt.ArgonMem = 65536
		opt.ArgonPara = 4
	}

	masterKey, err := blob.GenerateMasterKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "put: key: %v\n", err)
		return exitUsage
	}
	defer zeroSlice(masterKey)

	pkg, err := blob.Seal(plain, masterKey, opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "put: seal: %v\n", err)
		return exitUsage
	}

	base, err := url.Parse(strings.TrimRight(*server, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		fmt.Fprintf(os.Stderr, "put: invalid -server URL\n")
		return exitUsage
	}

	apiURL := base.String() + "/api/v1/secrets"
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(pkg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "put: request: %v\n", err)
		return exitUsage
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Seal-TTL", *ttl)
	if *burn {
		req.Header.Set("X-Seal-Burn", "1")
	} else {
		req.Header.Set("X-Seal-Burn", "0")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "put: http: %v\n", err)
		return exitUsage
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated {
		fmt.Fprintf(os.Stderr, "put: server %s: %s\n", resp.Status, strings.TrimSpace(string(body)))
		if resp.StatusCode == http.StatusForbidden && strings.Contains(string(body), "human_check") {
			fmt.Fprintf(os.Stderr, "put: this host requires a browser human check; use the web UI or a self-hosted server\n")
		}
		if resp.StatusCode == http.StatusNotFound {
			return exitNotFound
		}
		return exitUsage
	}

	var cr struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(body, &cr); err != nil || cr.ID == "" {
		fmt.Fprintf(os.Stderr, "put: bad response: %s\n", body)
		return exitUsage
	}

	// Full share link: origin + /s/{id}#key  (path from server is /s/id)
	sharePath := cr.Path
	if sharePath == "" {
		sharePath = "/s/" + cr.ID
	}
	link := base.String() + sharePath + "#" + blob.EncodeKeyB64URL(masterKey)
	fmt.Println(link)
	fmt.Fprintf(os.Stderr, "created id=%s burn=%v ttl=%s\n", cr.ID, *burn, *ttl)
	fmt.Fprintf(os.Stderr, "anyone with the full link (including #…) can decrypt; burn-after-read=%v\n", *burn)
	return exitOK
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
