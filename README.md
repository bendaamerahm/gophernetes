# Gophernetes

Gophernetes is a minimal, educational container runtime written in Go. It is designed to illustrate some of the fundamental principles behind containerization, including process isolation with namespaces, control groups, and filesystem isolation.

## Features

- **Namespaces**: Gophernetes creates new namespaces (PID, UTS, NS, NET) for the container, providing isolation from the host.
- **Chroot**: Gophernetes uses the chroot system call to change the apparent root directory for the current running process and its children.
- **Mount**: Gophernetes mounts a proc filesystem for the container in a new mount namespace.
- **Control Groups**: Gophernetes limits the memory usage of the container through cgroups.
- **Networking**: Gophernetes sets up a basic virtual ethernet networking stack inside the container using a separate network namespace.

## Usage

First, build the Go application:

```
go build -o gophernetes
```
Then, use the gophernetes binary to start a new container:
```
sudo ./gophernetes run /bin/bash
```
You will be dropped into a new shell running inside the container. From this shell, you can run commands isolated from the host.

The binary accepts commands in the following format:
```
./gophernetes [run | child] command [arguments...]
run: This starts a new container and runs the provided command. The run command should be followed by the command to run inside the container, for example /bin/bash.
child: This is an internal command used by gophernetes to start the isolated process inside the new namespaces.
```
## Building the Docker Image
You can build a Docker image for Gophernetes with the provided Dockerfile:

```
docker build -t gophernetes .
```
You can then run Gophernetes inside a Docker container. However, due to the nature of the operations it's performing (like creating namespaces, setting up network interfaces, etc.), it requires privileged permissions to work properly:

```
docker run --privileged -it gophernetes run /bin/bash
```

## Warning
Please note that Gophernetes is a minimal, educational container runtime. It is not feature-complete and is not intended for production use. Features like container image downloading, advanced networking, and resource isolation are either simplified or missing.

## License
Gophernetes is open-source software licensed under the MIT license.

### Additional Features:
In terms of features, this is a pretty solid basic implementation of a container runtime. Here are some additional features you could consider adding:

1. **Container Image Support**: Right now, the application assumes that the necessary file system (rootfs) is available on the host. Adding support to pull and unpack container images from a Docker registry would make this a more self-contained tool.

2. **Better Networking**: Right now, the network setup is quite rudimentary. You could look into how Docker sets up advanced networking, with a virtual ethernet bridge, NAT and IPTables rules.

3. **Isolation for Other Resources**: Right now, we're only limiting memory usage. You could add similar limitations for CPU, block I/O, etc.

4. **Better Error Handling**: Right now, we're ignoring all errors from the setupNetwork and limitMemory functions. These operations can and will fail in many scenarios, and we should handle these errors appropriately.

Please remember that these are all advanced features that involve quite a bit of additional complexity. I would recommend you to thoroughly understand the existing code and concepts before attempting to add these.