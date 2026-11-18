# WEDnesday Playground
WIP online playground for [Wednesday](https://github.com/DazFather/Wednesday) framework.
> When ready this repo will probably move as a subdir of the main one.

## Requirements
- [go](go.dev)
- [Wednesday](https://github.com/DazFather/Wednesday)
- [podman](https://podman.io) or another OCI provider like [docker](https://docker.com)

## Setup
- $ `cd ./oci; podman build . -t wedimg`
> The example shows the build using podman, you can use any another OCI provider like "docker"
- $ `wed build`
- $ `go build`
- $ `./wed-playground`
> Use flags like --port to specify the HTTP port (defaults to :8080) and --oci for the OCI provider (defaults to podman)
