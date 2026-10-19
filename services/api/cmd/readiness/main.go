package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"claimbridge/services/api/internal/readiness"
)

func main() {
	profileFlag := flag.String("profile", "core", "Readiness evaluation profile ('core' for local dev, 'submission' or 'demo' for live gates)")
	envFileFlag := flag.String("env-file", "", "Explicit path to .env file")
	batchIDFlag := flag.String("batch-id", "", "Optional PayPal Payout Batch ID for read-only lookup probe")
	itemIDFlag := flag.String("item-id", "", "Optional PayPal Payout Item ID for read-only lookup probe")
	flag.Parse()

	if *batchIDFlag != "" && *itemIDFlag != "" {
		fmt.Fprintf(os.Stderr, "Error: ambiguous PayPal lookup invocation: both -batch-id (%s) and -item-id (%s) provided; specify only one\n", *batchIDFlag, *itemIDFlag)
		os.Exit(1)
	}

	opts := readiness.RunnerOptions{
		Profile: *profileFlag,
		EnvFile: *envFileFlag,
		BatchID: *batchIDFlag,
		ItemID:  *itemIDFlag,
	}

	summary, err := readiness.Run(context.Background(), opts, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Readiness execution fatal error: %v\n", err)
		os.Exit(1)
	}

	os.Exit(summary.ExitCode)
}
