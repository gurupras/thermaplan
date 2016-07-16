PROG_NAME=thermaplan

vpath %.h $(INCLUDE)

LDFLAGS=-L.

sources=main
sources_go=$(patsubst %,%.go,$(sources))
GOARCH=

all: binary

arm: GOARCH=arm
arm: binary

VERSION=$(shell date +%Y-%m-%d.%H:%M:%S)

binary:
	GOARCH=$(GOARCH) go build -ldflags "-X main.Version=$(VERSION)" -o $(PROG_NAME) $(sources_go)

shared:
	go build -buildmode=c-shared -o libaosp_su_daemon.so $(sources_go)

static:
	go build -buildmode=c-archive -o libaosp_su_daemon.a $(sources_go)

phone: arm
	adb push $(PROG_NAME) /system/bin/$(PROG_NAME)

%.o: %.c
	gcc -c $< -o $@
clean:
	rm -f $(PROG_NAME) lib$(PROG_NAME).* test

