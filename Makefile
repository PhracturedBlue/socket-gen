.SILENT :
.PHONY : docker-gen clean fmt

TAG:=`git describe --tags`
LDFLAGS:=-X main.buildVersion=$(TAG)

all: docker-gen

socket-gen:
	echo "Building socket-gen"
	go build -ldflags "$(LDFLAGS)" ./cmd/socket-gen

check-gofmt:
	if [ -n "$(shell go fmt ./cmd/...)" ]; then \
		echo 1>&2 'The following files need to be formatted:'; \
		gofmt -l ./cmd/docker-gen; \
		exit 1; \
	fi

get-deps:
	go mod download
