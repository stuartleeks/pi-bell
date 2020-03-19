rsync:
	rsync -r . pi@wfpi:/home/pi/source/pi-bell

run-bellpush:
	go run ./cmd/bellpush/main.go

run-bellpush-nogpio:
	DISABLE_GPIO=true go run ./cmd/bellpush/main.go

build-bellpush:
	go build -o bellpush ./cmd/bellpush/main.go 


run-chime:
	go run ./cmd/chime/main.go --addr=${DOORBELL}