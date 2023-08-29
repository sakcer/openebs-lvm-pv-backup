# Build the manager binary
ARG TARGETOS
ARG TARGETARCH

FROM ubuntu:latest
WORKDIR /root
RUN apt update && apt install proot -y
RUN mkdir /mount && mkdir /mount/backup
COPY restic /usr/bin/restic
COPY bin/manager /root/manager


# USER 65532:65532
USER 0:0

# ENTRYPOINT ["/manager"]
