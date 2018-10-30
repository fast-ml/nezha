.PHONY: all

WEBHOOK_IMAGE_NAME=$(if $(ENV_WEBHOOK_IMAGE_NAME),$(ENV_WEBHOOK_IMAGE_NAME),docker.io/rootfs/hostalias-webhook)

all: initializer webhook

initializer:
	if [ ! -d ./vendor ]; then dep ensure; fi
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -i -o  _output/initializer app/initializer.go

webhook:
	if [ ! -d ./vendor ]; then dep ensure; fi
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -i -o  _output/webhook app/webhook.go

deploy_webhook: webhook
	cp _output/webhook deploy/docker
	docker build -t ${WEBHOOK_IMAGE_NAME} deploy/docker
	docker push ${WEBHOOK_IMAGE_NAME}

clean:
	go clean -r -x
	-rm -rf _output
