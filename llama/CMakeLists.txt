cmake_minimum_required(VERSION 3.14)
project(InkFlowLlama C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Keep the layout stable for the CGo backend link files:
#   bin/  runtime DLLs and executables
#   dll/  shared libraries when BUILD_SHARED_LIBS=ON
#   lib/  static libraries or import libraries
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/bin)
set(CMAKE_LIBRARY_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/dll)
set(CMAKE_ARCHIVE_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR}/lib)
foreach(_config Debug Release RelWithDebInfo MinSizeRel)
    string(TOUPPER "${_config}" _config_upper)
    set(CMAKE_RUNTIME_OUTPUT_DIRECTORY_${_config_upper} ${CMAKE_BINARY_DIR}/bin)
    set(CMAKE_LIBRARY_OUTPUT_DIRECTORY_${_config_upper} ${CMAKE_BINARY_DIR}/dll)
    set(CMAKE_ARCHIVE_OUTPUT_DIRECTORY_${_config_upper} ${CMAKE_BINARY_DIR}/lib)
endforeach()

option(LLAMA_BUILD_EXAMPLES "Build llama.cpp examples" OFF)
option(LLAMA_BUILD_TESTS "Build llama.cpp tests" OFF)
option(LLAMA_BUILD_SERVER "Build llama-server" OFF)
option(LLAMA_BUILD_TOOLS "Build llama.cpp tools" OFF)
option(GGML_CUDA "Build the CUDA backend" OFF)
option(GGML_VULKAN "Build the Vulkan backend" OFF)

add_subdirectory(llama.cpp)
