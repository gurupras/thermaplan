PROG_NAME=thermaplan

vpath %.h $(INCLUDE)

LDFLAGS=-L.

sources=main netlink common mpdecision_handler move_to_cgroup_handler cpuset_handler
test_sources=test_main netlink common mpdecision_handler move_to_cgroup_handler cpuset_handler
sources_go=$(patsubst %,%.go,$(sources))
test_sources_go=$(patsubst %,%.go,$(test_sources))
GOARCH=

all: binary

arm: GOARCH=arm
arm: binary

VERSION=$(shell git rev-parse --short HEAD)
TIMESTAMP=$(shell date +%Y-%m-%d.%H:%M:%S)

binary:
	GOARCH=$(GOARCH) go build -ldflags "-X main.Version=$(VERSION) -X main.Timestamp=$(TIMESTAMP)" -o $(PROG_NAME) $(sources_go)

shared:
	go build -buildmode=c-shared -o libaosp_su_daemon.so $(sources_go)

static:
	go build -buildmode=c-archive -o libaosp_su_daemon.a $(sources_go)

test: sources=$(test_sources)
test: arm

phone: arm
	adb push $(PROG_NAME) /system/bin/$(PROG_NAME)

%.o: %.c
	gcc -c $< -o $@
clean:
	rm -f $(PROG_NAME) lib$(PROG_NAME).* test

