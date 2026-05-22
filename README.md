# Containers

A simple container runtime written for me to finally understand the inner workings of containers.

## Prerequisites

This tool is Linux specific by nature. Consider using [WSL2] or similars for Windows and MacOS.

- Have [Go](https://go.dev/doc/install) installed
- Have [Crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane) installed

## Installation

First, make sure `$GOPATH/bin`, usually ~/go/bin is in your `$PATH`.

Then download the binary:

```bash
go install https://github.com/Souvlaki42/containers@latest
```

Finally, run it with:

```bash
containers -i ubuntu -c /bin/bash
```

## Development

First, clone this repository somewhere.

Then, run the thing with:

```bash
go run main.go -i ubuntu -c /bin/bash
```

## Uninstallation

Delete the following stuff:

```bash
rm "$HOME/go/bin/containers" # if it exists and that's your $GOPATH
rm -rf "$HOME/.containers"
```

## TODO

- [x] Implement this new API.
- [ ] Setup more filesystem stuff.
- [ ] Setup a few extra namespaces.
- [ ] Make it more secure.
- [ ] Drop the dependency on crane.

## Inspirations

- [Liz Rice - GOTO Amsterdam 2018](https://www.youtu.be/8fi7uSYlOdc)
- [Build a container in Go](https://www.infoq.com/articles/build-a-container-golang)
- [How container filesystems work](https://labs.iximiuz.com/tutorials/container-filesystem-from-scratch)
- [How container networks work](https://labs.iximiuz.com/tutorials/container-networking-from-scratch)

## LICENSE

This whole project is licensed under [The Unlicense](./LICENSE)
