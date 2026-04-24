package main

import (
	"fmt"
	"os"

	"remove-feishu-ai/internal/app"
)

func main() {
	if err := app.New().Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
