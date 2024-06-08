#!/bin/sh
/app/socket-gen --template /app/default.tmpl --output /etc/nginx/conf.d/default.conf --override-dir /etc/nginx/include.d --once --permissions 0o666 /sockets
(sleep5; /app/socket-gen --template /app/default.tmpl --output /etc/nginx/conf.d/default.conf --override-dir /etc/nginx/include.d --command "nginx -s reload" --permissions 0o666 /sockets) &
