BIN := ipxe-presign

.PHONY: all build test vet clean

all: build

build:
	go build -o $(BIN) .

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
