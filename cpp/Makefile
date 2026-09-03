# shpreflight C++ port — builds on Linux, macOS and Windows (MinGW).
# MSVC instead:  cl /O2 /utf-8 /EHsc shpreflight.cpp   (sources are UTF-8; /utf-8 is required)

CXX      ?= c++
CXXFLAGS ?= -O2 -std=c++17 -Wall -Wextra
BIN      = shpreflight

.PHONY: all test clean

all: $(BIN)

$(BIN): shpreflight.cpp patterns_gen.h
	$(CXX) $(CXXFLAGS) -o $(BIN) shpreflight.cpp

test: test_shpreflight
	./test_shpreflight

test_shpreflight: test_shpreflight.cpp shpreflight.cpp patterns_gen.h
	$(CXX) $(CXXFLAGS) -DSHPREFLIGHT_NO_MAIN -o test_shpreflight test_shpreflight.cpp

clean:
	rm -f $(BIN) test_shpreflight test_pathbin
