package main

import (
	"fmt"
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

type CGLimit struct {
	memory     uint64 // in bytes
	proc       int
	cpu_period int // in microseconds
	cpu_quota  int
}

func (lim CGLimit) cpu_limit() string {
	if lim.cpu_quota == 0 {
		return fmt.Sprintf("max %d", lim.cpu_period)
	} else {
		return fmt.Sprintf("%d %d", lim.cpu_quota, lim.cpu_period)
	}
}

func print_running_as() {
	Info.Printf("Running %s as user %d in process %d...\n", strings.Join(os.Args[2:], " "), os.Getuid(), os.Getpid())
}

func child() error {
	print_running_as()

	if err := setup_cgroup("/sys/fs/cgroup/containers"); err != nil {
		return err
	}

	cmd := exec.Command(os.Args[2], os.Args[3:]...)

	if err := syscall.Sethostname([]byte("container")); err != nil {
		return err
	}

	if err := syscall.Chroot("./ubuntu-fs"); err != nil {
		return err
	}

	if err := syscall.Chdir("/"); err != nil {
		return err
	}

	if err := syscall.Mount("proc", "proc", "proc", 0, ""); err != nil {
		return err
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}

	if err := syscall.Unmount("/proc", 0); err != nil {
		return err
	}

	return nil
}

func run() error {
	print_running_as()

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	userId := os.Getuid()

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUSER | syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		Unshareflags: syscall.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      userId,
			Size:        1,
		}},
	}

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func setup_cgroup(cgroupPath string) error {
	err := os.MkdirAll(cgroupPath, 0755)

	if err != nil && !os.IsExist(err) {
		return err
	}

	var info syscall.Sysinfo_t

	if err := syscall.Sysinfo(&info); err != nil {
		return err
	}

	cgLimit := CGLimit{
		memory:     info.Totalram * uint64(info.Unit),
		proc:       20,
		cpu_period: 100000,
		cpu_quota:  0,
	}

	if err = os.WriteFile(filepath.Join(cgroupPath, "memory.max"), []byte(strconv.FormatUint(cgLimit.memory, 10)), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(cgroupPath, "cpu.max"), []byte(cgLimit.cpu_limit()), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(cgroupPath, "pids.max"), []byte(strconv.Itoa(cgLimit.proc)), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(cgroupPath, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
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
