rsync-wfpi:
	rsync -r . pi@wfpi:/home/pi/source/pi-bell
rsync-raspberrypi:
	rsync -r . pi@raspberrypi:/home/pi/source/pi-bell

run-bellpush:
	go run ./cmd/bellpush/main.go

run-bellpush-nogpio:
	DISABLE_GPIO=true go run ./cmd/bellpush/main.go

build-bellpush:
	go build -o bellpush ./cmd/bellpush/main.go 


run-chime:
	go run ./cmd/chime/main.go --addr=${DOORBELL}

build-chime:
	go build -o chime ./cmd/chime/main.go