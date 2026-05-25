package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	Memory    uint64 `json:"memory"` // in bytes
	ProcsNum  int    `json:"procs_num"`
	CpuPeriod int    `json:"cpu_period"` // in microseconds
	CpuQuota  int    `json:"cpu_quota"`
}

type Paths struct {
	CgroupRoot    string `json:"cgroup_root"`
	CgroupPath    string `json:"cgroup_path"`
	ContainerRoot string `json:"container_root"`
	ContainerPath string `json:"container_path"`
}

type Container struct {
	HostUser int      `json:"host_user"`
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Init     string   `json:"init"`
	Args     []string `json:"args"`
	Limits   *CGLimit `json:"limits"`
	Paths    *Paths   `json:"paths"`
}

func (lim CGLimit) cpu_limit() string {
	if lim.CpuQuota == 0 {
		return fmt.Sprintf("max %d", lim.CpuPeriod)
	} else {
		return fmt.Sprintf("%d %d", lim.CpuQuota, lim.CpuPeriod)
	}
}

func create_container() (*Container, error) {
	var info syscall.Sysinfo_t

	if err := syscall.Sysinfo(&info); err != nil {
		return nil, fmt.Errorf("System info retrieval failed: %w", err)
	}

	container := Container{}

	container.Limits = &CGLimit{
		Memory:    info.Totalram * uint64(info.Unit),
		ProcsNum:  20,
		CpuPeriod: 100000,
		CpuQuota:  0,
	}

	name := flag.String("n", "", "Specifies the name of a container. UUID v4 is used as a fallback.")
	image := flag.String("i", "ubuntu", "Specifies the image the container will run upon. Ubuntu is used as a fallback.")
	init := flag.String("c", "/bin/bash", "Specifies the executable the container will start with. /bin/bash is used as a fallback.")

	flag.Parse()

	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		return nil, fmt.Errorf("Flag parsing failed: %w", err)
	}

	if *name == "" {
		uuid, err := exec.Command("uuidgen").Output()
		if err != nil {
			return nil, fmt.Errorf("ID generation failed: %w", err)
		}
		*name = string(uuid)
	}
	container.Name = strings.TrimSpace(*name)

	container.Image = *image

	container.Init = *init

	container.Args = flag.Args()

	uid := os.Getuid()
	container.HostUser = uid

	var paths Paths = Paths{}

	paths.CgroupRoot = fmt.Sprintf("/sys/fs/cgroup/user.slice/user-%d.slice/user@%d.service", uid, uid)

	paths.CgroupPath = filepath.Join(paths.CgroupRoot, "containers", container.Name)

	paths.ContainerRoot = filepath.Join(os.Getenv("HOME"), ".containers")

	paths.ContainerPath = filepath.Join(paths.ContainerRoot, "/store/", container.Name)

	container.Paths = &paths

	return &container, nil
}

func print_running_as() {
	Info.Printf("Running %s as user %d on group %d in process %d...\n", strings.Join(os.Args[1:], " "), os.Getuid(), os.Getgid(), os.Getpid())
}

func setup_image(container *Container) error {
	image_path := filepath.Join(container.Paths.ContainerRoot, "/images/", container.Image)

	err := os.MkdirAll(image_path, 0o755)
	image_exists := errors.Is(err, fs.ErrExist)

	if err != nil && !image_exists {
		return fmt.Errorf("Failed to create image path: %w", err)
	}

	if image_exists || strings.Contains(container.Image, ":latest") {
		if err := os.RemoveAll(image_path); err != nil {
			return fmt.Errorf("Failed to remove image path: %w", err)
		}
		if err := os.MkdirAll(image_path, 0o755); err != nil {
			return nil
		}

		image_exists = false
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("Failed to created image pipe: %w", err)
	}

	if !image_exists {
		crane := exec.Command("crane", "export", container.Image)
		tar := exec.Command("tar", "-xC", image_path)

		crane.Stdout = writer
		tar.Stdin = reader

		if err = crane.Start(); err != nil {
			return fmt.Errorf("Failed to start crane: %w", err)
		}

		if err = tar.Start(); err != nil {
			return fmt.Errorf("Failed to start tar: %w", err)
		}

		if err = crane.Wait(); err != nil {
			return fmt.Errorf("Failed to wait for crane: %w", err)
		}

		if err = writer.Close(); err != nil {
			return fmt.Errorf("Failed to close the image writer: %w", err)
		}

		if err = tar.Wait(); err != nil {
			return fmt.Errorf("Failed to wait for tar: %w", err)
		}
	}

	err = os.CopyFS(container.Paths.ContainerPath, os.DirFS(image_path))
	if err != nil {
		return fmt.Errorf("Failed to copy the filesystem: %w", err)
	}

	return nil
}

func setup_cgroup(container *Container) error {
	err := os.MkdirAll(container.Paths.CgroupRoot, 0o755)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("Failed to create cgroup root: %w", err)
	}

	init_path := filepath.Join(container.Paths.CgroupRoot, "init.scope")

	err = os.MkdirAll(init_path, 0o755)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("Failed to create init path: %w", err)
	}

	if err = os.WriteFile(filepath.Join(init_path, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("Failed to write init procs: %w", err)
	}

	if err := os.WriteFile(filepath.Join(container.Paths.CgroupRoot, "cgroup.subtree_control"), []byte("+memory +pids +cpu"), 0o644); err != nil {
		return fmt.Errorf("Failed to write root subtree control: %w", err)
	}

	cgroup_store := filepath.Join(container.Paths.CgroupRoot, "containers")

	err = os.MkdirAll(cgroup_store, 0o755)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("Failed to create cgroup store: %w", err)
	}

	if err := os.WriteFile(filepath.Join(cgroup_store, "cgroup.subtree_control"), []byte("+memory +pids +cpu"), 0o644); err != nil {
		return fmt.Errorf("Failed to write store subtree control: %w", err)
	}

	err = os.MkdirAll(container.Paths.CgroupPath, 0o755)

	if err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("Failed to create cgroup path: %w", err)
	}

	if err = os.WriteFile(filepath.Join(container.Paths.CgroupPath, "memory.max"), []byte(strconv.FormatUint(container.Limits.Memory, 10)), 0o644); err != nil {
		return fmt.Errorf("Failed to write memory limits: %w", err)
	}

	if err = os.WriteFile(filepath.Join(container.Paths.CgroupPath, "cpu.max"), []byte(container.Limits.cpu_limit()), 0o644); err != nil {
		return fmt.Errorf("Failed to write cpu limits: %w", err)
	}

	if err = os.WriteFile(filepath.Join(container.Paths.CgroupPath, "pids.max"), []byte(strconv.Itoa(container.Limits.ProcsNum)), 0o644); err != nil {
		return fmt.Errorf("Failed to write process limits: %w", err)
	}

	return nil
}

func child() error {
	print_running_as()

	readPipe := os.NewFile(3, "read-pipe")

	var jsonContainer []byte

	if err := json.NewDecoder(readPipe).Decode(&jsonContainer); err != nil {
		return fmt.Errorf("Failed to decode context: %w", err)
	}

	var container Container
	if err := json.Unmarshal(jsonContainer, &container); err != nil {
		return fmt.Errorf("Failed to parse context: %w", err)
	}

	cmd := exec.Command(container.Init, container.Args...)

	if err := os.WriteFile(filepath.Join(container.Paths.CgroupPath, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("Failed to cgroup procs: %w", err)
	}

	if err := syscall.Sethostname([]byte(container.Name)); err != nil {
		return fmt.Errorf("Failed to set hostname: %w", err)
	}

	if err := syscall.Mount(container.Paths.ContainerPath, container.Paths.ContainerPath, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("Failed to mount root: %w", err)
	}

	oldrootfs := filepath.Join(container.Paths.ContainerPath, "oldrootfs")

	if err := os.MkdirAll(oldrootfs, 0o700); err != nil {
		return fmt.Errorf("Failed to make old root: %w", err)
	}

	if err := syscall.PivotRoot(container.Paths.ContainerPath, oldrootfs); err != nil {
		return fmt.Errorf("Failed to pivot root: %w", err)
	}

	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("Failed to change dir: %w", err)
	}

	if err := syscall.Mount("proc", "proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("Failed to mount proc: %w", err)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Failed to run init: %w", err)
	}

	if err := syscall.Unmount("/proc", 0); err != nil {
		return fmt.Errorf("Failed to unmount proc: %w", err)
	}

	return nil
}

func parent() error {
	print_running_as()

	container, err := create_container()
	if err != nil {
		return fmt.Errorf("Failed to create context: %w", err)
	}

	childReader, parentWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("Failed to create ipc pipe: %w", err)
	}

	defer func() {
		// FIX: replace with proper inotify polling and cleanup
		// Original used cgroup v1's `notify-on-release`
		if err := os.RemoveAll(container.Paths.CgroupPath); err != nil {
			Error.Fatalln(fmt.Errorf("Failed to cleanup cgroup: %w", err))
		}

		if err := os.RemoveAll(container.Paths.ContainerPath); err != nil {
			Error.Fatalln(fmt.Errorf("Failed to cleanup container: %w", err))
		}
	}()

	if err := setup_cgroup(container); err != nil {
		return fmt.Errorf("Failed to setup cgroup: %w", err)
	}

	if err := setup_image(container); err != nil {
		return fmt.Errorf("Failed to setup image: %w", err)
	}

	cmd := exec.Command("/proc/self/exe")
	cmd.Args = slices.Concat([]string{"__CONTAINERS_INIT__"}, os.Args[1:])

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.ExtraFiles = []*os.File{childReader}

	jsonContainer, err := json.Marshal(container)
	if err != nil {
		return fmt.Errorf("Failed to stringify context: %w", err)
	}

	if err := json.NewEncoder(parentWriter).Encode(jsonContainer); err != nil {
		return fmt.Errorf("Failed to encode context: %w", err)
	}

	if err := parentWriter.Close(); err != nil {
		return fmt.Errorf("Failed to close parent writer: %w", err)
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
		return fmt.Errorf("Failed to run child: %w", err)
	}

	return nil
}

func main() {
	var process func() error
	if os.Args[0] == "__CONTAINERS_INIT__" {
		process = child
	} else {
		process = parent
	}

	if err := process(); err != nil {
		Error.Fatalln(err)
	}
}
