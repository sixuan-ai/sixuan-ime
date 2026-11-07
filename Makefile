.PHONY: build test vet deb
build:
	mkdir -p dist
	go build -buildvcs=false -trimpath -o dist/sixuan-ime .
test:
	go test ./...
vet:
	go vet ./...
deb:
	bash packaging/build-deb.sh
