package main

import (
	"context"
	"fmt"
	"os"

	"github.com/myuon/ui-shot/cmd"
)

func main() {
	if err := cmd.NewRootCmd().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
