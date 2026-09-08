# The dedicated-session image.
#
# One process is one session, so this image contains one thing: the statically
# linked `vif` binary. Nothing else is in the final layer — no shell, no libc, no
# package manager, no configuration tree — because everything a session needs at
# runtime is either compiled in (the embedded FSM scenario and corpus) or mounted
# read-only by the workload that runs it. What is not there cannot be executed by
# a peer that finds a way to make the game run something, and a session pod is
# reachable by whoever can reach the game port.
#
# Build from the repository root, which is the context this expects:
#
#   docker build -f deploy/docker/Dockerfile -t vi-fighter:dev .
#   make image                      # the same, with the revision label filled in
#
# The result is one layer of about 13 MB. See doc/kube_docker_deploy.md.

# Keep this pin equal to the go directive in ../../go.mod. The dependency layer
# downloads before an older builder reports the mismatch, so drift fails late.
ARG GO_VERSION=1.27.1

# --- builder -------------------------------------------------------------------
# The builder is not thin and does not need to be; nothing from it is shipped
# except /out/vif. git is installed so the Go toolchain can stamp the commit into
# the binary's own build info, which is what makes a running pod attributable to a
# revision without trusting a label somebody could have written by hand.
FROM docker.io/library/golang:${GO_VERSION}-alpine AS build

RUN apk add --no-cache git
WORKDIR /src

# Module graph first: it changes far less often than the source, so an ordinary
# code change reuses this layer instead of re-downloading the dependencies.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

# CGO off is what makes the binary static, and static is what lets the final stage
# be scratch. -trimpath removes the builder's paths from the binary; -s -w drop the
# symbol table and DWARF, and leave the Go build info the run is identified by.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/vif ./cmd/vif

# Prove the artifact in the builder rather than discovering it in a pod: a binary
# that cannot resolve its own embedded configuration is not one to publish. Skipped
# when cross-compiling, where the host cannot execute what was just built; the
# workload's init container runs the same check against the config it will serve.
RUN if [ "$(go env GOHOSTARCH)" = "${TARGETARCH}" ] && [ "$(go env GOHOSTOS)" = "${TARGETOS}" ]; then \
        /out/vif -check -d > /dev/null && echo "config check ok"; \
    else \
        echo "cross build: config check deferred to the init container"; \
    fi

# --- runtime -------------------------------------------------------------------
FROM scratch

ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE=https://github.com/lixenwraith/vi-fighter

LABEL org.opencontainers.image.title="vi-fighter" \
      org.opencontainers.image.description="Vi-Fighter dedicated session server" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="BSD-3-Clause"

COPY --from=build /out/vif /vif

# Numeric, because there is no /etc/passwd in scratch to resolve a name against
# and `runAsNonRoot: true` needs a UID it can compare. 65532 is the conventional
# unprivileged "nonroot" ID; the pod spec sets the same value so a cluster that
# ignores the image's user still refuses to run this as root.
USER 65532:65532

# 7777 is the framed TCP game protocol and 7778 is the HTTP probe endpoint. They
# are separate listeners so the probe port can stay inside the cluster while the
# game port is the only thing a player's client reaches.
EXPOSE 7777 7778

ENTRYPOINT ["/vif"]

# A lone container started with no arguments is still a usable session: allocated
# lifetime included, so a container run by hand cannot outlive its own emptiness.
# A workload overrides this with its own args; see deploy/k3s/30-session.yaml.
CMD ["-serve", ":7777", "-probe", ":7778", "-log-stdout", "-lv", "info", \
     "-d", "-size", "120x40", "-players", "4", \
     "-first-join", "90s", "-empty", "90s", "-drain", "20s"]
