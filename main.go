package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// docker/podman run <image> <cmd> <...params>
// go run main.go run <image> <cmd> <...params>

var (
	Info  *log.Logger = log.New(os.Stdout, "[INFO]  ", 0)
	Warn  *log.Logger = log.New(os.Stdout, "[WARN]  ", 0)
	Error *log.Logger = log.New(os.Stderr, "[ERROR]  ", 0)
)

func child() error {
	Info.Printf("Running %s as %d...\n", strings.Join(os.Args[2:], " "), os.Getpid())

	if err := cg(); err != nil {
		return err
	}

	cmd := exec.Command(os.Args[2], os.Args[3:]...)

	syscall.Sethostname([]byte("container"))
	syscall.Chroot("./ubuntu-fs")
	syscall.Chdir("/")
	syscall.Mount("proc", "proc", "proc", 0, "")
	defer syscall.Unmount("/proc", 0)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func run() error {
	Info.Printf("Running %s as %d...\n", strings.Join(os.Args[2:], " "), os.Getpid())

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		Unshareflags: syscall.CLONE_NEWNS,
	}

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func cg() error {
	cgroups := "/sys/fs/cgroup/"
	containers := filepath.Join(cgroups, "containers")
	err := os.Mkdir(containers, 0755)

	if err != nil && !os.IsExist(err) {
		return err
	}

	if err = os.WriteFile(filepath.Join(containers, "pids.max"), []byte("20"), 0700); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(containers, "notify_on_release"), []byte("1"), 0700); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(containers, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0700); err != nil {
		return err
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		Error.Printf("Usage: %s run <image> <cmd> <...params>\n", os.Args[0])
		os.Exit(1)
	}

	var err error = nil
	switch os.Args[1] {
	case "run":
		err = run()
	case "child":
		err = child()
	default:
		Error.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}

	if err != nil {
		Error.Println(err)
		os.Exit(1)
	}
}
