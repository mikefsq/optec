# optec

Go driver for Optec FocusLynx and ThirdLynx focusers, using USB-serial without
a vendor SDK. FocusLynx has two channels (F1 and F2); ThirdLynx has one (F1).

## Build and run

Requires Go 1.25 or later.

```sh
go build -o flprobe ./cmd/flprobe
./flprobe -list
./flprobe -nickname "OAG focuser"
```

The default probe run reads hub information, status, and configuration.
Use `-port` to choose a serial port and `-ch 1` or `-ch 2` to choose a channel.
`-nickname` discovers the channel by its stored name. Give channels unique
nicknames with `-setnick` when using more than one.

```sh
./flprobe -nickname "OAG focuser" -moveto 12000
./flprobe -nickname "OAG focuser" -stop
```

Motion and configuration flags operate the hardware. `-cfgtest` temporarily
changes settings and attempts to restore them. Run `-help` for all options.

## Use the library

```go
package main

import (
    "fmt"
    "log"

    "github.com/mikefsq/optec/focuslynx"
)

func run() error {
    hub, channel, err := focuslynx.OpenByNickname("OAG focuser")
    if err != nil {
        return err
    }
    defer hub.Close()

    position, err := hub.Focuser(channel).Position()
    if err != nil {
        return err
    }
    fmt.Println(position)
    return nil
}

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}
```

`OpenFirst` discovers a hub; `OpenPort` opens a specified port. A hub owns the
shared serial connection and exposes each channel through `Focuser`.

## Platforms and development

Serial I/O supports Linux, macOS, and Windows without cgo. On macOS,
discovery uses device names; elsewhere it uses USB IDs. The service user must
have permission to open the serial port.

```sh
go test -race ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
```

See [PROTOCOL.md](PROTOCOL.md) for commands and reply formats. Protocol tests
use a fake `Transport`. The [Alpaca driver](https://github.com/mikefsq/goalpaca-devices/tree/main/focuslynx)
is a separate module.

## License

[MIT](LICENSE).
