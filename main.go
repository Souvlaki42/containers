package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// docker/podman run <image> <cmd> <...params>
// go run main.go run <image> <cmd> <...params>

func child() {
	fmt.Printf("Running %s...\n", strings.Join(os.Args[2:], " "))

	cmd := exec.Command(os.Args[2], os.Args[3:]...)

	syscall.Sethostname([]byte("container"))

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() {
	fmt.Printf("Running %s...\n", strings.Join(os.Args[2:], " "))

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS,
	}

	err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s run <image> <cmd> <...params>\n", os.Args[0])
		os.Exit(0)
	}

	switch os.Args[1] {
	case "run":
		run()
	case "child":
		child()
	default:
		fmt.Println("Uknown command")
		os.Exit(2)
	}
}
