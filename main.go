package main

import (
	"encoding/json"
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
	Memory    uint64 // in bytes
	Proc      int
	CpuPeriod int // in microseconds
	CpuQuota  int
}

type Container struct {
	HostUser    int
	Uuid        string
	CgroupRoot  string
	CgroupStore string
	CgroupPath  string
	Limits      *CGLimit
}

func (lim CGLimit) cpu_limit() string {
	if lim.CpuQuota == 0 {
		return fmt.Sprintf("max %d", lim.CpuPeriod)
	} else {
		return fmt.Sprintf("%d %d", lim.CpuQuota, lim.CpuPeriod)
	}
}

func create_container() (*Container, error) {
	container := &Container{}

	var info syscall.Sysinfo_t

	if err := syscall.Sysinfo(&info); err != nil {
		return nil, err
	}

	container.Limits = &CGLimit{
		Memory:    info.Totalram * uint64(info.Unit),
		Proc:      20,
		CpuPeriod: 100000,
		CpuQuota:  0,
	}

	uuid, err := exec.Command("uuidgen").Output()
	if err != nil {
		return nil, err
	}
	container.Uuid = strings.TrimSpace(string(uuid))

	uid := os.Getuid()
	container.HostUser = uid

	cgroup_root := fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service", uid, uid)
	container.CgroupRoot = cgroup_root

	container.CgroupStore = filepath.Join(cgroup_root, "containers")

	container.CgroupPath = filepath.Join(container.CgroupStore, container.Uuid)

	return container, nil
}

func print_running_as() {
	Info.Printf("Running %s as user %d on group %d in process %d...\n", strings.Join(os.Args[2:], " "), os.Getuid(), os.Getgid(), os.Getpid())
}

func setup_cgroup(container *Container) error {
	err := os.MkdirAll(container.CgroupRoot, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	init_path := filepath.Join(container.CgroupRoot, "init.scope")

	err = os.MkdirAll(init_path, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	if err = os.WriteFile(filepath.Join(init_path, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(container.CgroupRoot, "cgroup.subtree_control"), []byte("+memory +pids +cpu"), 0644); err != nil {
		return err
	}

	err = os.MkdirAll(container.CgroupStore, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	if err := os.WriteFile(filepath.Join(container.CgroupStore, "cgroup.subtree_control"), []byte("+memory +pids +cpu"), 0644); err != nil {
		return err
	}

	err = os.MkdirAll(container.CgroupPath, 0755)

	if err != nil && !os.IsExist(err) {
		return err
	}

	if err = os.WriteFile(filepath.Join(container.CgroupPath, "memory.max"), []byte(strconv.FormatUint(container.Limits.Memory, 10)), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(container.CgroupPath, "cpu.max"), []byte(container.Limits.cpu_limit()), 0644); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(container.CgroupPath, "pids.max"), []byte(strconv.Itoa(container.Limits.Proc)), 0644); err != nil {
		return err
	}

	return nil
}

func child() error {
	print_running_as()

	readPipe := os.NewFile(3, "read-pipe")
	Info.Println(readPipe)

	var container Container
	json.NewDecoder(readPipe).Decode(&container)

	cmd := exec.Command(os.Args[2], os.Args[3:]...)

	if err := os.WriteFile(filepath.Join(container.CgroupPath, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return err
	}

	if err := syscall.Sethostname([]byte(container.Uuid)); err != nil {
		return err
	}

	if err := syscall.Chroot("./root-fs"); err != nil {
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

func parent() error {
	print_running_as()

	childReader, parentWriter, err := os.Pipe()
	if err != nil {
		return err
	}

	container, err := create_container()
	if err != nil {
		return err
	}

	defer func() {
		// FIX: replace with proper inotify polling and cleanup
		// Original used cgroup v1's `notify-on-release`
		if err = os.Remove(container.CgroupPath); err != nil {
			Error.Println(err)
			os.Exit(1)
		}

		if err = os.Remove(container.CgroupStore); err != nil {
			Error.Println(err)
			os.Exit(1)
		}

		if err != nil {
			Error.Println(err)
			os.Exit(1)
		}
	}()

	if err := setup_cgroup(container); err != nil {
		return err
	}

	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.ExtraFiles = []*os.File{childReader}

	if err := json.NewEncoder(parentWriter).Encode(container); err != nil {
		return err
	}

	if err := parentWriter.Close(); err != nil {
		return err
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   syscall.CLONE_NEWUSER | syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
		Unshareflags: syscall.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      container.HostUser,
			Size:        1,
		}},
		GidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getgid(),
			Size:        1,
		}},
	}

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func print_help() {
	Error.Printf("Usage: %s run <image> <cmd> <...params>\n", os.Args[0])
	Error.Println("Note: Please ignore subcommand `child`. It is intended for internal use only.")
}

func main() {
	if len(os.Args) < 2 {
		print_help()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "--help":
		print_help()
		os.Exit(1)
	case "run":
		if err := parent(); err != nil {
			Error.Println(err)
			os.Exit(1)
		}
	case "child":
		if err := child(); err != nil {
			Error.Println(err)
			os.Exit(1)
		}
	default:
		Error.Printf("Unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}

}
