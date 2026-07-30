package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// runMail dispatches the mail subcommands.
//
// These exist because suppression is otherwise a one-way door: a bounce adds an
// address, and nothing in the API takes it back off. A mailbox that was full once
// would stay unmailable — including for password resets — with no way out short
// of hand-written SQL.
func runMail(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}

	var err error
	switch args[0] {
	case "suppressions":
		err = cmdSuppressions(args[1:])
	case "unsuppress":
		err = cmdUnsuppress(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown mail action %q\n\n", args[0])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func cmdSuppressions(args []string) error {
	fs := flag.NewFlagSet("mail suppressions", flag.ExitOnError)
	limit := fs.Int("limit", 50, "max rows")
	_ = fs.Parse(args)

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	entries, err := d.mailEvents.ListSuppressions(ctx, int32(*limit)) //nolint:gosec // limit is small operator input
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	// tabwriter buffers; a write error only surfaces on Flush, checked below.
	_, _ = fmt.Fprintln(w, "EMAIL\tREASON\tSINCE\tDETAIL")
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			e.Email, e.Reason, e.CreatedAt.Format(time.DateOnly), e.Detail)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\n%d suppressed address(es)\n", len(entries))
	return nil
}

func cmdUnsuppress(args []string) error {
	fs := flag.NewFlagSet("mail unsuppress", flag.ExitOnError)
	email := fs.String("email", "", "address to make mailable again (required)")
	_ = fs.Parse(args)
	if strings.TrimSpace(*email) == "" {
		return fmt.Errorf("-email is required")
	}

	ctx := context.Background()
	d, cleanup, err := openDeps(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	removed, err := d.mailEvents.Unsuppress(ctx, *email)
	if err != nil {
		return err
	}
	if !removed {
		fmt.Printf("%s was not suppressed; nothing to do\n", *email)
		return nil
	}

	fmt.Printf("%s is mailable again\n", *email)
	return nil
}
