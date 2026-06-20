package main

import (
	"fmt"
	"os"
)

const version = "0.0.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Println(version)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "repocache %s\nno commands implemented yet\n", version)
	os.Exit(1)
}
