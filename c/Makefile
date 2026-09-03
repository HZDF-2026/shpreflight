# shpreflight C port — builds on Linux, macOS and Windows (MinGW).
# MSVC instead:  cl /O2 /utf-8 shpreflight.c   (the sources are UTF-8; /utf-8 is required)

CC      ?= cc
CFLAGS  ?= -O2 -std=c99 -Wall -Wextra
BIN     = shpreflight

.PHONY: all test clean

all: $(BIN)

$(BIN): shpreflight.c patterns_gen.h
	$(CC) $(CFLAGS) -o $(BIN) shpreflight.c

test: test_shpreflight
	./test_shpreflight

test_shpreflight: test_shpreflight.c shpreflight.c patterns_gen.h
	$(CC) $(CFLAGS) -DSHPREFLIGHT_NO_MAIN -o test_shpreflight test_shpreflight.c

clean:
	rm -f $(BIN) test_shpreflight test_pathbin
