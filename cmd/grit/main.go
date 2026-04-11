package main

import (
	"context"
	"os"

	"github.com/kaeawc/grit/internal/cli"
)

func main() {
	code := cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}
