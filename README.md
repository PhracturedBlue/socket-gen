# socket-gen
`socket-gen` is designed to fill a similar need as [docker-gen](https://github.com/nginx-proxy/docker-gen) when used as a reverse-proxy.
Specifically, it is being used to automatically detect virtual-hosts and to dynamically build an nginx configuration file for them as
they apear and disappear.  However unlike `docker-gen`, `socket-gen` does not access the Docker control socket, and instead detects
hosts by looking for the existance of unix-domain-socketes in specified directories (It can also support TCP based hosts by looking
for specifcially named files in the same directories).

The main use case is to be able to have containerized services run with `--network=none` and communicate solely over
unix-domain-sockets.  Coupled with Podman's socket-activation for containers, this can also allow nginx to run with `--network=none`
as well.  Such a configuration can allow all services (including nginx) to run in a rootless environment without network access,
potentially improving security.  When socket-activation is used to manage services, it can also improve boot time and memory usage
by delaying service startup until the 1st access.

While `socket-gen` is targetted at use with Nginx, it is not tightly coupled with it, and there may be other interesting use-cases,
similar to `docker-gen`, although probably not as flexible as the amount of information available for templating is less.

## Implementation
`socket-gen` will generate a template variable containing specific environment variables, whether it was run in a socket-activated,
environment, and a dictionary containing found entires and their attributes.  The entries are found by recursively monitoring one
or more directories for change using inotify.  It looks for 4 types of files being created or removed:
* Any unix domain socket
* a file named override.<some extention>
* a file named host.yml
* a file named host
Whenever a change occurs, it will read a template file (following golang's text/template syntax) and generate an output-file by
applying the generated template variable.  It will then run a trigger command if specified to indicate updates.

Any environement variable prefixed with `SOCKETGEN_` will have the prefix removed and be stored as a key-value pair.
If one of the listed file-types is found, it is stored in a key-value pair with the key being the name of the parent directory
containing the file.  For instance if a monitoried directory contains `/foo/bar/vhost.local/unixdomain.sock`, then an entry
would be added with `key=vhost.local` and the value would contain `.SocketPath=/foo/bar/vhost.local/unixdomain.sock`.  Or if
a monitored directory contained `/host2.lan/host` and the `host` file contained `0.0.0.0:3000`, then an entry would be added
with `key=host2.lan` and the value would contain `.Name=0.0.0.0:3000`.

## Templates
The template file is composed as follows:
```
{
    .Env = {
        Var1:Key1,
        Var2:Key2,
        ...
        },
    .ListenAddrs = [Addr1, Addr2, ...],
    .Hosts = {
        Host1: {
            .SocketPath,
            .Name,
            .Args = {
               Arg1:Value1,
               Arg2:Value2,
               ...,
               }
            .Overrides = [Overridefile1, Overridefile2, ...]
            },
        Host2: ...
    }
}
```

The ListenAddrs can be specified in the `LISTEN_ADDRS` environment variable (space separated), or will be
auto-detected from any socket-activated sockets that are available (as per the Systemd passing method using
the `LISTEN_FDS` environment variable.

## Command-line usage
```
socket-gen --templatefile <filename> --output <filename> [options] path1 path2 ...
```
* --templatefile (**required**): Input file using golang's `text/template` templating system
* --output (**required**): Output file to generate from template
* <paths> (**optional**): Paths to recirsively monitor for activity.  Defaults to the current directory
* --trigger (**optional**): Command to run whenever the output file is re-written
* --override-dir (**optional**): directory to place override files in if found

## Using with a reverse proxy
The included example, will build a reverse-proxy container, which can be used to automatically forward to
virtual hosts.  While this can be used with Docker, it works better using Podman and socket-activation
```
podman build --tag reverse-proxy:socket-activated example/nginx
cp example/nginx/reverse-proxy.s* ~/.config/systemd/user/
<< change ~/.config/systemd/user/reverse-proxy.socket with the approriate listening port >>
systemctl --user daemon-reload
systemctl --user enable reverse-proxy
systemctl --user start reverse-proxy
```
Then ensure that you have all services create their unix-domain-sockets in $XDG_RUNTIME_DIR/nginx_sockets/<vhost name>/<somefile>.sock
