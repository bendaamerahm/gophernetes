package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func main() {
	help := flag.Bool("help", false, "Display help information")

	flag.Usage = func() {
		fmt.Println(`Gophernetes is a minimal, educational container runtime written in Go.

Usage:
  gophernetes [options] command

Options:`)
		flag.PrintDefaults()

		fmt.Println(`\nCommands:
  run <cmd> - Starts a new container and runs the provided command.
  child     - An internal command used by Gophernetes to start the isolated process inside the new namespaces.`)
	}

	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	switch flag.Arg(0) {
	case "run":
		// os.Args should include the command and all arguments.
		// flag.Args() excludes the flags and their values, so we use os.Args[2:]
		// to get the command to run inside the container.
		os.Args = append([]string{"run"}, flag.Args()[1:]...)
		run()
	case "child":
		// os.Args should include the command and all arguments.
		// flag.Args() excludes the flags and their values, so we use os.Args[2:]
		// to get the command to run inside the container.
		os.Args = append([]string{"child"}, flag.Args()[1:]...)
		child()
	default:
		fmt.Println("Unknown command:", flag.Arg(0))
		flag.Usage()
	}
}
func run() {
	cmd := exec.Command("/proc/self/exe", append([]string{"child"}, os.Args[2:]...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Here is where we will create the new namespaces for our "container"
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
	}

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

	// Clean up the cgroup once we have finished
	os.RemoveAll("/sys/fs/cgroup/memory/mydocker")
}

func child() {
	// Setup the network
	setupNetwork()

	// Limit the memory usage
	limitMemory()

	cmd := exec.Command(os.Args[2], os.Args[3:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Here is where we do the chroot to the new filesystem for our "container"
	if err := syscall.Chroot("/home/newroot"); err != nil {
		log.Fatal(err)
	}
	if err := os.Chdir("/"); err != nil {
		log.Fatal(err)
	}

	// Here is where we would setup the new /proc for our "container"
	if err := syscall.Mount("proc", "proc", "proc", 0, ""); err != nil {
		log.Fatal(err)
	}

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
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
