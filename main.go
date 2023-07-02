package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	customLog "log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	logging "github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type Container struct {
	ID      string
	Command string
	Args    []string
	EnvVars []EnvironmentVariable
	Volumes []Volume
}

type EnvironmentVariable struct {
	Name  string
	Value string
}

type Volume struct {
	HostPath      string
	ContainerPath string
}

var (
	help            bool
	createNetwork   bool
	attachLogs      bool
	detachLogs      bool
	attachContainer bool
	detachContainer bool
	containerID     string
	bridgeName      string
	logsDir         string
	logs            string
	listContainers  bool
	run             bool
)

func main() {
	flag.BoolVar(&help, "help", false, "Display help information")
	flag.BoolVar(&createNetwork, "create-network", false, "Create a new network for the container")
	flag.BoolVar(&attachLogs, "attach-logs", false, "Attach logs from the container")
	flag.BoolVar(&detachLogs, "detach-logs", false, "Detach logs from the container")
	flag.BoolVar(&attachContainer, "attach-container", false, "Attach to the running container")
	flag.BoolVar(&detachContainer, "detach-container", false, "Detach from the running container")
	flag.StringVar(&containerID, "container-id", "", "ID of the container")
	flag.StringVar(&bridgeName, "bridge", "br0", "Name of the network bridge")
	flag.StringVar(&logsDir, "logs-dir", "/var/logs", "Directory to store container logs")
	flag.StringVar(&logs, "logs", "", "Show logs of the container")
	flag.BoolVar(&listContainers, "list-containers", false, "List all containers")
	flag.BoolVar(&run, "run", false, "run container")

	// Additional flags for the 'run' command
	runCommand := flag.NewFlagSet("run", flag.ExitOnError)
	imageName := runCommand.String("image", "", "Container image to use")
	containerName := runCommand.String("name", "", "Name of the container")
	//portMapping := runCommand.String("port", "", "Port mapping in the format hostPort:containerPort")

	flag.Parse()

	if help {
		displayHelp()
		return
	}

	// You need to define the namespace you're using.
	createNamespace("my-namespace")
	ctx := namespaces.WithNamespace(context.Background(), "my-namespace")

	// Connecting to containerd
	client, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		customLog.Fatalf("Failed to connect to containerd: %v", err)
	}
	defer client.Close()

	if listContainers {
		err := listAllContainers(ctx, client)
		if err != nil {
			customLog.Fatalf("Failed to list containers: %s", err)
		}
		return
	}

	if run || containerID == "" {
		customLog.Fatal("Container ID is required")
	}

	if createNetwork {
		setupNetwork()
	}

	if attachLogs {
		err := attachContainerLogs(containerID)
		if err != nil {
			customLog.Fatalf("Failed to attach logs for container %s: %s", containerID, err)
		}
	}

	if detachLogs {
		err := detachContainerLogs(containerID)
		if err != nil {
			customLog.Fatalf("Failed to detach logs for container %s: %s", containerID, err)
		}
	}

	if attachContainer {
		err := attachToContainer(containerID)
		if err != nil {
			customLog.Fatalf("Failed to attach to container %s: %s", containerID, err)
		}
	}

	if detachContainer {
		err := detachFromContainer(containerID)
		if err != nil {
			customLog.Fatalf("Failed to detach from container %s: %s", containerID, err)
		}
	}

	if logs != "" {
		err := displayContainerLogs(containerID)
		if err != nil {
			customLog.Fatalf("Failed to get log from container %s: %s", containerID, err)
		}
	}

	// Check if the command is 'run' and process its specific flags
	if len(flag.Args()) > 0 && flag.Args()[0] == "run" {
		runCommand.Parse(flag.Args()[1:])
		// Run the 'run' command
		if runCommand.Parsed() {
			if *imageName == "" {
				customLog.Fatal("Image name is required")
			}
			if *containerName == "" {
				customLog.Fatal("Container name is required")
			}
			// if *portMapping == "" {
			// 	customLog.Fatal("Port mapping is required")
			// }

			err = runContainer(client, ctx, *imageName, *containerName)
			if err != nil {
				customLog.Fatalf("Failed to run container: %s", err)
			}

		}
	}
}

func displayHelp() {
	fmt.Println(`Gophernetes is a minimal, educational container runtime written in Go.

	Usage:
	gophernetes [options] --container-id <container-id>

	Options:`)
	flag.PrintDefaults()
}

func setupNetwork() {
	// Create a new network namespace
	newNS, _ := netns.New()

	// Switch to the new network namespace
	netns.Set(newNS)

	// Create veth pair
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: "veth1",
		},
		PeerName: "veth2",
	}

	// Add veth to default network namespace
	_ = netlink.LinkAdd(veth)

	// Fetch the peer link
	peer, _ := netlink.LinkByName(veth.PeerName)

	// Move the peer to the new network namespace
	_ = netlink.LinkSetNsFd(peer, int(newNS))

	// Configure the interfaces, assign IP addresses, set up routes, etc...

	// Get a handle on the interface
	veth1, _ := netlink.LinkByName("veth1")

	// Bring it up
	_ = netlink.LinkSetUp(veth1)

	// Assign IP address
	addr, _ := netlink.ParseAddr("192.168.1.2/24")
	_ = netlink.AddrAdd(veth1, addr)

	// Set up the default route
	gw := net.ParseIP("192.168.1.1")
	route := netlink.Route{
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
		LinkIndex: veth1.Attrs().Index,
		Gw:        gw,
	}
	_ = netlink.RouteAdd(&route)

	// Switch back to the original network namespace
	netns.Set(netns.None())
}

func limitMemory() {
	cgroupPath := "/sys/fs/cgroup/memory/mydocker"
	os.MkdirAll(cgroupPath, 0755)
	ioutil.WriteFile(filepath.Join(cgroupPath, "memory.limit_in_bytes"), []byte("999424"), 0700)
	ioutil.WriteFile(filepath.Join(cgroupPath, "notify_on_release"), []byte("1"), 0700)
	ioutil.WriteFile(filepath.Join(cgroupPath, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0700)
}

func attachContainerLogs(containerID string) error {
	logFileName := fmt.Sprintf("%s/%s.log", logsDir, containerID)

	logFile, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	// Redirect stdout and stderr to the log file
	syscall.Dup2(int(logFile.Fd()), int(os.Stdout.Fd()))
	syscall.Dup2(int(logFile.Fd()), int(os.Stderr.Fd()))

	return nil
}

func detachContainerLogs(containerID string) error {
	logFileName := fmt.Sprintf("%s/%s.log", logsDir, containerID)

	err := syscall.Unlink(logFileName)
	if err != nil {
		return err
	}

	return nil
}

func attachToContainer(containerID string) error {
	// Implement attaching to the running container
	// Example: Attach to the container by sending signals
	pid, err := strconv.Atoi(containerID)
	if err != nil {
		return err
	}

	err = syscall.Kill(pid, syscall.SIGUSR1)
	if err != nil {
		return err
	}

	return nil
}

func detachFromContainer(containerID string) error {
	// Implement detaching from the running container
	// Example: Detach from the container by sending signals
	pid, err := strconv.Atoi(containerID)
	if err != nil {
		return err
	}

	err = syscall.Kill(pid, syscall.SIGUSR2)
	if err != nil {
		return err
	}

	return nil
}

func fetchContainerDetails(containerID string) (*Container, error) {
	// Simulate fetching container details from a data source
	// such as a database or API call

	// Example container details
	container := &Container{
		ID:      containerID,
		Command: "echo",
		Args:    []string{"Hello, World!"},
		EnvVars: []EnvironmentVariable{
			{Name: "ENV_VAR_1", Value: "Value 1"},
			{Name: "ENV_VAR_2", Value: "Value 2"},
		},
		Volumes: []Volume{
			{HostPath: "/path/on/host", ContainerPath: "/path/in/container"},
		},
	}

	// Check if the container ID is valid or not
	if containerID != "valid_container_id" {
		return nil, errors.New("container not found")
	}

	return container, nil
}

func createVolumeMount(volume Volume) error {
	// Implement the logic to create a volume mount based on the provided volume details
	// Here, you can perform any necessary operations, such as creating directories or verifying paths

	// Check if the host path exists
	if _, err := os.Stat(volume.HostPath); os.IsNotExist(err) {
		return fmt.Errorf("host path does not exist: %s", volume.HostPath)
	}

	// add more validation or customization based on your specific requirements

	return nil
}

func runContainer(client *containerd.Client, ctx context.Context, imageName string, containerName string) error {
	// Pull the image
	image, err := client.Pull(ctx, imageName, containerd.WithPullUnpack)
	if err != nil {
		return fmt.Errorf("error pulling image: %v", err)
	}

	// Create the container
	container, err := client.NewContainer(
		ctx,
		containerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(containerName+"-snapshot", image),
		containerd.WithNewSpec(oci.WithImageConfig(image)),
	)
	if err != nil {
		return fmt.Errorf("error creating container: %v", err)
	}

	// Create a task from the container
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		return fmt.Errorf("error creating task: %v", err)
	}

	// Start the task
	if err := task.Start(ctx); err != nil {
		return fmt.Errorf("error starting task: %v", err)
	}

	fmt.Println("Container started successfully")

	return nil
}

func displayContainerLogs(containerID string) error {
	// Connect to the container runtime
	ctx := namespaces.WithNamespace(context.Background(), "my-namespace")

	client, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		logging.G(ctx).WithError(err).Fatal("Failed to connect to container runtime")
		return err
	}
	defer client.Close()

	// Get the container
	container, err := client.LoadContainer(ctx, containerID)
	if err != nil {
		logging.G(ctx).WithError(err).Fatalf("Failed to load container %s", containerID)
		return err
	}

	// Get the container's runtime information
	_, err = container.Info(ctx)
	if err != nil {
		logging.G(ctx).WithError(err).Fatal("Failed to get container info")
		return err
	}

	// Retrieve the container's rootfs path
	rootfsPath, err := getContainerRootfsPath(client, container)
	if err != nil {
		logging.G(ctx).WithError(err).Fatal("Failed to retrieve container rootfs path")
		return err
	}

	// Read the log file
	logs, err := ioutil.ReadFile(filepath.Join(rootfsPath, "var/log/logs.txt"))
	if err != nil {
		logging.G(ctx).WithError(err).Fatal("Failed to read container logs")
		return err
	}

	// Display the logs
	fmt.Println(string(logs))
	return nil
}

func getContainerRootfsPath(client *containerd.Client, container containerd.Container) (string, error) {
	// Retrieve the container's snapshotter service
	ctx := namespaces.WithNamespace(context.Background(), "my-namespace")

	info, err := container.Info(ctx)
	if err != nil {
		return "", err
	}
	snapshotter := client.SnapshotService(info.Snapshotter)

	// Retrieve the mounts for the snapshot
	mounts, err := snapshotter.View(ctx, "tmpmount", info.SnapshotKey)
	if err != nil {
		return "", err
	}

	// For simplicity, just return the source of the first mount
	if len(mounts) > 0 {
		return mounts[0].Source, nil
	}

	return "", fmt.Errorf("failed to find rootfs mount for container")
}

func listAllContainers(ctx context.Context, client *containerd.Client) error {
	containers, err := client.Containers(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch containers: %v", err)
	}

	// Print container information
	for _, container := range containers {
		info, err := container.Info(context.Background())
		if err != nil {
			customLog.Fatal("Failed to get container info:", err)
			return err
		}

		fmt.Printf("Container ID: %s\n", info.ID)
		fmt.Printf("Container Image: %s\n", info.Image)
		fmt.Printf("Container Creation Date: %s\n", info.CreatedAt)

		// Get the task for the container
		task, err := container.Task(context.Background(), nil)
		if err != nil {
			customLog.Fatal("Failed to get task for container:", err)
			return err
		}

		// Get the status of the task
		status, err := task.Status(context.Background())
		if err != nil {
			customLog.Fatal("Failed to get task status:", err)
			return err
		}

		fmt.Printf("Container Status: %s\n", status.Status)

		fmt.Println("-------------------------------")
	}

	return nil
}

func createNamespace(namespace string) error {
	// Connect to the container runtime with a new namespace
	client, err := containerd.New("/run/containerd/containerd.sock", containerd.WithDefaultNamespace(namespace))
	if err != nil {
		return err
	}
	defer client.Close()

	// Use the newly created namespace for subsequent container operations
	namespaces.WithNamespace(context.Background(), "my-namespace")

	// Perform container operations using the namespace

	return nil
}
