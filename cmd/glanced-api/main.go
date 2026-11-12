// Command glanced-api serves the Glanced card sync API.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "serve":
		return errors.New("serve: not implemented")
	case "user":
		return errors.New("user: not implemented")
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}
