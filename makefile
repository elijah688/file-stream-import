.PHONY: run read write run-reader run-writer gen_file 

read:
	go run cmd/reader/main.go

write:
	go run cmd/writer/main.go

AIR_CMD = air

run-reader:
	bash -c "$(AIR_CMD) -c .air.reader.toml;"

run-writer:
	bash -c "$(AIR_CMD) -c .air.writer.toml;"

gen_file:
	go run cmd/gen_file/main.go
