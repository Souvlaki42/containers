# Containers in Go

This is a simple container runtime written in Go

## Prerequisites

- Have [Go](https://go.dev/doc/install) installed
- Have [Crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane) installed

## Usage

Clone the repo using:

```bash
git clone https://github.com/Souvlaki42/go-containers.git
```

Then, setup the root filesystem by doing:

```bash
mkdir root-fs
crane export ubuntu | sudo tar -xvC ./root-fs
```

Finally, run the thing with:

```bash
go run main.go run /bin/bash
```

## Inspirations

- [Liz Rice - GOTO Amsterdam 2018](https://www.youtu.be/8fi7uSYlOdc)
- [Build a container in Go](https://www.infoq.com/articles/build-a-container-golang)
- [How container filesystems work](https://labs.iximiuz.com/tutorials/container-filesystem-from-scratch)
- [How container networks work](https://labs.iximiuz.com/tutorials/container-networking-from-scratch)

## LICENSE

This whole project is licensed under [The Unlicense](./LICENSE)
