FROM ubuntu:focal as build

# https://serverfault.com/questions/949991/how-to-install-tzdata-on-a-ubuntu-docker-image
ENV DEBIAN_FRONTEND=noninteractive

# For TC and tcpdump
RUN apt-get update -y && apt-get install -y curl

# For Go 1.16
ENV PATH=$PATH:/usr/local/go/bin
RUN curl -L https://go.dev/dl/go1.18.10.linux-amd64.tar.gz |tar -xz -C /usr/local

# Note that git is very important for codecov to discover the .codecov.yml
RUN apt update && apt install -y gcc g++ make patch

ADD . /g
WORKDIR /g
RUN cd backend && go build .

# Use UPX to compress the binary.
RUN apt-get install -y upx

RUN echo "Before UPX for $TARGETARCH" && \
    ls -lh /g/backend/* && \
    upx --best --lzma /g/backend/server && \
    echo "After UPX for $TARGETARCH" && \
    ls -lh /g/backend/*

FROM node:18-slim as ui

ARG MAKEARGS
RUN echo "MAKEARGS: $MAKEARGS"

ADD . /g
WORKDIR /g

RUN npm i && npm run build

FROM ossrs/srs:ubuntu20 as ffmpeg

# Use UPX to compress the binary.
# https://serverfault.com/questions/949991/how-to-install-tzdata-on-a-ubuntu-docker-image
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && apt-get install -y upx

RUN echo "Before UPX for $TARGETARCH" && \
    ls -lh /usr/local/bin/* && \
    upx --best --lzma /usr/local/bin/ffmpeg && \
    upx --best --lzma /usr/local/bin/ffprobe && \
    echo "After UPX for $TARGETARCH" && \
    ls -lh /usr/local/bin/*

FROM ubuntu:focal as dist

COPY --from=ffmpeg /usr/local/bin/ffmpeg /usr/local/bin/
COPY --from=build /g/backend/*.aac /g/backend/*.mp3 /g/backend/*.opus /g/backend/server /g/backend/
COPY --from=ui /g/build /g/build

ENV AIT_HTTP_LISTEN=3000 AIT_HTTPS_LISTEN=3443 AIT_PROXY_STATIC=false

WORKDIR /g/backend
CMD ["./server"]
