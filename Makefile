.PHONY: all
all: test

.PHONY: autocomplete
autocomplete:
	go install .

.PHONY: protocgorums
protocgorums:
	go get github.com/relab/gorums-dev/cmd/protoc-gen-gorums

.PHONY: proto
proto: protocgorums
	protoc -I ../../../:. --gorums_out=plugins=grpc+gorums:. gorumspb/gorums.proto
	protoc -I ../../../:. --gogofast_out=. raftpb/raft.proto

.PHONY: test
test:
	go test -v

.PHONY: bench
bench:
	go test -v -run ^none -bench .

.PHONY: lint
check:
	@gometalinter --config metalinter.json
