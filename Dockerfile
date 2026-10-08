FROM node:20-bookworm-slim AS web-builder

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build


FROM golang:1.25-bookworm AS app-builder

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential ca-certificates cmake curl pkg-config tar \
    && rm -rf /var/lib/apt/lists/*

ARG USEARCH_VERSION=2.25.3
ARG USEARCH_SHA256=18748a2507b257c3a9e42562fef3c52291bce0fd6026611cdc3e242532411e71
RUN curl -fsSL \
        "https://github.com/unum-cloud/USearch/releases/download/v${USEARCH_VERSION}/usearch_linux_amd64_${USEARCH_VERSION}.deb" \
        -o /tmp/usearch.deb \
    && echo "${USEARCH_SHA256}  /tmp/usearch.deb" | sha256sum -c - \
    && dpkg -i /tmp/usearch.deb \
    && rm -f /tmp/usearch.deb

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /src/web/dist ./web/dist

RUN cmake -S llama -B llama/cmake-build-release -DCMAKE_BUILD_TYPE=Release \
    && cmake --build llama/cmake-build-release -j"$(nproc)"

RUN mkdir -p lib \
    && find ./llama/cmake-build-release -name "*.so*" -exec cp {} ./lib/ \; \
    && cp /usr/local/lib/libusearch_c.so ./lib/

RUN bash ./scripts/build_onnx_layout.sh

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -tags inkflow_onnx -trimpath -ldflags="-s -w" -o InkFlow main.go


FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libgomp1 libstdc++6 poppler-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

ENV LD_LIBRARY_PATH=/app/lib

COPY --from=app-builder /src/InkFlow /app/InkFlow
COPY --from=app-builder /src/lib /app/lib
COPY --from=app-builder /src/ocr /app/ocr
COPY --from=app-builder /src/web/dist /app/web/dist
COPY config.docker.example.yaml /app/config.yaml

RUN mkdir -p /app/log /app/llama/llama.cpp/models

EXPOSE 8888

CMD ["/app/InkFlow"]
