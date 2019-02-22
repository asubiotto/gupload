install:
	go install -v

grpc:
	protoc \
		./messaging/service.proto \
		--gogofaster_out=plugins=grpc:.

.PHONY: install grpc
