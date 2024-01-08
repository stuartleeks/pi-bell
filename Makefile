.PHONY: help
help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%s\033[0m|%s\n", $$1, $$2}' \
		| column -t -s '|'

run-bellpush: ## run the bellpush
	cd cmd/bellpush && go run main.go

run-bellpush-nogpio: ## run the bellpush with gpio disabled
	cd cmd/bellpush && DISABLE_GPIO=true go run main.go

run-bellpush-nogpio-nowebcam: ## run the bellpush with gpio disabled
	cd cmd/bellpush && DISABLE_GPIO=true DISABLE_WEBCAM=true go run main.go

build-bellpush: ## build the bellpush
	# Using zig to cross compile for arm: https://github.com/vladimirvivien/go4vl/tree/main/examples#cross-compile-with-zig-toolchain
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=1 CC="zig cc -target arm-linux-musleabihf" CXX="zig c++ -target arm-linux-musleabihf" go build -o bellpush ./cmd/bellpush/main.go 


run-chime: ## run the chime (set DOORBELL)
	go run ./cmd/chime/main.go --addr=${DOORBELL}

run-chime-nogpio: ## run the chime (set DOORBELL)
	DISABLE_GPIO=true go run ./cmd/chime/main.go --addr=${DOORBELL}


build-chime: ## build the chime
	GOOS=linux GOARCH=arm GOARM=5 go build -o chime ./cmd/chime/main.go

fmt: ## go fmt
	find . -name '*.go' | grep -v vendor | xargs gofmt -s -w

checks: ## run checks/linter
	GO111MODULE=on golangci-lint run

build-all: checks build-bellpush build-chime

release: build-all ## build the release archive
	tar -czvf pi-bell.tar.gz chime bellpush scripts/pibell-bellpush.service scripts/pibell-chime.service scripts/chime.env

install: build-bellpush build-chime ## install the bellpush and chime
	mkdir -p /usr/local/bin/pi-bell
	cp bellpush /usr/local/bin/pi-bell
	cp chime /usr/local/bin/pi-bell



rsync-wfpi4: 
	rsync -r . pi@wfpi4:/home/pi/source/pi-bell
rsync-raspberrypi:
	rsync -r . pi@raspberrypi:/home/pi/source/pi-bell
rsync-pibell-1:
	rsync -r . pi@pibell-1:/home/pi/source/pi-bell
rsync-pibell-2:
	rsync -r . pi@pibell-2:/home/pi/source/pi-bell
rsync-pibell-3:
	rsync -r . pi@pibell-3:/home/pi/source/pi-bell


