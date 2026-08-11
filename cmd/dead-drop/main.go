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
	case "serve":
		code = cmdServe(os.Args[2:])
	case "put":
		code = cmdPut(os.Args[2:])
	case "get":
		code = cmdGet(os.Args[2:])
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
  dead-drop seal  [flags]   # offline encrypt
  dead-drop open  [flags]   # offline decrypt
  dead-drop put   [flags]   # seal + upload; prints share link with #key
  dead-drop get   URL       # download + decrypt
  dead-drop serve [flags]
  dead-drop version

Examples:
  dead-drop seal -in secret.txt -out secret.seal -key-out secret.key
  dead-drop serve -addr :8080 -data ./data
  dead-drop put -server http://127.0.0.1:8080 -in secret.txt
  dead-drop get 'http://127.0.0.1:8080/s/ID#KEY' -out secret.txt

Flags: run "dead-drop <cmd> -h".
`)
}
