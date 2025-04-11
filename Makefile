.SILENT :
.PHONY : socket-gen clean fmt

# TAG:=`git describe --tags`
# LDFLAGS:=-X main.buildVersion=$(TAG)

all: socket-gen

socket-gen:
	echo "Building socket-gen"
	#go build -ldflags "$(LDFLAGS)" ./cmd/socket-gen
	go build ./cmd/socket-gen

check-gofmt:
	if [ -n "$(shell go fmt ./cmd/...)" ]; then \
		echo 1>&2 'The following files need to be formatted:'; \
		gofmt -l ./cmd/socket-gen; \
		exit 1; \
	fi

get-deps:
	go mod download
