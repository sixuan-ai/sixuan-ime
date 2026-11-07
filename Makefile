.PHONY: build test vet deb schema-test
build:
	mkdir -p dist
	go build -buildvcs=false -trimpath -o dist/sixuan-ime .
test:
	go test ./...
schema-test:
	bash tests/desktop_schema_test.sh
vet:
	go vet ./...
deb:
	bash packaging/build-deb.sh
