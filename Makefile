run-bellpush:
	go run ./cmd/bellpush/main.go

run-bellpush-nogpio:
	DISABLE_GPIO=true go run ./cmd/bellpush/main.go

build-bellpush:
	go build -o bellpush ./cmd/bellpush/main.go 