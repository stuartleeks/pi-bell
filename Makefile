rsync-wfpi:
	rsync -r . pi@wfpi:/home/pi/source/pi-bell
rsync-raspberrypi:
	rsync -r . pi@raspberrypi:/home/pi/source/pi-bell
rsync-pibell-1:
	rsync -r . pi@pibell-1:/home/pi/source/pi-bell
rsync-pibell-2:
	rsync -r . pi@pibell-2:/home/pi/source/pi-bell
rsync-pibell-3:
	rsync -r . pi@pibell-3:/home/pi/source/pi-bell



run-bellpush:
	go run ./cmd/bellpush/main.go

run-bellpush-nogpio:
	DISABLE_GPIO=true go run ./cmd/bellpush/main.go

build-bellpush:
	GOOS=linux GOARCH=arm GOARM=5 go build -o bellpush ./cmd/bellpush/main.go 


run-chime:
	go run ./cmd/chime/main.go --addr=${DOORBELL}

build-chime:
	GOOS=linux GOARCH=arm GOARM=5 go build -o chime ./cmd/chime/main.go

fmt:
	find . -name '*.go' | grep -v vendor | xargs gofmt -s -w

checks:
	GO111MODULE=on golangci-lint run

release: checks build-bellpush build-chime
	tar -czvf pi-bell.tar.gz chime bellpush scripts/pibell-bellpush.service scripts/pibell-chime.service

install: build-bellpush build-chime
	mkdir -p /usr/local/bin/pi-bell
	cp bellpush /usr/local/bin/pi-bell
	cp chime /usr/local/bin/pi-bell