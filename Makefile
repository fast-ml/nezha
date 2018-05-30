.PHONY: all

all: initializer

initializer:
	if [ ! -d ./vendor ]; then dep ensure; fi
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -i -o  _output/initializer app/main.go


clean:
	go clean -r -x
	-rm -rf _output
