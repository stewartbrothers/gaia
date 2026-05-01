package main

import (
	"fmt"

	"github.com/stewartbrothers/gaia/internal/version"
)

func main() {
	fmt.Printf("gaia %s (commit %s)\n", version.Version, version.Commit)
	fmt.Println("Phase 1 scaffold — see https://github.com/stewartbrothers/gaia/issues/6")
}
