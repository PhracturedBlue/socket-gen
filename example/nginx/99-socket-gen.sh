#!/bin/sh
/app/socket-gen --templatefile /app/default.tmpl --output /etc/nginx/conf.d/default.conf --override-dir /etc/nginx/include.d /sockets &
