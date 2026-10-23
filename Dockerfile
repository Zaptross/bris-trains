# syntax=docker/dockerfile:1
#
# Packages the release binaries CI has already built (bin/trains-linux-<arch>);
# nothing is compiled here, so multi-arch images need no emulation.
#
#   docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 .
#
# The timetable cache lives in the nonroot user's ~/.cache/trains; mount a
# volume there to keep it across container restarts.

# distroless/static-debian12:nonroot (multi-arch index: amd64, arm64, arm/v7)
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

# buildx sets these per platform: amd64, arm64, or arm + v7.
ARG TARGETARCH
ARG TARGETVARIANT
COPY --chmod=0755 bin/trains-linux-${TARGETARCH}${TARGETVARIANT} /trains

USER nonroot:nonroot
VOLUME /home/nonroot/.cache
EXPOSE 8080
ENTRYPOINT ["/trains", "serve", "--addr", ":8080"]
