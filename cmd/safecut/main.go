package main

import (
	"os"

	"github.com/Rafaelhdsg/safecut/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
