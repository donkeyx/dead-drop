// Command dead-drop is the CLI for offline SEAL packages and (later) server/network ops.
package main

import (
	"fmt"
	"os"
)

// Exit codes (DESIGN.md).
const (
	exitOK       = 0
	exitDecrypt  = 2 // auth / decrypt fail
	exitNotFound = 3
	exitUsage    = 4
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitUsage)
	}
	var code int
	switch os.Args[1] {
	case "seal":
		code = cmdSeal(os.Args[2:])
	case "open":
		code = cmdOpen(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		code = exitOK
	case "version", "-version", "--version":
		fmt.Println("dead-drop dev")
		code = exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		code = exitUsage
	}
	os.Exit(code)
}

func usage() {
	fmt.Fprintf(os.Stderr, `dead-drop — client-side encrypted secret packages (SEAL v1)

Usage:
  dead-drop seal  [flags]
  dead-drop open  [flags]
  dead-drop version

Offline seal/open (no server):
  dead-drop seal -in secret.txt -out secret.seal -key-out secret.key
  dead-drop open -in secret.seal -out secret.txt -key-file secret.key

  # passphrase from env (never pass on argv)
  export DEADDROP_PASS='...'
  dead-drop seal -in f -out f.seal -key-out k -passphrase-env DEADDROP_PASS
  dead-drop open -in f.seal -out f -key-file k -passphrase-env DEADDROP_PASS

Flags: run "dead-drop seal -h" or "dead-drop open -h".
`)
}
