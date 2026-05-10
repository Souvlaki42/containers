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

type Container struct {
	host_user   int
	uuid        []byte
	cgroup_root string
	cgroup_path string
	limits      *CGLimit
}

func (lim CGLimit) cpu_limit() string {
	if lim.cpu_quota == 0 {
		return fmt.Sprintf("max %d", lim.cpu_period)
	} else {
		return fmt.Sprintf("%d %d", lim.cpu_quota, lim.cpu_period)
	}
}

func create_container() (*Container, error) {
	container := &Container{}

	var info syscall.Sysinfo_t

	if err := syscall.Sysinfo(&info); err != nil {
		return nil, err
	}

	container.limits = &CGLimit{
		memory:     info.Totalram * uint64(info.Unit),
		proc:       20,
		cpu_period: 100000,
		cpu_quota:  0,
	}

	uuid, err := exec.Command("uuidgen").Output()
	if err != nil {
		return nil, err
	}
	container.uuid = uuid

	uid := os.Getuid()
	container.host_user = uid

	cgroup_root := fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service", uid, uid)
	container.cgroup_root = cgroup_root

	container.cgroup_path = filepath.Join(cgroup_root, "containers", string(uuid))

	return container, nil
}

func print_running_as() {
	Info.Printf("Running %s as user %d in process %d...\n", strings.Join(os.Args[2:], " "), os.Getuid(), os.Getpid())
}

func setup_cgroup(container *Container) error {
	if err := os.WriteFile(filepath.Join(container.cgroup_root, "cgroup.subtree_control"), []byte("+memory +pids +cpu"), 0644); err != nil {
		return err
	}

	err := os.MkdirAll(container.cgroup_path, 0755)

	if err != nil && !os.IsExist(err) {
		return err
	}

	if err = os.WriteFile(filepath.Join(container.cgroup_path, "memory.max"), []byte(strconv.FormatUint(container.limits.memory, 10)), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(container.cgroup_path, "cpu.max"), []byte(container.limits.cpu_limit()), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(container.cgroup_path, "pids.max"), []byte(strconv.Itoa(container.limits.proc)), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(container.cgroup_path, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return err
	}

	return nil
}

func child(container *Container) error {
	print_running_as()

	cmd := exec.Command(os.Args[2], os.Args[3:]...)

	if err := syscall.Sethostname(container.uuid); err != nil {
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

func run(container *Container) error {
	print_running_as()

	if err := setup_cgroup(container); err != nil {
		return err
	}

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUSER | syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		Unshareflags: syscall.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getuid(),
			Size:        1,
		}},
	}

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		Error.Printf("Usage: %s run <image> <cmd> <...params>\n", os.Args[0])
		os.Exit(1)
	}

	container, err := create_container()
	if err != nil {
		Error.Println(err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		err = run(container)
	case "child":
		err = child(container)
	default:
		Error.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}

	if err != nil {
		Error.Println(err)
		os.Exit(1)
	}
}
