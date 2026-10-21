# The dedicated-session image: the static `vif_headless` binary on scratch, with no
# shell, libc or configuration tree; a session's data is compiled in or mounted
# read-only. Build from the repository root with BuildKit, which the caches need:
#   docker buildx build -f deploy/docker/Dockerfile -t vif:dev .    # or: make image
# See doc/kube-docker-deploy.md.

# By digest, so a cached base is never re-resolved and downloads once per pin. Keep
# its version equal to the go directive in ../../go.mod.
ARG GO_IMAGE=docker.io/library/golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d

# --- builder -------------------------------------------------------------------
# The builder is not thin and does not need to be; nothing from it is shipped
# except /out/vif. git is installed so the Go toolchain can stamp the commit into
# the binary's own build info, which is what makes a running pod attributable to a
# revision without trusting a label somebody could have written by hand.
FROM ${GO_IMAGE} AS build

RUN apk add --no-cache git
WORKDIR /src
COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

# CGO off makes the binary static, so the final stage can be scratch; -s -w keep the
# build info the run is identified by. The module and build caches outlive the build
# in BuildKit's store: a rebuild fetches only new modules and compiles only changes.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -tags=vif_headless -ldflags="-s -w" -o /out/vif ./cmd/vif

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
ARG SOURCE=https://github.com/lixenwraith/vif

LABEL org.opencontainers.image.title="vif" \
      org.opencontainers.image.description="vif dedicated session server" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="BSD-3-Clause" \
      dev.lixenwraith.vif.build-profile="headless"

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
