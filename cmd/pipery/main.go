package main

import (
	"fmt"
	"os"

	"github.com/pipery-dev/pipery/internal/pipery"
)

// main is intentionally very small.
//
// The usual pattern in CLI programs is:
// 1. Build an "app" object that contains the real logic.
// 2. Ask that object to run with the command-line arguments.
// 3. Print any human-readable error.
// 4. Exit with the exit code returned by the app.
//
// Keeping main tiny makes the code easier to test because the important logic
// lives in regular Go functions instead of being buried inside main().
func main() {
	app := pipery.NewApp(os.Stdin, os.Stdout, os.Stderr)

	exitCode, err := app.Run(os.Args[1:])
	if err != nil {
		// Errors are printed to stderr so stdout stays clean for command output.
		fmt.Fprintln(os.Stderr, err)
	}

	// The exit code is part of the CLI contract. Returning the child command's
	// exit code makes psh behave like a command mediator instead of hiding
	// failures behind its own success/failure.
	os.Exit(exitCode)
}
