# DOS doors for the BBS: dosemu2 (with the fdpp kernel and the comcom64
# shell), built from source as dosemu2's own Dockerfile.debian does (on
# Ubuntu, which has every build dependency), so that no PPA is needed.
#
#   docker build -t bbs-dosemu2 doors/dosemu2       (or: make doors-image)
#
# The BBS runs a door as
#
#   docker run --rm -i --network none --user UID:GID \
#       -v /srv/doors:/doors -v NODEDIR:/node bbs-dosemu2 'F:\LORD' 'LORD.EXE'
#
# with the caller's line on the door's COM1, behind dosemu2's FOSSIL: see
# rundoor.  The pieces are built at their newest; pin them with the *_REF
# build arguments (a tag or a commit each).

ARG BASE=ubuntu:26.04

FROM ${BASE} AS builder
WORKDIR /src
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential devscripts meson git python3-ply ca-certificates \
        nasm pkgconf libelf-dev binutils-i686-linux-gnu smallerc ninja-build \
        debhelper universal-ctags gcc-i686-linux-gnu \
        libb64-dev libjson-c-dev libslirp-dev libseccomp-dev libslang2-dev \
        libsearpc-dev libsdl3-dev libfluidsynth-dev gawk xfonts-utils \
        libgpm-dev libsdl3-ttf-dev libfontconfig1-dev ladspa-sdk libao-dev \
        libieee1284-3-dev valgrind bison flex autoconf automake libreadline-dev \
        libasound2-dev libx11-dev libxext-dev libglib2.0-dev

# the pieces, in order; each a git ref (a tag or commit to pin a build)
ARG THUNK_GEN_REF=HEAD FDPP_REF=HEAD DJSTUB_REF=HEAD DJDEV64_REF=HEAD \
    DJ64DEV_REF=HEAD COMCOM64_REF=HEAD DOSEMU2_REF=HEAD
COPY build.sh /src/
RUN ./build.sh thunk_gen https://github.com/stsp/thunk_gen.git $THUNK_GEN_REF && \
    apt-get install -y ./thunk-gen*.deb
RUN ./build.sh fdpp https://github.com/dosemu2/fdpp.git $FDPP_REF && \
    apt-get install -y ./libfdpp*.deb ./libfdldr*.deb ./fdpp*.deb
RUN ./build.sh djstub https://github.com/stsp/djstub.git $DJSTUB_REF && \
    apt-get install -y ./djstub*.deb
RUN ./build.sh djdev64 https://github.com/dosemu2/djdev64.git $DJDEV64_REF && \
    apt-get install -y ./libdjstub64*.deb ./libdjdev64*.deb ./djdev64-dev*.deb
RUN ./build.sh dj64dev https://github.com/stsp/dj64dev.git $DJ64DEV_REF && \
    apt-get install -y ./dj64*.deb ./dj32*.deb
RUN ./build.sh comcom64 https://github.com/dosemu2/comcom64.git $COMCOM64_REF && \
    apt-get install -y ./comcom64*.deb
RUN ./build.sh dosemu2 https://github.com/dosemu2/dosemu2.git $DOSEMU2_REF && \
    apt-get install -y ./dosemu2*.deb
RUN mkdir /debs && cp *.deb /debs/ && ls /debs

FROM ${BASE}
ARG DEBIAN_FRONTEND=noninteractive
COPY --from=builder /debs /debs
RUN apt-get update && \
    apt-get install -y --no-install-recommends socat \
        /debs/dosemu2_*.deb /debs/comcom64_*.deb /debs/comcom32_*.deb /debs/dj64_*.deb \
        /debs/libdjdev64-0_*.deb /debs/libdjstub64-0_*.deb /debs/libfdldr*.deb /debs/libfdpp*.deb && \
    rm -rf /debs /var/lib/apt/lists/*
# any user but root (dosemu2 will not run a port's program as root): the
# BBS's own, with --user, so that the drop files are its; dosemu2's own
# files go in a home under /tmp
RUN useradd -m door && mkdir -p /doors /node
COPY rundoor /usr/local/bin/rundoor
COPY dosemurc /etc/dosemu-door.conf
RUN chmod 755 /usr/local/bin/rundoor && chmod 644 /etc/dosemu-door.conf
USER door
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/rundoor"]
