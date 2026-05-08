package main

import (
	"fmt"
	"os"
	"strings"
)

// docker/podman run <image> <cmd> <...params>
// go run main.go run <image> <cmd> <...params>

func run() {
	fmt.Printf("Running %s...\n", strings.Join(os.Args[2:], " "))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s run <image> <cmd> <...params>\n", os.Args[0])
		os.Exit(0)
	}

	switch os.Args[1] {
	case "run":
		run()
	default:
		fmt.Println("Uknown command")
		os.Exit(2)
	}
}
